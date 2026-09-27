-- Target choice. Higher score wins; ties break on robot id so the choice is
-- stable between decisions.
local arena = require "arena"
local M = {}

local function score(obs, robot)
  local d = arena.distance(obs.self, robot)
  local bounty = robot.bounty or 0
  local weak = 1 - math.min(1, (robot.hp or 100) / 100)
  return bounty * 2 + weak * 120 - d * 0.15
end

-- Best target in line of fire now, or nil.
function M.best(obs, _)
  local best, best_score
  for _, robot in ipairs(obs.robots or {}) do
    if robot.alive ~= false and robot.team ~= obs.self.team and not robot.protected and arena.can_see(obs, robot) then
      local s = score(obs, robot)
      if not best_score or s > best_score or (s == best_score and robot.robotId < best.robotId) then best, best_score = robot, s end
    end
  end
  return best
end

-- The most valuable bounty we remember but cannot see.
function M.remembered(obs, memory)
  local best
  for _, entry in pairs(memory.robots) do
    if entry.bounty > 0 and entry.seen < obs.tick and (not best or entry.bounty > best.bounty) then best = entry end
  end
  return best
end

return M
