local websocket = require "http.websocket"
local cqueues = require "cqueues"
local json = require "dkjson"

local arena = { VERSION = "0.3.2" }
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

function arena.line_of_sight(x1, y1, x2, y2, obstacles)
  obstacles = obstacles or (latest_observation and latest_observation.obstacles) or {}
  for _, obstacle in ipairs(obstacles) do
    local blocked
    if obstacle.shape == "circle" or obstacle.radius ~= nil then
      blocked = segment_intersects_circle(x1, y1, x2, y2, obstacle)
    else
      blocked = segment_intersects_aabb(x1, y1, x2, y2, obstacle)
    end
    if blocked then return false end
  end
  return true
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

local function point_blocked(x, y, obstacles)
  for _, obstacle in ipairs(obstacles) do
    if obstacle.shape == "circle" or obstacle.radius ~= nil then
      if (x - obstacle.x) ^ 2 + (y - obstacle.y) ^ 2 <= (obstacle.radius or 0) ^ 2 then return true end
    elseif x >= obstacle.x and x <= obstacle.x + (obstacle.width or 0) and y >= obstacle.y and y <= obstacle.y + (obstacle.height or 0) then
      return true
    end
  end
  return false
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
  local ws = websocket.new_from_uri(assert(config.url, "arena URL is required"), { "robot-arena.v1" })
  ws.request.headers:upsert("authorization", "Bearer " .. assert(config.token, "robot token is required"), true)
  assert(ws:connect(10))
  return ws
end

function arena.run(config)
  assert(type(config.decide) == "function", "config.decide must be a function")
  local retry_delay = 1
  while true do
    local ok, failure = pcall(function()
      local ws = connect(config)
      retry_delay = 1
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
          action.type = "action"
          action.requestId = observation.requestId
          action.computeMs = (os.clock() - started) * 1000
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
