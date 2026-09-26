-- Pure-Lua tests for SDK geometry and tactics helpers. Network modules are
-- stubbed; run with `lua sdk/lua/arena_test.lua` from the repository root.
package.preload["http.websocket"] = function() return {} end
package.preload["cqueues"] = function() return {} end
package.preload["dkjson"] = function() return {} end
package.path = "sdk/lua/?.lua;" .. package.path
local arena = require "arena"

local failures = 0
local function check(name, ok, detail)
  if ok then
    print("ok   " .. name)
  else
    failures = failures + 1
    print("FAIL " .. name .. (detail and (": " .. detail) or ""))
  end
end

-- Deterministic LCG so the random fixtures never depend on wall-clock time.
local state = 12345
local function rand()
  state = (state * 48271) % 2147483647
  return state / 2147483647
end

local obstacles = {}
for i = 1, 6000 do
  obstacles[i] = { id = "o" .. i, shape = "aabb", x = rand() * 42000, y = rand() * 26250,
    width = 20 + rand() * 200, height = 20 + rand() * 200 }
end
-- Brute force: chunks below the index threshold take the linear path.
local function brute_los(x1, y1, x2, y2)
  for start = 1, #obstacles, 40 do
    local chunk = {}
    for i = start, math.min(start + 39, #obstacles) do chunk[#chunk + 1] = obstacles[i] end
    if not arena.line_of_sight(x1, y1, x2, y2, chunk) then return false end
  end
  return true
end
local mismatches = 0
for _ = 1, 300 do
  local x1, y1 = rand() * 42000, rand() * 26250
  local angle, length = rand() * 2 * math.pi, rand() * 2500
  local x2, y2 = x1 + math.cos(angle) * length, y1 + math.sin(angle) * length
  if arena.line_of_sight(x1, y1, x2, y2, obstacles) ~= brute_los(x1, y1, x2, y2) then mismatches = mismatches + 1 end
end
check("indexed line_of_sight matches brute force", mismatches == 0, mismatches .. " mismatches")

local started = os.clock()
for _ = 1, 5000 do
  local x1, y1 = rand() * 42000, rand() * 26250
  arena.line_of_sight(x1, y1, x1 + 600, y1 + 300, obstacles)
end
local elapsed = os.clock() - started
check("5000 indexed sight checks on 6000 obstacles stay fast", elapsed < 1.0, string.format("%.3fs", elapsed))

local wall = { { id = "wall", shape = "aabb", x = 300, y = -100, width = 40, height = 200 } }
local distance, hit = arena.raycast(0, 0, 0, 1000, wall)
check("raycast stops at the wall face", hit == wall[1] and math.abs(distance - 300) < 1, tostring(distance))
distance, hit = arena.raycast(0, 0, 180, 1000, wall)
check("raycast reports clear rays", hit == nil and distance == 1000)

local obs = {
  tick = 100, tickRate = 20, arenaWidth = 2000, arenaHeight = 2000,
  self = { robotId = "me", team = "red", x = 500, y = 500 },
  obstacles = { { id = "block", shape = "aabb", x = 560, y = 460, width = 80, height = 80 } },
  robots = {
    { robotId = "far", team = "blue", x = 1100, y = 500, hp = 20, alive = true },
    { robotId = "near", team = "blue", x = 500, y = 800, hp = 90, alive = true },
    { robotId = "ally", team = "red", x = 520, y = 520, hp = 10, alive = true },
  },
  sites = { { id = "a", kind = "armoury", biome = "urban", x = 100, y = 100 },
            { id = "b", kind = "bunker", biome = "forest", x = 900, y = 900 } },
}
local threat = { x = 1100, y = 500 }
local spot, cover = arena.find_cover(obs, threat)
check("find_cover returns a hidden spot",
  spot ~= nil and cover == obs.obstacles[1] and not arena.line_of_sight(threat.x, threat.y, spot.x, spot.y, obs.obstacles))
local open_obs = { self = obs.self, obstacles = {}, arenaWidth = 2000, arenaHeight = 2000 }
check("find_cover returns nil in the open", arena.find_cover(open_obs, threat) == nil)

local target = arena.best_target(obs, { range = 1000 })
check("best_target skips blocked and allied robots", target and target.robotId == "near", target and target.robotId)

check("nearest_site filters by biome", arena.nearest_site(obs, { biome = "forest" }).id == "b")
check("nearest_site defaults to closest", arena.nearest_site(obs).id == "a")

local tracker = arena.contact_tracker(40)
arena.update_contacts(tracker, { tick = 10, tickRate = 20, self = obs.self,
  robots = { { robotId = "e1", team = "blue", x = 0, y = 0, vx = 100, vy = 0, hp = 50, alive = true } } })
local later = arena.update_contacts(tracker, { tick = 20, tickRate = 20, self = obs.self, robots = {} })
check("contacts persist out of sight with extrapolation",
  #later == 1 and later[1].age == 10 and not later[1].visible and math.abs(later[1].x - 50) < 1e-9)
local forgotten = arena.update_contacts(tracker, { tick = 60, tickRate = 20, self = obs.self, robots = {} })
check("contacts expire after ttl", #forgotten == 0)

local near = arena.obstacles_near(obs, 500, 500, 100)
check("obstacles_near finds cover within radius", #near == 1 and near[1].id == "block")

local function steer_obs(heading, x, y)
  return { tick = 10, self = { robotId = "me", team = "red", x = x or 0, y = y or 0, heading = heading, hp = 100, maxHp = 100,
    weapons = { { kind = "plasma", heat = 0, readyAt = 0, overheated = false } }, activeWeapon = 0 }, obstacles = {}, robots = {} }
end
local stop = arena.drive_to(steer_obs(0), { x = 20, y = 0 })
local reach_loot = arena.drive_to(steer_obs(0), { x = 30, y = 0 })
check("drive_to keeps driving until inside pickup reach", reach_loot.throttle > 0)
check("drive_to brakes on arrival", stop.brake == true and stop.throttle == 0)
local back = arena.drive_to(steer_obs(0), { x = -200, y = 0 })
check("drive_to reverses toward a near point behind", back.throttle < 0 and math.abs(back.turn) < 0.1)
local ahead = arena.drive_to(steer_obs(0), { x = 900, y = 0 })
check("drive_to drives full speed at a far point ahead", ahead.throttle == 1 and ahead.turn == 0)
local sharp = arena.drive_to(steer_obs(0), { x = 0, y = 900 })
check("drive_to crawls through sharp turns", sharp.throttle > 0 and sharp.throttle < 0.5 and sharp.turn == 1)

local near_goal = steer_obs(0, 0, 0)
local short_route = { status = "ready", revision = nil, path = { { x = 0, y = 0 }, { x = 40, y = 0 } }, obstacles = {} }
local hop = arena.follow_path(near_goal, short_route)
check("follow_path keeps driving to a goal 40 units away", hop.throttle > 0 and hop.label == "FOLLOW_PATH", hop.label)
local walled = steer_obs(0, 0, 0)
walled.arenaWidth, walled.arenaHeight, walled.revision = 2000, 2000, 1
walled.self.x, walled.self.y = 100, 300
walled.obstacles = { { id = "wall", shape = "aabb", x = 200, y = 100, width = 30, height = 400 } }
local around = arena.begin_path(walled, { x = 400, y = 300 })
for _ = 1, 40 do arena.advance_path(around, 128); if around.status ~= "pending" then break end end
local clear_route = around.status == "ready"
for i = 2, clear_route and #around.path or 0 do
  local a, b = around.path[i - 1], around.path[i]
  if not arena.line_of_sight(a.x, a.y, b.x, b.y, walled.obstacles) then clear_route = false end
end
check("begin_path routes around walls instead of through them", clear_route, around.status)
local hugging = steer_obs(90, 300, 186)
hugging.arenaWidth, hugging.arenaHeight, hugging.revision = 2000, 2000, 1
hugging.obstacles = { { id = "hedge", shape = "aabb", x = 200, y = 200, width = 200, height = 30 } }
local escape = arena.begin_path(hugging, { x = 300, y = 400 })
for _ = 1, 40 do arena.advance_path(escape, 128); if escape.status ~= "pending" then break end end
check("begin_path plans from a spot hugging cover", escape.status == "ready", escape.status)
local pinned, escape, started = {}, nil, false
for tick = 1, 12 do
  local o = steer_obs(0, 50, 50); o.tick = tick * 2
  escape, started = arena.unstick(o, pinned)
  if escape then break end
  arena.note_action(pinned, { throttle = 1 })
end
check("unstick backs out after pushing into cover without progress", escape and started and escape.throttle == -1 and escape.label == "UNSTICK")
local resting = {}
for tick = 1, 12 do
  local o = steer_obs(0, 50, 50); o.tick = tick * 2
  escape = arena.unstick(o, resting)
  arena.note_action(resting, { brake = true, throttle = 0 })
end
check("unstick ignores intentional stops", escape == nil)
local fight = {}
local near = arena.engage(steer_obs(0), { robotId = "e", x = 100, y = 0, vx = 0, vy = 0 }, fight)
check("engage kites when too close", near.label == "KITE" and near.fire == true)
local far = arena.engage(steer_obs(0), { robotId = "e", x = 1500, y = 0, vx = 0, vy = 0 }, fight)
check("engage closes in when far", far.label == "CLOSE_IN")
local band = arena.engage(steer_obs(0), { robotId = "e", x = 420, y = 0, vx = 0, vy = 0 }, fight)
check("engage strafes inside its range band", band.label == "STRAFE")
local moving = arena.engage(steer_obs(0), { robotId = "e", x = 420, y = 0, vx = 0, vy = 100 }, fight)
check("engage leads a moving target", moving.aim > 0 and moving.aim < 90, tostring(moving.aim))

local patrol_obs = { tick = 0, self = { robotId = "me", x = 0, y = 0 }, zone = { x = 500, y = 500, radius = 2000 },
  sites = { { x = 100, y = 100 }, { x = 900, y = 900 }, { x = 100, y = 900 } } }
local memory_a, memory_b = {}, {}
local goal_a, goal_b = arena.patrol(patrol_obs, memory_a), arena.patrol(patrol_obs, memory_b)
check("patrol is deterministic", goal_a.x == goal_b.x and goal_a.y == goal_b.y)
patrol_obs.self.x, patrol_obs.self.y = goal_a.x, goal_a.y
local next_goal = arena.patrol(patrol_obs, memory_a)
check("patrol moves on after arrival", arena.distance(next_goal, goal_a) > 1 or memory_a.count == 2)

if failures > 0 then
  print(failures .. " failure(s)")
  os.exit(1)
end
print("all SDK tests passed")
