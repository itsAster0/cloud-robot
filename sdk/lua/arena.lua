local websocket = require "http.websocket"
local cqueues = require "cqueues"
local json = require "dkjson"

local arena = { VERSION = "0.2.0" }
local pickup_config = { auto_pickup = true, pickup_types = {} }
local latest_observation

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
  return {
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
          latest_observation = observation
          local started = os.clock()
          local action = config.decide(observation) or arena.action()
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
