-- Scout: fast site-to-site recon for large maps. Cruises between transit
-- stations, pulses scan on cooldown, relays positions to the squad channel,
-- and only fights when cornered. Loadout: scout, machine_gun, optics,
-- mobility_tuning, cloak_emitter.
local arena = require "arena"
local state = { route = nil, stuck = arena.stuck_tracker(), scan_at = 0 }

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(obs)
    assert(obs.version == 4, "scout requires a v4 arena")
    local self = obs.self
    local enemy, distance = arena.nearest_enemy(obs)
    if enemy and distance < 220 then
      local away = { x = self.x * 2 - enemy.x, y = self.y * 2 - enemy.y }
      return arena.drive_to(obs, away, { aim = arena.bearing(self, enemy), fire = true, label = "SCOUT_BREAK" })
    end
    if arena.danger_level(obs) > 0 then
      local dodge = arena.dodge(obs)
      if dodge then return arena.drive_to(obs, dodge, { label = "SCOUT_DODGE" }) end
    end
    if (self.cooldowns or {}).scan == nil and obs.tick >= state.scan_at then
      state.scan_at = obs.tick + 100
      return arena.control({ scan = true, throttle = 0.4, label = "SCOUT_SWEEP" })
    end
    local goal = arena.safe_zone_goal(obs)
    if not enemy then
      local route = arena.transit_route(obs, goal)
      if route.entry and arena.distance(self, route.entry) < 60 then
        return arena.control({ transit = route.entry.id, brake = true, label = "SCOUT_TRANSIT" })
      end
      if route.entry then goal = route.entry end
    end
    if not state.route or state.route.revision ~= obs.revision or arena.is_stuck(obs, state.stuck) then
      state.route = arena.begin_path(obs, goal)
    end
    arena.advance_path(state.route, 64)
    local action = arena.follow_path(obs, state.route)
    action.cruise = true
    action.message = ("scout %d,%d"):format(math.floor(self.x), math.floor(self.y))
    action.label = "SCOUT_CRUISE"
    return action
  end,
})
