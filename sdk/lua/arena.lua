local websocket = require "http.websocket"
local cqueues = require "cqueues"
local json = require "dkjson"

local arena = { VERSION = "0.4.0" }
local pickup_config = { auto_pickup = true, pickup_types = {} }
local latest_observation
local zone_history
local cached_layout = { match_id = nil, obstacles = {}, hazards = {} }

local function clamp(value, low, high)
  return math.max(low, math.min(high, value))
end

function arena.distance(a, b)
  local dx, dy = b.x - a.x, b.y - a.y
  return math.sqrt(dx * dx + dy * dy)
end

function arena.bearing(a, b)
  return math.deg(math.atan(b.y - a.y, b.x - a.x))
end

function arena.nearest_enemy(observation)
  local nearest, nearest_distance
  for _, robot in ipairs(observation.robots or {}) do
    if robot.alive and robot.team ~= observation.self.team then
      local distance = arena.distance(observation.self, robot)
      if nearest_distance == nil or distance < nearest_distance then
        nearest, nearest_distance = robot, distance
      end
    end
  end
  return nearest, nearest_distance
end

function arena.configure(options)
  options = options or {}
  if options.auto_pickup ~= nil then
    pickup_config.auto_pickup = options.auto_pickup == true
  end
  if options.pickup_types ~= nil then
    assert(type(options.pickup_types) == "table", "pickup_types must be a table")
    pickup_config.pickup_types = options.pickup_types
  end
end

function arena.nearest_item(observation, item_type)
  local nearest, nearest_distance
  for _, item in ipairs(observation.items or {}) do
    if item.active ~= false and (item_type == nil or item.type == item_type) then
      local distance = arena.distance(observation.self, item)
      if nearest_distance == nil or distance < nearest_distance then
        nearest, nearest_distance = item, distance
      end
    end
  end
  return nearest, nearest_distance
end

local function segment_intersects_aabb(x1, y1, x2, y2, obstacle)
  local minimum_x = obstacle.x or 0
  local minimum_y = obstacle.y or 0
  local maximum_x = minimum_x + (obstacle.width or 0)
  local maximum_y = minimum_y + (obstacle.height or 0)
  local dx, dy = x2 - x1, y2 - y1
  local first, last = 0, 1
  for _, edge in ipairs({ { -dx, x1 - minimum_x }, { dx, maximum_x - x1 }, { -dy, y1 - minimum_y }, { dy, maximum_y - y1 } }) do
    local p, q = edge[1], edge[2]
    if p == 0 and q < 0 then return false end
    if p ~= 0 then
      local ratio = q / p
      if p < 0 then first = math.max(first, ratio) else last = math.min(last, ratio) end
      if first > last then return false end
    end
  end
  return true
end

local function segment_intersects_circle(x1, y1, x2, y2, obstacle)
  local dx, dy = x2 - x1, y2 - y1
  local length_squared = dx * dx + dy * dy
  local amount = 0
  if length_squared > 0 then
    amount = clamp(((obstacle.x - x1) * dx + (obstacle.y - y1) * dy) / length_squared, 0, 1)
  end
  local nearest_x, nearest_y = x1 + amount * dx, y1 + amount * dy
  local radius = obstacle.radius or 0
  return (nearest_x - obstacle.x) ^ 2 + (nearest_y - obstacle.y) ^ 2 <= radius ^ 2
end

-- Uniform-grid index over an obstacle array, built once per array and cached
-- weakly: the SDK reuses one array per geometry revision, so dense maps pay
-- the build cost once instead of scanning every obstacle per query.
local INDEX_CELL = 256
local INDEX_MIN = 48
local index_cache = setmetatable({}, { __mode = "k" })
local function obstacle_bounds(o)
  if o.shape == "circle" or o.radius ~= nil then
    local r = o.radius or 0
    return o.x - r, o.y - r, o.x + r, o.y + r
  end
  return o.x or 0, o.y or 0, (o.x or 0) + (o.width or 0), (o.y or 0) + (o.height or 0)
