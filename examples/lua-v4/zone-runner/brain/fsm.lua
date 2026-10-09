-- A tiny finite-state machine. Each state is a table with a required
-- run(obs, facts, machine) and optional enter(machine) / exit(machine).
-- run returns an action, a goal point { x, y }, or nil to fall through.
local M = {}
M.__index = M

function M.new(states, initial)
  return setmetatable({ states = states, current = initial, since = 0 }, M)
end

function M:go(name, tick)
  if name == self.current or not self.states[name] then return end
  local old = self.states[self.current]
  if old and old.exit then old.exit(self) end
  self.current, self.since = name, tick
  local new = self.states[name]
  if new.enter then new.enter(self) end
end

-- Ticks spent in the current state, for timeouts and hysteresis.
function M:age(tick) return tick - self.since end

function M:run(obs, facts) return self.states[self.current].run(obs, facts, self) end

return M
