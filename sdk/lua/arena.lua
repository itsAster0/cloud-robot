local websocket = require "http.websocket"
local cqueues = require "cqueues"
local json = require "dkjson"

local arena = { VERSION = "0.1.0" }

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

