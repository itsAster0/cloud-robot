-- State selection. Rules are checked in order; the first that matches wins.
-- A state is kept for at least MIN_TICKS so the robot does not flicker
-- between two goals every decision.
local M = {}
local MIN_TICKS = 20

M.rules = {
  { state = "escape", when = function(f) return f.zone and not f.inside end },
  { state = "heal", when = function(f) return f.hp < 0.4 and f.heal_slot ~= nil and not f.target end },
  { state = "drift", when = function(f) return f.next and not f.inside_next and f.arrives_in < 500 and not f.target end },
  { state = "bounty", when = function(f) return f.bounty and f.hp > 0.5 end },
}

function M.next(facts, machine)
  local wanted = "tactics"
  for _, rule in ipairs(M.rules) do
    if rule.when(facts) then wanted = rule.state; break end
  end
  -- Escaping the storm and healing are urgent; everything else waits out
  -- the minimum time in the current state.
  local urgent = wanted == "escape" or wanted == "heal"
  if not urgent and machine:age(facts.tick) < MIN_TICKS then return machine.current end
  return wanted
end

return M
