-- Sniper: long-range control. Keeps 600-950 units, stops to take steady
-- led shots from there, re-ranges when enemies close in, and cloaks when
-- hurt. Loadout: generalist, railgun, optics, cooling_system, cloak_emitter.
local arena = require "arena"
arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = arena.tactics({
    name = "SNIPER", range = 1000, preferred = 750, retreat_hp = 0.4,
    pickup = { "optics", "cooling_system", "energy_cell" },
    on_enemy = function(obs, target)
      local d = arena.distance(obs.self, target)
      if d >= 600 and d <= 950 and obs.self.hp >= obs.self.maxHp * 0.4 then
        local lead = arena.aim_predict(obs.self, target, arena.PROJECTILE_SPEED.railgun, target.vx, target.vy)
        return arena.control({ brake = true, aim = arena.bearing(obs.self, lead), fire = arena.weapon_ready(obs) ~= false, label = "SNIPER_HOLD" })
      end
    end,
    decorate = function(obs, action)
      if obs.self.hp < obs.self.maxHp * 0.4 and arena.best_target(obs, { range = 1000 }) then action.utility = "cloak_emitter" end
    end,
  }),
})
