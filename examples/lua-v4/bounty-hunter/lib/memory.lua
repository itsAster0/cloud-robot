-- Contact memory keyed by robot id: position, velocity, bounty, and the
-- tick it was last seen. Entries older than `ttl` ticks are dropped.
local M = {}

function M.new(ttl) return { ttl = ttl or 200, robots = {} } end

function M.update(memory, obs)
  for _, robot in ipairs(obs.robots or {}) do
    if robot.alive ~= false and robot.team ~= obs.self.team then
      memory.robots[robot.robotId] = {
        robotId = robot.robotId, x = robot.x, y = robot.y, vx = robot.vx or 0, vy = robot.vy or 0,
        hp = robot.hp, bounty = robot.bounty or 0, seen = obs.tick,
      }
    elseif robot.alive == false then
      memory.robots[robot.robotId] = nil
    end
  end
  for id, entry in pairs(memory.robots) do
    if obs.tick - entry.seen > memory.ttl then memory.robots[id] = nil end
  end
end

-- Where a remembered robot probably is now, dead-reckoned from its last
-- velocity for at most `cap` ticks (robots turn; guesses decay fast).
function M.predict(entry, tick, cap)
  local dt = math.min(tick - entry.seen, cap or 40) / 20
  return { x = entry.x + entry.vx * dt, y = entry.y + entry.vy * dt }
end

return M
