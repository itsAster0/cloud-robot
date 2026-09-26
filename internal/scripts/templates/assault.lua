-- Assault: balanced brawler for mid-size fights. Leads shots with
-- velocity prediction, strafes orbits up close, dodges incoming fire,
-- retreats and dashes at low HP, channels heals, scavenges nearby loot.
-- Loadout: generalist, machine_gun, reinforced_plating, cooling_system,
-- mine_dispenser.
local arena = require "arena"
local state = { route = nil, stuck = arena.stuck_tracker(), strafe = 1, flip_at = 0 }

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(obs)
    assert(obs.version == 4, "assault requires a v4 arena")
    local self = obs.self
    local enemy, distance = arena.nearest_enemy(obs)
    if enemy then
      if obs.tick >= state.flip_at then
        state.strafe = -state.strafe
        state.flip_at = obs.tick + 60
      end
      if arena.danger_level(obs) > 0 and self.hp < self.maxHp * 0.6 then
        local dodge = arena.dodge(obs)
        if dodge then return arena.drive_to(obs, dodge, { aim = arena.bearing(self, enemy), fire = true, label = "ASSAULT_DODGE" }) end
      end
      if self.hp < self.maxHp * 0.35 then
        local away = { x = self.x * 2 - enemy.x, y = self.y * 2 - enemy.y }
        local action = arena.drive_to(obs, away, { aim = arena.bearing(self, enemy), fire = true, label = "ASSAULT_RETREAT" })
        action.dash = self.energy >= 30
        return action
      end
      if distance < 320 then
        return arena.strafe_around(obs, enemy, state.strafe)
      end
      local lead = arena.lead_for(obs, enemy)
      return arena.drive_to(obs, lead, { aim = arena.bearing(self, lead), fire = arena.weapon_ready(obs), label = "ASSAULT_ENGAGE" })
    end
    local heal = arena.find_consumable(obs, "medkit") or arena.find_consumable(obs, "repair_pack")
    if heal and self.hp < self.maxHp * 0.7 then
      return arena.control({ brake = true, consume = heal, label = "ASSAULT_REPAIR" })
    end
    local loot, d = arena.nearest_item(obs)
    if loot and d < 40 then
      return arena.control({ brake = true, pickup = loot.itemId, label = "ASSAULT_SCAVENGE" })
    end
    local goal = loot or arena.safe_zone_goal(obs)
    if not state.route or state.route.revision ~= obs.revision or arena.is_stuck(obs, state.stuck) then
      state.route = arena.begin_path(obs, goal)
    end
    arena.advance_path(state.route, 64)
    return arena.follow_path(obs, state.route)
  end,
})
