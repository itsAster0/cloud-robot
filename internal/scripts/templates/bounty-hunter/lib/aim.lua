-- Fire discipline for a slow-firing weapon: hold the shot while the turret
-- is far off the lead point, so the railgun's cooldown is not wasted.
local arena = require "arena"
local M = {}

local TOLERANCE = 6 -- degrees

function M.refine(obs, target, action)
  if not action or not action.fire or not action.aim then return action end
  local turret = obs.self.turretHeading or obs.self.heading
  local off = math.abs((action.aim - turret + 180) % 360 - 180)
  local d = arena.distance(obs.self, target)
  -- Close targets fill more of the view, so allow a wider error.
  local allowed = TOLERANCE + math.max(0, 300 - d) / 40
  if off > allowed then action.fire = false end
  return action
end

return M
