-- Sentinel: area-denial anchor. Holds the safe-zone goal, keeps shields
-- prioritized, lays mines at close range, scans on cooldown, strafes
-- mid-range duels, and never chases past its post. Loadout: heavy, shotgun,
-- reinforced_plating, shield_reservoir, mine_dispenser.
local arena = require "arena"
local state = { scan_at = 0, strafe = 1, flip_at = 0 }

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(obs)
    assert(obs.version == 4, "sentinel requires a v4 arena")
    local self = obs.self
    local enemy, d = arena.nearest_enemy(obs)
    if obs.tick >= state.flip_at then
      state.strafe = -state.strafe
      state.flip_at = obs.tick + 80
    end
    if obs.tick >= state.scan_at then
      state.scan_at = obs.tick + 120
      return arena.control({ scan = true, brake = true, pickupPriorities = { "shield_cell", "repair_pack", "medkit" }, label = "SENTINEL_SWEEP" })
    end
    if enemy and d < 420 then
      local aim = arena.bearing(self, enemy)
      if d > 200 then
        return arena.drive_to(obs, enemy, { aim = aim, fire = arena.weapon_ready(obs), label = "SENTINEL_MEET" })
      end
      if d < 160 then
        local action = arena.control({ brake = true, aim = aim, fire = arena.weapon_ready(obs), label = "SENTINEL_HOLD" })
        action.utility = "mine_dispenser"
        return action
      end
      return arena.strafe_around(obs, enemy, state.strafe)
    end
    local shield = arena.find_loot(obs, "shield_cell")
    if shield and (self.shield or 0) < 20 then
      return arena.drive_to(obs, shield, { label = "SENTINEL_SHIELD" })
    end
    return arena.drive_to(obs, arena.safe_zone_goal(obs), { label = "SENTINEL_POST" })
  end,
})
