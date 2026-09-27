-- Zone Runner: a modular strategy for the endless arena. It keeps a small
-- state machine on top of arena.tactics:
--   brain/sense.lua  turns an observation into facts (zone, targets, health)
--   brain/plan.lua   picks the state from those facts, highest priority first
--   brain/act.lua    what each state does (or nil to let arena.tactics fight,
--                    loot, and hold the Uplink)
--   brain/fsm.lua    the tiny state machine that ties them together
-- Loadout: generalist, plasma, reinforced_plating, cooling_system.
local arena = require "arena"
local fsm = require "brain.fsm"
local sense = require "brain.sense"
local plan = require "brain.plan"
local act = require "brain.act"

local memory = { fight = {} }
local machine = fsm.new(act.states(memory), "tactics")

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = arena.tactics({
    name = "ZR", range = 850, retreat_hp = 0.3,
    first = function(obs)
      local facts = sense.read(obs)
      machine:go(plan.next(facts, machine), obs.tick)
      arena.draw.text(obs.self.x, obs.self.y - 60, machine.current, "#f5f0a0")
      return machine:run(obs, facts)
    end,
  }),
})
