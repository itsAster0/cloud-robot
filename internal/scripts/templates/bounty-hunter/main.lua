-- Bounty Hunter: tracks every robot on a kill streak, remembers where it
-- was last seen, and hunts the most valuable one. Split into modules:
--   lib/memory.lua  last-seen positions and velocities, forgotten after 10 s
--   lib/pick.lua    scores targets: bounty, damage already done, distance
--   lib/aim.lua     leads moving targets and holds fire when it would miss
-- Loadout: scout, railgun, optics, mobility_tuning.
local arena = require "arena"
local memory = require "lib.memory"
local pick = require "lib.pick"
local aim = require "lib.aim"

local seen = memory.new(200)
local fight = {}

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = arena.tactics({
    name = "BH", range = 1000, preferred = 650, retreat_hp = 0.35, uplink = false,
    first = function(obs)
      memory.update(seen, obs)
      return nil
    end,
    -- Fight our chosen target rather than simply the closest one.
    on_enemy = function(obs, closest)
      local target = pick.best(obs, seen) or closest
      local action = arena.engage(obs, target, fight, { label = "BH_" .. (target.bounty and target.bounty > 0 and "BOUNTY" or "FIGHT") })
      return aim.refine(obs, target, action)
    end,
    -- Nothing in sight: go where the richest remembered bounty was heading,
    -- or contest the Uplink, or patrol.
    idle = function(obs)
      local lead = pick.remembered(obs, seen)
      if lead then
        arena.draw.text(lead.x, lead.y - 40, "$" .. lead.bounty, "#ffc857")
        return memory.predict(lead, obs.tick, 40)
      end
      return arena.uplink(obs)
    end,
  }),
})
