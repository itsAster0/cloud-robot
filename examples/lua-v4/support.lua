-- Support: trails the nearest teammate, drops repair fields when anyone
-- nearby is hurt, screens retreats with smoke, and relays squad reports.
-- Without teammates it patrols like the balanced robot.
-- Loadout: generalist, plasma, capacitor, repair_field, smoke_projector.
local arena = require "arena"
arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = arena.tactics({
    name = "SUPPORT", preferred = 420,
    idle = function(obs, ctx)
      local ally = arena.nearest_ally(obs)
      if ally and arena.distance(obs.self, ally) > 140 then return ctx.travel(obs, ally, "TRAIL") end
    end,
    decorate = function(obs, action)
      local hurt = obs.self.hp < obs.self.maxHp * 0.7
      for _, ally in ipairs(arena.teammates(obs)) do
        if ally.hp and ally.hp < 70 and arena.distance(obs.self, ally) < 120 then hurt = true end
      end
      if hurt then action.utility = "repair_field" end
      if obs.self.hp < obs.self.maxHp * 0.4 and arena.best_target(obs) then action.utility = "smoke_projector" end
      for _, message in ipairs(obs.messages or {}) do
        if message:match("^TARGET") then action.message = message; break end
      end
    end,
  }),
})
