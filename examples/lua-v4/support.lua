-- Support: squad medic and screen. Trails the nearest ally, drops a repair
-- field when allies are hurt, pops smoke when enemies close, dodges focused
-- fire, and relays state on the squad channel. Loadout: generalist, plasma,
-- capacitor, repair_field + smoke_projector.
local arena = require "arena"

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(obs)
    assert(obs.version == 4, "support requires a v4 arena")
    local ally, ad = arena.nearest_ally(obs)
    local enemy, ed = arena.nearest_enemy(obs)
    if ally and (ally.hp or 100) < 70 and ad < 300 then
      return arena.control({ brake = true, utility = "repair_field", label = "SUPPORT_FIELD", message = "field up" })
    end
    if arena.danger_level(obs) >= 2 then
      local dodge = arena.dodge(obs)
      if dodge then return arena.drive_to(obs, dodge, { label = "SUPPORT_DODGE", message = "taking fire" }) end
    end
    if enemy and ed < 260 then
      return arena.control({ utility = "smoke_projector", throttle = 0.6, aim = arena.bearing(obs.self, enemy), label = "SUPPORT_SCREEN" })
    end
    if ally and ad > 160 then
      return arena.drive_to(obs, ally, { label = "SUPPORT_TRAIL", message = "on you" })
    end
    return arena.drive_to(obs, arena.safe_zone_goal(obs), { label = "SUPPORT_POST" })
  end,
})