end
local function cell_key(cx, cy) return cx * 1048576 + cy end
local function obstacle_index(obstacles)
  if #obstacles < INDEX_MIN then return nil end
  local index = index_cache[obstacles]
  if index and index.count == #obstacles then return index end
  index = { cells = {}, count = #obstacles, stamp = 0, seen = {} }
  for i, o in ipairs(obstacles) do
    local x0, y0, x1, y1 = obstacle_bounds(o)
    for cx = math.floor(x0 / INDEX_CELL), math.floor(x1 / INDEX_CELL) do
      for cy = math.floor(y0 / INDEX_CELL), math.floor(y1 / INDEX_CELL) do
        local key = cell_key(cx, cy)
        local cell = index.cells[key]
        if not cell then cell = {}; index.cells[key] = cell end
        cell[#cell + 1] = i
      end
    end
  end
  index_cache[obstacles] = index
  return index
end
-- Calls visit(obstacle) once for each obstacle whose cells overlap the box;
-- stops early and returns true when visit returns true.
local function each_candidate(obstacles, x0, y0, x1, y1, visit)
  local index = obstacle_index(obstacles)
  if not index then
    for _, o in ipairs(obstacles) do if visit(o) then return true end end
    return false
  end
  index.stamp = index.stamp + 1
  local stamp, seen = index.stamp, index.seen
  for cx = math.floor(math.min(x0, x1) / INDEX_CELL), math.floor(math.max(x0, x1) / INDEX_CELL) do
    for cy = math.floor(math.min(y0, y1) / INDEX_CELL), math.floor(math.max(y0, y1) / INDEX_CELL) do
      local cell = index.cells[cell_key(cx, cy)]
      if cell then
        for _, i in ipairs(cell) do
          if seen[i] ~= stamp then
            seen[i] = stamp
            if visit(obstacles[i]) then return true end
          end
        end
      end
    end
  end
  return false
end
local function segment_blocked_by(x1, y1, x2, y2, obstacle)
  if obstacle.shape == "circle" or obstacle.radius ~= nil then
    return segment_intersects_circle(x1, y1, x2, y2, obstacle)
  end
  return segment_intersects_aabb(x1, y1, x2, y2, obstacle)
end

function arena.line_of_sight(x1, y1, x2, y2, obstacles)
  obstacles = obstacles or (latest_observation and latest_observation.obstacles) or {}
  -- Long segments walk the grid in short pieces so only nearby cells are read.
  local length = math.sqrt((x2 - x1) ^ 2 + (y2 - y1) ^ 2)
  local pieces = math.max(1, math.ceil(length / INDEX_CELL))
  for n = 0, pieces - 1 do
    local ax, ay = x1 + (x2 - x1) * n / pieces, y1 + (y2 - y1) * n / pieces
    local bx, by = x1 + (x2 - x1) * (n + 1) / pieces, y1 + (y2 - y1) * (n + 1) / pieces
    if each_candidate(obstacles, ax, ay, bx, by, function(o) return segment_blocked_by(x1, y1, x2, y2, o) end) then
      return false
    end
  end
  return true
end

-- Distance along `heading` (degrees) to the first obstacle, capped at
-- max_distance. Returns the distance and the obstacle hit, if any.
function arena.raycast(x, y, heading, max_distance, obstacles)
  obstacles = obstacles or (latest_observation and latest_observation.obstacles) or {}
  max_distance = max_distance or 1000
  local radians = math.rad(heading)
  local ex, ey = x + math.cos(radians) * max_distance, y + math.sin(radians) * max_distance
  if arena.line_of_sight(x, y, ex, ey, obstacles) then return max_distance, nil end
  local low, high, hit = 0, max_distance, nil
  for _ = 1, 14 do
    local mid = (low + high) / 2
    local mx, my = x + math.cos(radians) * mid, y + math.sin(radians) * mid
    local blocker
    each_candidate(obstacles, x, y, mx, my, function(o)
      if segment_blocked_by(x, y, mx, my, o) then blocker = o; return true end
    end)
    if blocker then high, hit = mid, blocker else low = mid end
  end
  return high, hit
end

-- Obstacles whose bounds come within `radius` of (x, y), nearest first.
function arena.obstacles_near(observation, x, y, radius)
  local found = {}
  each_candidate(observation.obstacles or {}, x - radius, y - radius, x + radius, y + radius, function(o)
    local x0, y0, x1, y1 = obstacle_bounds(o)
    local nx, ny = clamp(x, x0, x1), clamp(y, y0, y1)
    local d = math.sqrt((nx - x) ^ 2 + (ny - y) ^ 2)
    if d <= radius then found[#found + 1] = { obstacle = o, distance = d } end
  end)
  table.sort(found, function(a, b) return a.distance < b.distance end)
  local list = {}
  for i, entry in ipairs(found) do list[i] = entry.obstacle end
  return list
end

function arena.path_to(x, y, observation)
  observation = observation or latest_observation
  assert(observation and observation.self, "path_to requires an observation before arena.run receives one")
  local self = observation.self
  if arena.line_of_sight(self.x, self.y, x, y, observation.obstacles) then
    return { { x = x, y = y } }
  end
  local margin = 20
  for _, obstacle in ipairs(observation.obstacles or {}) do
    if obstacle.radius ~= nil then
      local angle = math.atan(self.y - obstacle.y, self.x - obstacle.x)
      local radius = obstacle.radius + margin
      return { { x = obstacle.x + math.cos(angle) * radius, y = obstacle.y + math.sin(angle) * radius }, { x = x, y = y } }
    end
    local corners = {
      { x = obstacle.x - margin, y = obstacle.y - margin },
      { x = obstacle.x + obstacle.width + margin, y = obstacle.y - margin },
      { x = obstacle.x - margin, y = obstacle.y + obstacle.height + margin },
      { x = obstacle.x + obstacle.width + margin, y = obstacle.y + obstacle.height + margin },
    }
    table.sort(corners, function(a, b) return arena.distance(self, a) < arena.distance(self, b) end)
    for _, corner in ipairs(corners) do
      if arena.line_of_sight(self.x, self.y, corner.x, corner.y, observation.obstacles) then
        return { corner, { x = x, y = y } }
      end
    end
  end
  return {}
end

function arena.action(options)
  options = options or {}
  if latest_observation and latest_observation.version == 4 then
    local action = arena.control(options)
    if options.move ~= nil then action.throttle = clamp(options.move / 8, -1, 1) end
    if options.turn ~= nil and options.throttle == nil then action.turn = clamp(options.turn / 18, -1, 1) end
    if options.target_x and options.target_y then
      action.aim = arena.bearing(latest_observation.self, { x = options.target_x, y = options.target_y })
    end
    return action
  end
  local action = {
    move = clamp(options.move or 0, -4, 8),
    turn = clamp(options.turn or 0, -18, 18),
    fire = options.fire == true,
    targetX = options.target_x,
    targetY = options.target_y,
    logs = options.logs or {},
    equipment = options.equipment or { "cannon", "scanner" },
    memoryMb = options.memory_mb or 0,
    sdkVersion = arena.VERSION,
    autoPickup = pickup_config.auto_pickup,
    pickupTypes = pickup_config.pickup_types,
  }
  if options.dash then action.dash = true end
  if options.deploy then action.deploy = options.deploy end
  if options.scan then action.scan = options.scan end
  if options.message then action.message = options.message end
  return action
end

function arena.fire_at(target, options)
  options = options or {}
  return arena.action({
    move = options.move or 0,
    fire = true,
    target_x = target.x,
    target_y = target.y,
    logs = options.logs,
    equipment = options.equipment,
    memory_mb = options.memory_mb,
  })
end

function arena.approach(observation, target, speed)
  return arena.fire_at(target, {
    move = speed or 8,
    logs = { string.format("tracking %s at %.1f units", target.name, arena.distance(observation.self, target)) },
  })
end

function arena.strafe(observation, target, direction)
  local desired = arena.bearing(observation.self, target) + (direction or 1) * 75
  local turn = ((desired - observation.self.heading + 540) % 360) - 180
  return arena.action({ move = 6, turn = turn, fire = true, equipment = { "cannon", "light-armor" } })
end

-- Server relays at most 128 bytes per message; trim on a UTF-8 character
-- boundary so multibyte text is never cut mid-sequence.
local function trim_message(text)
  text = tostring(text)
  if #text <= 128 then return text end
  local bytes, index = 0, 1
  while index <= #text do
    local byte = text:byte(index)
    local size = byte >= 0xF0 and 4 or byte >= 0xE0 and 3 or byte >= 0xC0 and 2 or 1
    if bytes + size > 128 then break end
    bytes, index = bytes + size, index + size
  end
  return text:sub(1, index - 1)
end

function arena.dash(options)
  options = options or {}
  local action = arena.action(options)
  action.dash = true
  return action
end

function arena.deploy_mine(options)
  options = options or {}
  local action = arena.action(options)
  action.deploy = "mine"
  return action
end

function arena.scan(x, y, radius, options)
  options = options or {}
  local action = arena.action(options)
  action.scan = { x = x, y = y, radius = clamp(radius or 200, 40, 400) }
  return action
end

function arena.send_message(text, options)
  options = options or {}
  if text == nil then return arena.action(options) end
  local action = arena.action(options)
  action.message = trim_message(text)
  return action
end

-- Query helpers are pure reads on one observation. They copy entries and
-- attach a distance field instead of mutating the server payload.

local function with_distance(origin, entry)
  local copy = {}
  for key, value in pairs(entry) do copy[key] = value end
  local dx, dy = entry.x - origin.x, entry.y - origin.y
  copy.distance = math.sqrt(dx * dx + dy * dy)
  return copy
end

local function sort_by_distance(list, id_key)
  table.sort(list, function(a, b)
    if a.distance ~= b.distance then return a.distance < b.distance end
    return (a[id_key] or "") < (b[id_key] or "")
  end)
  return list
end

function arena.items_in_area(observation, x, y, radius)
  local origin = { x = x, y = y }
  local found = {}
  for _, item in ipairs(observation.items or {}) do
    if item.active ~= false then
      local entry = with_distance(origin, item)
      if entry.distance <= radius then found[#found + 1] = entry end
    end
  end
  return sort_by_distance(found, "itemId")
end

function arena.enemies_in_area(observation, x, y, radius)
  local origin = { x = x, y = y }
  local found = {}
  for _, robot in ipairs(observation.robots or {}) do
    if robot.alive and robot.team ~= observation.self.team then
      local entry = with_distance(origin, robot)
      if entry.distance <= radius then found[#found + 1] = entry end
    end
  end
  return sort_by_distance(found, "robotId")
end

-- Robots perceive enemies, items, projectiles, and mines only inside a
-- per-tick vision range (engine default 320). The server already filtered the
-- observation; these helpers let scripts reason about the same boundary
-- locally. A range of 0 means the observation carried none: treat sight as
-- unlimited. Teammates and map geometry (obstacles, hazards, zone) are never
-- vision-filtered.

function arena.vision_range(observation)
  local range = observation.visionRange
  if range == nil then range = (observation.self or {}).visionRange end
  if range == nil or range <= 0 then return 0 end
  return range
end

function arena.in_vision(observation, x, y)
  local range = arena.vision_range(observation)
  if range <= 0 then return true end
  local self = observation.self
  if not self or self.x == nil or y == nil then return true end
  return arena.distance(self, { x = x, y = y }) <= range
end

-- Sight = inside the vision range and not blocked by map geometry.
function arena.can_see(observation, robot)
  if not robot or not observation.self or observation.self.x == nil then return false end
  return arena.in_vision(observation, robot.x, robot.y)
    and arena.line_of_sight(observation.self.x, observation.self.y, robot.x, robot.y, observation.obstacles)
end

function arena.visible_enemies(observation)
  local self = observation.self
  local range = arena.vision_range(observation)
  return arena.enemies_in_area(observation, self.x, self.y, range > 0 and range or math.huge)
end

local function hazard_center(hazard)
  return { x = hazard.x + (hazard.width or 0) / 2, y = hazard.y + (hazard.height or 0) / 2 }
end

local function mines_in_radius(observation, origin, radius)
  local source = observation.mines
  if source == nil and observation.scanResult then source = observation.scanResult.mines end
  local found = {}
  for _, mine in ipairs(source or {}) do
    if mine.active ~= false then
      local entry = with_distance(origin, mine)
      if entry.distance <= radius then
        if mine.armed ~= nil then
          entry.armed = mine.armed == true
        else
          entry.armed = observation.tick ~= nil and mine.armTick ~= nil and observation.tick >= mine.armTick
        end
        found[#found + 1] = entry
      end
    end
  end
  return sort_by_distance(found, "mineId")
end

function arena.area_report(observation, x, y, radius)
  local origin = { x = x, y = y }
  local hazards = {}
  for _, hazard in ipairs(observation.hazards or {}) do
    local entry = with_distance(origin, hazard)
    entry.distance = arena.distance(origin, hazard_center(hazard))
    if entry.distance <= radius then hazards[#hazards + 1] = entry end
  end
  return {
    items = arena.items_in_area(observation, x, y, radius),
    enemies = arena.enemies_in_area(observation, x, y, radius),
    mines = mines_in_radius(observation, origin, radius),
    hazards = sort_by_distance(hazards, "id"),
  }
end

-- Snapshots carry projectiles at the top level; agent observations omit them
-- today, so read both shapes and treat missing arrays as empty.
local function observation_projectiles(observation)
  local self = observation.self or {}
  return observation.projectiles or self.projectiles or {}
end

function arena.projectiles_near(observation, radius)
  local found = {}
  for _, projectile in ipairs(observation_projectiles(observation)) do
    local entry = with_distance(observation.self, projectile)
    if entry.distance <= radius then found[#found + 1] = entry end
  end
  return sort_by_distance(found, "projectileId")
end

function arena.mines_near(observation, radius)
  return mines_in_radius(observation, observation.self, radius)
end

-- Linear lead: flight time is distance over projectile speed and the target
-- drifts targetVX/targetVY per tick. Observations carry no robot velocity, so
-- callers estimate it (heading * speed) or accept the current position.
function arena.aim_predict(from, target, projectile_speed, target_vx, target_vy)
  if not from or not target then return nil end
  local vx, vy = target_vx or target.vx or 0, target_vy or target.vy or 0
  local speed = projectile_speed or 0
  if speed <= 0 or (vx == 0 and vy == 0) then return { x = target.x, y = target.y } end
  local flight = arena.distance(from, target) / speed
  return { x = target.x + vx * flight, y = target.y + vy * flight }
end

-- Closest approach of a projectile to self along its velocity: returns the
-- flight time to the closest point and the miss distance, or nil when it
-- recedes.
local function closest_approach(self, projectile)
  local vx, vy = projectile.vx or 0, projectile.vy or 0
  local speed_squared = vx * vx + vy * vy
  if speed_squared <= 0 then return nil end
  local time = ((self.x - projectile.x) * vx + (self.y - projectile.y) * vy) / speed_squared
  if time <= 0 then return nil end
  local miss = arena.distance(self, { x = projectile.x + vx * time, y = projectile.y + vy * time })
  return time, miss
end

-- Counts projectiles whose flight path passes within 30 units of us while
-- still approaching; 0 means nothing is aimed at our current position.
function arena.danger_level(observation)
  local threats = 0
  for _, projectile in ipairs(observation_projectiles(observation)) do
    local _, miss = closest_approach(observation.self, projectile)
    if miss and miss <= 30 then threats = threats + 1 end
  end
  return threats
end

-- Perpendicular escape point from the most imminent projectile (smallest time
-- to closest approach). Nil when nothing threatens.
function arena.dodge(observation)
  local self = observation.self
  local threat, threat_time
  for _, projectile in ipairs(observation_projectiles(observation)) do
    local time, miss = closest_approach(self, projectile)
    if time and miss <= 30 and (threat_time == nil or time < threat_time) then
      threat, threat_time = projectile, time
    end
  end
  if not threat then return nil end
  local vx, vy = threat.vx or 0, threat.vy or 0
  local offset_x, offset_y = self.x - (threat.x + vx * threat_time), self.y - (threat.y + vy * threat_time)
  if offset_x * offset_x + offset_y * offset_y < 0.0001 then
    -- Dead center on the flight path: either perpendicular escapes.
    offset_x, offset_y = -vy, vx
  end
  local length = math.max(math.sqrt(offset_x * offset_x + offset_y * offset_y), 0.001)
  local x, y = self.x + offset_x / length * 60, self.y + offset_y / length * 60
  if observation.arenaWidth then x = clamp(x, 20, observation.arenaWidth - 20) end
  if observation.arenaHeight then y = clamp(y, 20, observation.arenaHeight - 20) end
  return { x = x, y = y }
end

-- closing compares this tick's zone radius with the previous observed tick,
-- so the first call of a match reports false. Repeated calls within one tick
-- replay the stored verdict; history is keyed by matchId to survive restarts.
function arena.zone_status(observation)
  local zone = observation.zone
  if not zone then return nil end
  local self = observation.self or {}
  local distance = self.x and arena.distance(self, zone) or 0
  local history = zone_history
  local closing
  local fresh = history and history.matchId == observation.matchId and history.tick == observation.tick
  if fresh then
    closing = history.closing
  elseif history and history.matchId == observation.matchId and history.tick ~= nil and observation.tick ~= nil then
    closing = zone.radius < history.radius
  else
    closing = false
  end
  if not fresh then
    zone_history = { tick = observation.tick, radius = zone.radius, matchId = observation.matchId, closing = closing }
  end
  return { inside = distance <= zone.radius, distanceToEdge = zone.radius - distance, radius = zone.radius, x = zone.x, y = zone.y, closing = closing }
end

function arena.hazard_at(observation, x, y)
  for _, hazard in ipairs(observation.hazards or {}) do
    if x >= hazard.x and x <= hazard.x + (hazard.width or 0) and y >= hazard.y and y <= hazard.y + (hazard.height or 0) then
      return hazard
    end
  end
  return nil
end

function arena.teammates(observation)
  local found = {}
  for _, robot in ipairs(observation.robots or {}) do
    if robot.alive and robot.team == observation.self.team and robot.robotId ~= observation.self.robotId then
      found[#found + 1] = robot
    end
  end
  return found
end

local function point_blocked(x, y, obstacles, margin)
  margin = margin or 0
  return each_candidate(obstacles, x - margin, y - margin, x + margin, y + margin, function(obstacle)
    if obstacle.shape == "circle" or obstacle.radius ~= nil then
      return (x - obstacle.x) ^ 2 + (y - obstacle.y) ^ 2 <= ((obstacle.radius or 0) + margin) ^ 2
    end
    local x0, y0, x1, y1 = obstacle_bounds(obstacle)
    return x >= x0 - margin and x <= x1 + margin and y >= y0 - margin and y <= y1 + margin
  end)
end

-- Deterministic ring sampling around (x, y): a small LCG seeded from the
-- request coordinates keeps the result stable across calls and reconnects
-- without wall-clock time.
function arena.random_safe_point(observation, x, y, radius, tries)
  local obstacles = observation.obstacles or {}
  local state = (math.floor(x * 1024) * 73856093 + math.floor(y * 1024) * 19349663) % 2147483647
  local function random_unit()
    state = (state * 48271) % 2147483647
    return state / 2147483647
  end
  for _ = 1, tries or 8 do
    local angle = random_unit() * 2 * math.pi
    local distance = math.sqrt(random_unit()) * (radius or 60)
    local point = { x = x + math.cos(angle) * distance, y = y + math.sin(angle) * distance }
    if observation.arenaWidth then point.x = clamp(point.x, 20, observation.arenaWidth - 20) end
    if observation.arenaHeight then point.y = clamp(point.y, 20, observation.arenaHeight - 20) end
    if not point_blocked(point.x, point.y, obstacles) and arena.line_of_sight(x, y, point.x, point.y, obstacles) then
      return point
    end
  end
  return { x = x, y = y }
end

local function connect(config)
  local ws = websocket.new_from_uri(assert(config.url, "arena URL is required"), { "robot-arena.v4", "robot-arena.v1" })
  ws.request.headers:upsert("authorization", "Bearer " .. assert(config.token, "robot token is required"), true)
  ws.request.headers:upsert("x-robot-sdk-version", arena.VERSION)
  assert(ws:connect(10))
  return ws
end

-- Protocol v4 uses normalized controls and zero-based inventory/weapon slots.
function arena.control(options)
  options = options or {}
  local out = {}
  for _, key in ipairs({ "throttle", "brake", "turn", "aim", "fire", "cruise", "dash", "scan", "weapon", "utility", "consume", "pickup", "dropSlot", "equip", "dropEquipment", "pickupPriorities", "transit", "message", "label" }) do
    if options[key] ~= nil then out[key] = options[key] end
  end
  if out.throttle then out.throttle = clamp(out.throttle, -1, 1) end
  if out.turn then out.turn = clamp(out.turn, -1, 1) end
  return out
end

-- Steers toward `point`. Brakes inside options.arrive (default 25, inside the
-- 35-unit pickup reach so driving to loot still ends in range) instead
-- of orbiting the point, slows on approach, crawls through sharp turns, and
-- reverses toward nearby points behind the robot rather than pivoting.
-- options.reverse = false disables reversing.
function arena.drive_to(obs, point, options)
  options = options or {}
  local self = obs.self
  local base = { aim = options.aim, fire = options.fire == true, label = options.label or "NAVIGATE" }
  local distance = arena.distance(self, point)
  if distance < (options.arrive or 25) then
    base.brake, base.throttle, base.turn = true, 0, 0
    return arena.control(base)
  end
  local turn = (arena.bearing(self, point) - self.heading + 180) % 360 - 180
  local approach = clamp(distance / 150, 0.35, 1)
  if math.abs(turn) > 120 and options.reverse ~= false and (distance < 350 or options.reverse) then
    local back = (turn + 360) % 360 - 180
    base.turn, base.throttle = clamp(back / 18, -1, 1), -approach
    return arena.control(base)
  end
  base.turn = clamp(turn / 18, -1, 1)
  base.throttle = math.abs(turn) > 70 and 0.35 or math.abs(turn) > 35 and math.min(0.7, approach) or approach
  base.cruise = options.cruise == true and math.abs(turn) < 15
  return arena.control(base)
end

function arena.weapon_ready(obs, slot)
  local w = (obs.self.weapons or {})[(slot or obs.self.activeWeapon or 0) + 1]
  return w and not w.overheated and w.readyAt <= obs.tick
end

function arena.find_consumable(obs, kind)
  for i, item in ipairs(obs.self.inventory or {}) do if item.kind == kind and item.count > 0 then return i - 1 end end
end

function arena.scan_contacts(obs)
  for _, event in ipairs(obs.events or {}) do
    if event.type == "scan_result" then return json.decode(event.message) or {} end
  end
  return {}
end

function arena.stuck_tracker()
  return { x = nil, y = nil, tick = nil, stalled = 0 }
end
-- Recovery for a robot pinned against cover. Call every decision with a
-- persistent `memory` table and record each returned action with
-- arena.note_action. After ~0.8 s of commanded driving with no progress it
-- backs out (or pushes forward, if it was reversing) while turning for one
-- second, alternating sides. Returns the escape action and, on the decision
-- that starts an escape, true so the caller can drop its current target.
function arena.unstick(obs, memory)
  if memory.escape_until and obs.tick < memory.escape_until then
    return arena.control({ throttle = memory.escape_throttle, turn = memory.side, label = "UNSTICK" }), false
  end
  local self = obs.self
  local driving = math.abs(memory.last_throttle or 0) >= 0.3
  if driving and memory.x and arena.distance(self, memory) < 1 then
    memory.stalled = (memory.stalled or 0) + 1
  else
    memory.stalled = 0
  end
  memory.x, memory.y = self.x, self.y
  if memory.stalled < 8 then return nil, false end
  memory.stalled = 0
  memory.side = -(memory.side or 1)
  memory.escape_throttle = (memory.last_throttle or 1) > 0 and -1 or 1
  memory.escape_until = obs.tick + 20
  memory.escapes = (memory.escapes or 0) + 1
  return arena.control({ throttle = memory.escape_throttle, turn = memory.side, label = "UNSTICK" }), true
end
function arena.note_action(memory, action)
  memory.last_throttle = (action and not action.brake and action.throttle) or 0
end

function arena.is_stuck(obs, memory)
  if memory.tick == obs.tick then return memory.stalled >= 10 end
  if memory.x and arena.distance(obs.self, memory) < 1 then memory.stalled = memory.stalled + 1 else memory.stalled = 0 end
  memory.x, memory.y, memory.tick = obs.self.x, obs.self.y, obs.tick
  return memory.stalled >= 10
end

-- Search progresses across decide calls. A caller chooses a work budget,
-- bounded to 256 expansions per call and 4096 total explored cells.
function arena.begin_path(obs, goal)
  local obstacles = {}
  for _, o in ipairs(obs.obstacles or {}) do
    -- Only circles carry a radius: the geometry helpers treat any obstacle
    -- with a radius as a circle, so giving boxes one erased every wall.
    -- A robot hugging cover already stands inside that piece's 15-unit
    -- margin; inflating it would block every move and make the search
    -- report unreachable, so plan against its real outline instead.
    local sx, sy = obs.self.x, obs.self.y
    if o.shape == "circle" or o.radius ~= nil then
      local inside = (sx - o.x) ^ 2 + (sy - o.y) ^ 2 < ((o.radius or 0) + 15) ^ 2
      obstacles[#obstacles + 1] = { shape = "circle", x = o.x, y = o.y, radius = (o.radius or 0) + (inside and 0 or 15) }
    else
      local w, h = o.width or 0, o.height or 0
      local inside = sx > o.x - 15 and sx < o.x + w + 15 and sy > o.y - 15 and sy < o.y + h + 15
      local m = inside and 0 or 15
      obstacles[#obstacles + 1] = { shape = o.shape, x = o.x - m, y = o.y - m, width = w + 2 * m, height = h + 2 * m }
    end
  end
  return { start = { x = obs.self.x, y = obs.self.y }, goal = { x = goal.x, y = goal.y },
    open = { { x = obs.self.x, y = obs.self.y, g = 0 } }, seen = {}, expanded = 0,
    revision = obs.revision, obstacles = obstacles, width = obs.arenaWidth, height = obs.arenaHeight, status = "pending" }
end
function arena.advance_path(search, budget)
  if search.status ~= "pending" then return search end
  for _ = 1, clamp(budget or 64, 1, 256) do
    if #search.open == 0 or search.expanded >= 4096 then search.status = "unreachable"; return search end
    local best = 1
    for i = 2, #search.open do
      if search.open[i].g + arena.distance(search.open[i], search.goal) < search.open[best].g + arena.distance(search.open[best], search.goal) then best = i end
    end
    local node = table.remove(search.open, best)
    search.expanded = search.expanded + 1
    if arena.line_of_sight(node.x, node.y, search.goal.x, search.goal.y, search.obstacles) then
      local path = { search.goal }
      while node do table.insert(path, 1, { x = node.x, y = node.y }); node = node.parent end
      search.status, search.path = "ready", path; return search
    end
    for _, d in ipairs({ {64,0}, {0,64}, {-64,0}, {0,-64} }) do
      local x, y = node.x + d[1], node.y + d[2]
      local key = tostring(x) .. ":" .. tostring(y)
      if not search.seen[key] and x >= 15 and y >= 15 and x <= search.width - 15 and y <= search.height - 15 and arena.line_of_sight(node.x, node.y, x, y, search.obstacles) then
        search.seen[key] = true; search.open[#search.open + 1] = { x = x, y = y, g = node.g + 64, parent = node }
      end
    end
  end
  return search
end
function arena.follow_path(obs, search, arrive)
  if search.revision ~= obs.revision then return arena.control({ brake = true, label = "PATH_REVISION_CHANGED" }) end
  if search.status ~= "ready" then return arena.control({ brake = true, label = "PATH_" .. string.upper(search.status) }) end
  search.waypoint = search.waypoint or 2
  -- Intermediate waypoints advance within 48 units; the final one only
  -- counts as reached inside the arrival radius, so short hops still end
  -- within pickup reach.
  while search.waypoint < #search.path and arena.distance(obs.self, search.path[search.waypoint]) < 48 do search.waypoint = search.waypoint + 1 end
  if search.waypoint == #search.path and arena.distance(obs.self, search.path[#search.path]) < (arrive or 25) then search.waypoint = #search.path + 1 end
  -- Skip ahead to the furthest waypoint already in clear sight so the robot
  -- cuts corners instead of zig-zagging through every 64-unit grid step.
  for index = #search.path, math.min(#search.path, search.waypoint) + 1, -1 do
    local p = search.path[index]
    if arena.line_of_sight(obs.self.x, obs.self.y, p.x, p.y, search.obstacles) then search.waypoint = index; break end
  end
  if search.waypoint > #search.path then return arena.control({ brake = true, label = "ARRIVED" }) end
  local final = search.waypoint == #search.path
  return arena.drive_to(obs, search.path[search.waypoint], { arrive = final and (arrive or 25) or 1, label = "FOLLOW_PATH" })
end
function arena.transit_route(obs, goal)
  assert(goal and goal.x and goal.y, "transit_route requires a goal point; pass a site or safe_zone_goal(obs)")
  local best, cost = nil, arena.distance(obs.self, goal)
  for _, link in ipairs(obs.transit or {}) do
    local exit = { x = link.targetX, y = link.targetY }
    local usable = not obs.zone or (arena.distance(link, obs.zone) <= obs.zone.radius and arena.distance(exit, obs.zone) <= obs.zone.radius)
    local route_cost = arena.distance(obs.self, link) + arena.distance(exit, goal) + 160
    if usable and route_cost < cost then best, cost = link, route_cost end
  end
  return { entry = best, goal = goal, estimatedDistance = cost }
end
function arena.safe_zone_goal(obs)
  local z = obs.zone
  if not z then return { x = obs.arenaWidth / 2, y = obs.arenaHeight / 2 } end
  return { x = z.x, y = z.y }
end

-- Map-loot search. v4 observations expose loot as `items` entries shaped like
-- containers ({ itemId, x, y, contents = { { kind, count } } }) with no `type`
-- field, so nearest_item cannot filter by kind. find_consumable is different:
-- it reads your inventory and returns a consume slot, not a map position.
function arena.find_loot(obs, kind)
  local best, best_distance
  for _, item in ipairs(obs.items or {}) do
    if item.active ~= false then
      local match = kind == nil
      if not match then
        for _, stack in ipairs(item.contents or {}) do
          if stack.kind == kind then match = true; break end
        end
      end
      if match then
        local distance = arena.distance(obs.self, item)
        if best_distance == nil or distance < best_distance then
          best, best_distance = item, distance
        end
      end
    end
  end
  return best, best_distance
end

-- Nearest living teammate. Squad observations follow a survivor when you are
-- eliminated, but returned actions still drive your own robot.
function arena.nearest_ally(obs)
  local best, best_distance
  for _, robot in ipairs(obs.robots or {}) do
    if robot.alive and robot.team == obs.self.team and robot.robotId ~= obs.controlledRobotId then
      local distance = arena.distance(obs.self, robot)
      if best_distance == nil or distance < best_distance then
        best, best_distance = robot, distance
      end
    end
  end
  return best, best_distance
end

-- Aim where the enemy is going, not where it is. Observations carry no robot
-- velocity, so drift is estimated from heading at half cruise speed.
function arena.lead_for(obs, enemy, projectile_speed)
  local radians = (enemy.heading or 0) * math.pi / 180
  return arena.aim_predict(obs.self, enemy, projectile_speed or 24,
    math.cos(radians) * 2.8, math.sin(radians) * 2.8)
end

-- Orbit an enemy at roughly its current distance while keeping the turret on
-- it. direction is +1 or -1; flip it on a timer so orbits stay unpredictable.
function arena.strafe_around(obs, enemy, direction)
  direction = direction or 1
  local aim = arena.bearing(obs.self, enemy)
  local side = aim + 90 * direction
  local radians = side * math.pi / 180
  local point = { x = enemy.x + math.cos(radians) * 120, y = enemy.y + math.sin(radians) * 120 }
  return arena.drive_to(obs, point, { aim = aim, fire = arena.weapon_ready(obs), label = "STRAFE" })
end

-- Nearest reachable point hidden from `threat` behind nearby cover. Scans
-- obstacles within opts.radius (default 500) and returns the closest clear
-- spot on each obstacle's far side, or nil when nothing nearby blocks sight.
function arena.find_cover(obs, threat, opts)
  opts = opts or {}
  local self, obstacles = obs.self, obs.obstacles or {}
  local best, best_distance, best_obstacle
  for _, o in ipairs(arena.obstacles_near(obs, self.x, self.y, opts.radius or 500)) do
    local x0, y0, x1, y1 = obstacle_bounds(o)
    local cx, cy = (x0 + x1) / 2, (y0 + y1) / 2
    local dx, dy = cx - threat.x, cy - threat.y
    local length = math.sqrt(dx * dx + dy * dy)
    if length > 1 then
      local reach = math.sqrt((x1 - x0) ^ 2 + (y1 - y0) ^ 2) / 2 + (opts.clearance or 30)
      local spot = { x = cx + dx / length * reach, y = cy + dy / length * reach }
      local inside = (not obs.arenaWidth or (spot.x > 20 and spot.x < obs.arenaWidth - 20))
        and (not obs.arenaHeight or (spot.y > 20 and spot.y < obs.arenaHeight - 20))
      if inside and not point_blocked(spot.x, spot.y, obstacles, 18)
        and not arena.line_of_sight(threat.x, threat.y, spot.x, spot.y, obstacles) then
        local d = arena.distance(self, spot)
        if not best_distance or d < best_distance then best, best_distance, best_obstacle = spot, d, o end
      end
    end
  end
  return best, best_obstacle
end

-- Remembers enemies after they leave sight. Call arena.update_contacts every
-- decision; it returns contacts with `age` (ticks since seen) and a position
-- extrapolated from last velocity for up to one second. Contacts older than
-- ttl ticks (default 200, ten seconds) are forgotten.
function arena.contact_tracker(ttl)
  return { ttl = ttl or 200, contacts = {} }
end
function arena.update_contacts(tracker, obs)
  local self = obs.self
  for _, robot in ipairs(obs.robots or {}) do
    if robot.team ~= self.team then
      if robot.alive == false then
        tracker.contacts[robot.robotId] = nil
      else
        tracker.contacts[robot.robotId] = { robotId = robot.robotId, team = robot.team, x = robot.x, y = robot.y,
          vx = robot.vx or 0, vy = robot.vy or 0, hp = robot.hp, weapon = robot.weapon, seenTick = obs.tick }
      end
    end
  end
  local list = {}
  for id, c in pairs(tracker.contacts) do
    local age = obs.tick - c.seenTick
    if age > tracker.ttl then
      tracker.contacts[id] = nil
    else
      local lead = math.min(age, 20) / (obs.tickRate or 20)
      list[#list + 1] = { robotId = c.robotId, team = c.team, hp = c.hp, weapon = c.weapon, age = age,
        visible = age == 0, x = c.x + c.vx * lead, y = c.y + c.vy * lead, lastX = c.x, lastY = c.y }
    end
  end
  table.sort(list, function(a, b)
    if a.age ~= b.age then return a.age < b.age end
    return a.robotId < b.robotId
  end)
  return list
end

-- Best visible enemy to shoot: in range with line of sight, scored by
-- distance plus weighted health so weak nearby targets win. Returns the
-- robot and its score, or nil.
function arena.best_target(obs, opts)
  opts = opts or {}
  local self, range, hp_weight = obs.self, opts.range or 700, opts.hpWeight or 3
  local best, best_score
  for _, robot in ipairs(obs.robots or {}) do
    if robot.alive ~= false and robot.team ~= self.team then
      local d = arena.distance(self, robot)
      if d <= range and arena.line_of_sight(self.x, self.y, robot.x, robot.y, obs.obstacles) then
        local score = d + (robot.hp or 100) * hp_weight
        if not best_score or score < best_score or (score == best_score and robot.robotId < best.robotId) then
          best, best_score = robot, score
        end
      end
    end
  end
  return best, best_score
end

-- Nearest site, optionally filtered by kind (e.g. "armoury") or biome.
function arena.nearest_site(obs, filter)
  filter = filter or {}
  local best, best_distance
  for _, site in ipairs(obs.sites or {}) do
    if (not filter.kind or site.kind == filter.kind) and (not filter.biome or site.biome == filter.biome) then
      local d = arena.distance(obs.self, site)
      if not best_distance or d < best_distance then best, best_distance = site, d end
    end
  end
  return best, best_distance
end

-- Effective ranges from the engine catalogue, used for engagement spacing.
arena.WEAPON_RANGE = { plasma = 650, machine_gun = 450, shotgun = 250, cannon = 700, railgun = 1000,
  grenade = 500, incendiary = 450, cryo = 450, emp = 500 }
arena.PROJECTILE_SPEED = { plasma = 480, machine_gun = 640, shotgun = 600, cannon = 280, railgun = 0,
  grenade = 200, incendiary = 400, cryo = 400, emp = 360 }

-- Fights `enemy` at a weapon-appropriate distance: closes in when far, orbits
-- (strafes) inside the band, and backs off while still facing and firing when
-- too close. Pass a persistent `memory` table so the orbit direction flips on
-- a timer and after getting stuck instead of circling the same way forever.
function arena.engage(obs, enemy, memory, options)
  memory, options = memory or {}, options or {}
  local self = obs.self
  local weapon = (self.weapons or {})[(self.activeWeapon or 0) + 1]
  local range = arena.WEAPON_RANGE[weapon and weapon.kind or "plasma"] or 500
  local preferred = options.preferred or clamp(range * 0.65, 140, 650)
  -- Lead with the enemy's observed velocity (units/second) and the active
  -- weapon's projectile speed; railguns are hitscan.
  local lead = arena.aim_predict(self, enemy, arena.PROJECTILE_SPEED[weapon and weapon.kind or "plasma"] or 0, enemy.vx, enemy.vy)
  local aim = arena.bearing(self, lead)
  local fire = arena.weapon_ready(obs) ~= false and arena.line_of_sight(self.x, self.y, enemy.x, enemy.y, obs.obstacles)
  memory.orbit = memory.orbit or 1
  memory.flip_at = memory.flip_at or (obs.tick + 60)
  if obs.tick >= memory.flip_at then memory.orbit, memory.flip_at = -memory.orbit, obs.tick + 60 + (obs.tick % 40) end
  local d = arena.distance(self, enemy)
  local goal, label
  if d < preferred * 0.75 then
    goal, label = { x = self.x * 2 - enemy.x, y = self.y * 2 - enemy.y }, "KITE"
  elseif d > preferred * 1.2 then
    goal, label = { x = enemy.x, y = enemy.y }, "CLOSE_IN"
  else
    local side = math.rad(arena.bearing(self, enemy) + 90 * memory.orbit)
    goal, label = { x = self.x + math.cos(side) * 200, y = self.y + math.sin(side) * 200 }, "STRAFE"
  end
  return arena.drive_to(obs, goal, { aim = aim, fire = fire, label = options.label and (options.label .. "_" .. label) or label, arrive = 1, reverse = label == "KITE" })
end

-- Deterministic patrol across sites inside the safe zone. Keeps its own
-- state in `memory`; returns the next goal point. Re-picks on arrival or
-- after 20 seconds so robots keep exploring instead of parking.
function arena.patrol(obs, memory)
  local zone = obs.zone
  local inside = {}
  for _, site in ipairs(obs.sites or {}) do
    if not zone or not zone.radius or arena.distance(site, zone) < zone.radius * 0.85 then inside[#inside + 1] = site end
  end
  local stale = not memory.goal or arena.distance(obs.self, memory.goal) < 150 or obs.tick >= (memory.until_tick or 0)
    or (zone and zone.radius and arena.distance(memory.goal, zone) > zone.radius * 0.9)
  if stale then
    memory.count = (memory.count or 0) + 1
    if #inside == 0 then
      memory.goal = arena.safe_zone_goal(obs)
    else
      local seed = 0
      for i = 1, #(obs.self.robotId or "") do seed = (seed * 31 + string.byte(obs.self.robotId, i)) % 2147483647 end
      local site = inside[(seed + memory.count * 7919) % #inside + 1]
      local angle = (seed + memory.count) * 2.399963229728653
      memory.goal = { x = site.x + math.cos(angle) * 160, y = site.y + math.sin(angle) * 160 }
    end
    memory.until_tick = obs.tick + 400
  end
  return memory.goal
end

-- A complete decision loop assembled from the helpers above, used by every
-- shipped strategy. Priorities: escape when pinned, fight the best visible
-- target (retreating to cover when hurt), rotate into the zone, heal, pick up
-- nearby loot, hunt remembered contacts, then idle (patrol by default).
-- Options:
--   name            label prefix, e.g. "SCOUT" gives SCOUT_PATROL
--   range           max distance to engage a visible enemy (default 900)
--   preferred       engagement distance (default: 65% of weapon range)
--   retreat_hp      health fraction that triggers retreat (default 0.3)
--   loot_reach      { early, late } detour distances for loot (300, 120)
--   scan            "idle" (default), "always", or "never"
--   pickup          pickupPriorities sent on the first decision
--   on_enemy(obs, target, ctx)  may return an action to override fighting
--   idle(obs, ctx)              may return an action or a goal point
--   decorate(obs, action, ctx)  adjusts every action (utilities, messages)
-- ctx.travel(obs, goal, label) follows a bounded path with recovery;
-- ctx.state is persistent strategy memory.
function arena.tactics(options)
  options = options or {}
  local prefix = options.name and (options.name .. "_") or ""
  local reach = options.loot_reach or { 300, 120 }
  local state = { tried = {}, unstick = {}, route = nil, goal = nil, stuck = arena.stuck_tracker(),
    fight = {}, patrol = {}, contacts = arena.contact_tracker(200), sent_pickup = false }
  local ctx = { state = state }

  function ctx.travel(obs, goal, label)
    local moved = not state.goal or arena.distance(goal, state.goal) > 120
    if moved or not state.route or state.route.revision ~= obs.revision or arena.is_stuck(obs, state.stuck) then
      state.route, state.goal = arena.begin_path(obs, goal), goal
    end
    arena.advance_path(state.route, 96)
    if state.route.status == "unreachable" then
      -- Goals inside cover never resolve: retarget a clear point nearby.
      state.route = arena.begin_path(obs, arena.random_safe_point(obs, goal.x, goal.y, 260, 16))
      arena.advance_path(state.route, 96)
    end
    local action
    if state.route.status == "ready" then
      action = arena.follow_path(obs, state.route)
      if action.label == "ARRIVED" then state.route = nil end
    else
      action = arena.drive_to(obs, goal)
    end
    action.label = prefix .. label
    return action
  end

  local function choose(obs)
    local self = obs.self
    state.target = nil
    local contacts = arena.update_contacts(state.contacts, obs)
    local target = arena.best_target(obs, { range = options.range or 900 })
    if target then
      local override = options.on_enemy and options.on_enemy(obs, target, ctx)
      if override then return override end
      if self.hp < self.maxHp * (options.retreat_hp or 0.3) then
        local aim = arena.bearing(self, target)
        local cover = arena.find_cover(obs, target)
        local action = cover and arena.drive_to(obs, cover, { aim = aim, fire = true, label = prefix .. "TAKE_COVER" })
          or arena.drive_to(obs, { x = self.x * 2 - target.x, y = self.y * 2 - target.y }, { aim = aim, fire = true, label = prefix .. "RETREAT", reverse = true })
        action.dash = self.energy >= 30
        return action
      end
      return arena.engage(obs, target, state.fight, { preferred = options.preferred, label = options.name })
    end
    local zone = obs.zone
    if zone and zone.radius and arena.distance(self, zone) > zone.radius * 0.9 then
      return ctx.travel(obs, arena.safe_zone_goal(obs), "ROTATE")
    end
    local heal = arena.find_consumable(obs, "medkit") or arena.find_consumable(obs, "repair_pack")
    if heal and self.hp < self.maxHp * 0.6 then return arena.control({ brake = true, consume = heal, label = prefix .. "REPAIR" }) end
    -- Containers we cannot take stay on the map; remember tried ones so the
    -- robot never hovers beside one.
    local loot, loot_distance
    for _, item in ipairs(obs.items or {}) do
      local d = arena.distance(self, item)
      if not state.tried[item.itemId] and (not loot_distance or d < loot_distance) then loot, loot_distance = item, d end
    end
    if loot and loot_distance < 35 then
      state.tried[loot.itemId] = true
      return arena.control({ brake = true, pickup = loot.itemId, label = prefix .. "SCAVENGE" })
    end
    local remembered = contacts[1]
    if remembered and not remembered.visible then return ctx.travel(obs, remembered, "HUNT") end
    if loot and loot_distance < (obs.tick < 400 and reach[1] or reach[2]) then
      state.target = loot.itemId
      return ctx.travel(obs, loot, "LOOT")
    end
    local idle = options.idle and options.idle(obs, ctx)
    if idle and idle.x and not idle.label then return ctx.travel(obs, idle, "MOVE") end
    if idle then return idle end
    return ctx.travel(obs, arena.patrol(obs, state.patrol), "PATROL")
  end

  return function(obs)
    assert(obs.version == 4, "This strategy requires a v4 arena")
    local action, started = arena.unstick(obs, state.unstick)
    if action then
      if started then
        state.route = nil
        if state.target then state.tried[state.target] = true end
      end
      action.label = prefix .. "UNSTICK"
    else
      action = choose(obs)
    end
    local scan = options.scan or "idle"
    if scan == "always" or (scan == "idle" and not arena.best_target(obs, { range = options.range or 900 })) then
      action.scan = action.scan or obs.self.energy > 60
    end
    if options.pickup and not state.sent_pickup then
      action.pickupPriorities, state.sent_pickup = options.pickup, true
    end
    if options.decorate then options.decorate(obs, action, ctx) end
    arena.note_action(state.unstick, action)
    return action
  end
end

function arena.run(config)
  assert(type(config.decide) == "function", "config.decide must be a function")
  local retry_delay = 1
  while true do
    local ok, failure = pcall(function()
      local ws = connect(config)
      retry_delay = 1
      local sequence = 0
      while true do
        local payload = assert(ws:receive(35))
        local observation, _, decode_error = json.decode(payload)
        assert(observation, decode_error)
        if observation.type == "observation" then
          if cached_layout.match_id ~= observation.matchId then
            cached_layout = { match_id = observation.matchId, obstacles = {}, hazards = {} }
          end
          if observation.obstacles ~= nil then cached_layout.obstacles = observation.obstacles end
          if observation.hazards ~= nil then cached_layout.hazards = observation.hazards end
          observation.obstacles = cached_layout.obstacles
          observation.hazards = cached_layout.hazards
          if observation.version == 4 then
            for _, key in ipairs({"sites", "transit"}) do
              if observation[key] ~= nil then cached_layout[key] = observation[key] end
              observation[key] = cached_layout[key] or {}
            end
          end
          latest_observation = observation
          local started = os.clock()
          -- A player script bug must not masquerade as a lost WebSocket or
          -- immediately kill the robot. Keep the connection alive, submit a
          -- safe no-op, and expose the Lua error through normal telemetry.
          local decided, action_or_error = pcall(config.decide, observation)
          local action
          if decided then
            action = action_or_error or arena.action()
          else
            action = arena.action({ logs = { "script error: " .. tostring(action_or_error) } })
          end
          if observation.version == 4 then
            sequence = sequence + 1
            if not decided then
              io.stderr:write("script error: " .. tostring(action_or_error) .. "\n")
              action = { label = "SCRIPT_ERROR", brake = true }
            end
            action = { type = "action", version = 4, sdkVersion = arena.VERSION,
              sequence = sequence, observedTick = observation.tick, geometryRevision = observation.revision, action = action }
          else
            action.type = "action"
            action.requestId = observation.requestId
            action.computeMs = (os.clock() - started) * 1000
          end
          assert(ws:send(json.encode(action), "text", 2))
        end
      end
    end)
    if not ok then
      io.stderr:write("robot connection lost: " .. tostring(failure) .. "\n")
      cqueues.sleep(retry_delay)
      retry_delay = math.min(retry_delay * 2, 15)
    end
  end
end

return arena
