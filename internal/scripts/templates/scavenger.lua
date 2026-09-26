-- Scavenger: loot-first winner on dense maps. Sets pickup priorities,
-- explicitly equips weapon upgrades from containers, rides transit between
-- rich sites, strafes close fights, and dodges focused fire. Loadout: scout,
-- shotgun, mobility_tuning, capacitor.
local arena = require "arena"
local state = { route = nil, stuck = arena.stuck_tracker(), strafe = 1, flip_at = 0 }
local PRIORITIES = { "weapon:railgun", "weapon:shotgun", "shield_cell", "repair_pack", "medkit", "energy_cell" }

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(obs)
    assert(obs.version == 4, "scavenger requires a v4 arena")
    local self = obs.self
    if obs.tick >= state.flip_at then
      state.strafe = -state.strafe
      state.flip_at = obs.tick + 60
    end
    local enemy, d = arena.nearest_enemy(obs)
    if enemy and d < 300 then
      if arena.danger_level(obs) > 0 then
        local dodge = arena.dodge(obs)
        if dodge then return arena.drive_to(obs, dodge, { aim = arena.bearing(self, enemy), fire = true, label = "SCAV_DODGE" }) end
      end
      if d < 200 then return arena.strafe_around(obs, enemy, state.strafe) end
      local lead = arena.lead_for(obs, enemy)
      return arena.drive_to(obs, lead, { aim = arena.bearing(self, lead), fire = arena.weapon_ready(obs), label = "SCAV_FIGHT" })
    end
    local loot = arena.nearest_item(obs)
    if loot and arena.distance(self, loot) < 40 then
      local upgrade
      for _, stack in ipairs(loot.contents or {}) do
        if stack.kind and stack.kind:find("^weapon:") then upgrade = stack.kind break end
      end
      if upgrade then
        return arena.control({ brake = true, equip = { container = loot.itemId, kind = upgrade, slot = 0 }, label = "SCAV_EQUIP" })
      end
      return arena.control({ brake = true, pickup = loot.itemId, pickupPriorities = PRIORITIES, label = "SCAV_GRAB" })
    end
    local goal = loot or arena.safe_zone_goal(obs)
    if not enemy then
      local route = arena.transit_route(obs, goal)
      if route.entry and arena.distance(self, route.entry) < 60 then
        return arena.control({ transit = route.entry.id, brake = true, label = "SCAV_RIDE" })
      end
      if route.entry then goal = route.entry end
    end
    if not state.route or state.route.revision ~= obs.revision or arena.is_stuck(obs, state.stuck) then
      state.route = arena.begin_path(obs, goal)
    end
    arena.advance_path(state.route, 64)
    local action = arena.follow_path(obs, state.route)
    action.pickupPriorities = PRIORITIES
    action.label = "SCAV_ROUTE"
    return action
  end,
})
