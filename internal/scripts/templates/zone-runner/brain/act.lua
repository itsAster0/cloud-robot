-- What each state does. Goal points are travelled by arena.tactics with
-- pathing and unstick; returning nil hands the decision back to tactics.
local arena = require "arena"
local M = {}

function M.states(memory)
  return {
    -- Outside the safe zone: head for its centre, shooting on the way.
    escape = { run = function(_, f) return { x = f.zone.x, y = f.zone.y } end },

    -- The zone is about to move and we are not in the next circle yet:
    -- wait in the overlap of the current and next zone.
    drift = { run = function(_, f) return f.lens end },

    -- Low health and nothing to shoot: use the best healing item.
    heal = { run = function(_, f) return arena.control({ consume = f.heal_slot, brake = true, label = "ZR_HEAL" }) end },

    -- Chase the richest bounty in sight with the SDK's engagement helper
    -- (strafe inside the weapon's range, lead moving targets).
    bounty = {
      enter = function() memory.fight = {} end,
      run = function(obs, f)
        if not f.bounty then return nil end
        return arena.engage(obs, f.bounty, memory.fight, { label = "ZR_BOUNTY" })
      end,
    },

    -- Default: arena.tactics fights, loots, and contests the Uplink.
    tactics = { run = function() return nil end },
  }
end

return M
