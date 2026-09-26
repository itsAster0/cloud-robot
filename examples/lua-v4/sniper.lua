-- Sniper: long-range railgun control. Holds 600-900 units, brakes to aim
-- with predicted lead, dodges incoming fire, cloaks to break contact, and
-- rides loot routes toward optics caches. Loadout: generalist, railgun,
-- optics, cooling_system.
local arena = require "arena"
local state = { route = nil, stuck = arena.stuck_tracker() }

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(obs)
    assert(obs.version == 4, "sniper requires a v4 arena")
    local self = obs.self
    local enemy, d = arena.nearest_enemy(obs)
    if enemy then
      local lead = arena.lead_for(obs, enemy, 0)
      local aim = arena.bearing(self, lead)
      if d < 450 then
        local away = { x = self.x * 2 - enemy.x, y = self.y * 2 - enemy.y }
        local action = arena.drive_to(obs, away, { aim = aim, fire = false, label = "SNIPER_RERANGE" })
        action.utility = "cloak_emitter"
        return action
      end
      if arena.danger_level(obs) > 0 then
        local dodge = arena.dodge(obs)
        if dodge then return arena.drive_to(obs, dodge, { aim = aim, fire = false, label = "SNIPER_DODGE" }) end
      end
      if d > 950 then
        return arena.drive_to(obs, lead, { aim = aim, fire = false, label = "SNIPER_CLOSE" })
      end
      return arena.control({ brake = true, aim = aim, fire = arena.weapon_ready(obs), label = "SNIPER_HOLD" })
    end
    local optics = arena.find_loot(obs, "optics")
    local goal = optics or arena.safe_zone_goal(obs)
    if not state.route or state.route.revision ~= obs.revision or arena.is_stuck(obs, state.stuck) then
      state.route = arena.begin_path(obs, goal)
    end
    arena.advance_path(state.route, 64)
    local action = arena.follow_path(obs, state.route)
    if optics then action.pickupPriorities = { "optics", "cooling_system", "energy_cell" } end
    return action
  end,
})
