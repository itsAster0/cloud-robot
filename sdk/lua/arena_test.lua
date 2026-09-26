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

if failures > 0 then
  print(failures .. " failure(s)")
  os.exit(1)
end
print("all SDK tests passed")
