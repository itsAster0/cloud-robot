local arena = require "arena"
local state = { route = nil, stuck = arena.stuck_tracker() }
arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(obs)
    assert(obs.version == 4, "This strategy requires a v4 arena")
    local enemy, distance = arena.nearest_enemy(obs)
    if enemy then
      local aim = arena.bearing(obs.self, enemy)
      if obs.self.hp < obs.self.maxHp * .3 then
        local retreat = { x = obs.self.x * 2 - enemy.x, y = obs.self.y * 2 - enemy.y }
        local action = arena.drive_to(obs, retreat, { aim = aim, fire = true, label = "RETREAT_LOW_HP" })
        action.dash = obs.self.energy >= 30
        return action
      end
      return arena.drive_to(obs, enemy, { aim = aim, fire = arena.weapon_ready(obs), label = "ENGAGE" })
    end
    local heal = arena.find_consumable(obs, "medkit") or arena.find_consumable(obs, "repair_pack")
    if heal and obs.self.hp < obs.self.maxHp * .6 then return arena.control({ brake = true, consume = heal, label = "REPAIR" }) end
    local loot, loot_distance = arena.nearest_item(obs)
    if loot and loot_distance < 35 then return arena.control({ brake = true, pickup = loot.itemId, label = "SCAVENGE" }) end
    local goal = loot or arena.safe_zone_goal(obs)
    if not state.route or state.route.revision ~= obs.revision or arena.is_stuck(obs, state.stuck) then state.route = arena.begin_path(obs, goal) end
    arena.advance_path(state.route, 64)
    local action = arena.follow_path(obs, state.route)
    if action.label == "ARRIVED" or state.route.status == "unreachable" then state.route = nil end
    return action
  end,
})
