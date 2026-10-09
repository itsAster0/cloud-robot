-- Scavenger: loot first. Detours far for containers, auto-picks weapons and
-- supplies by priority, rides transit between sites, then fights with what
-- it found. Loadout: scout, machine_gun, mobility_tuning.
local arena = require "arena"
local PRIORITIES = { "weapon:railgun", "weapon:shotgun", "shield_cell", "repair_pack", "medkit", "energy_cell" }
arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = arena.tactics({
    name = "SCAV", loot_reach = { 600, 400 }, pickup = PRIORITIES,
    idle = function(obs, ctx)
      local goal = arena.patrol(obs, ctx.state.patrol)
      local route = arena.transit_route(obs, goal)
      if route.entry and arena.distance(obs.self, route.entry) < 30 then
        return arena.control({ brake = true, transit = route.entry.id, label = "SCAV_RIDE" })
      end
      return route.entry and { x = route.entry.x, y = route.entry.y } or goal
    end,
    decorate = function(obs, action)
      -- Switch to a picked-up second weapon when the first overheats.
      local weapons = obs.self.weapons or {}
      local active = weapons[(obs.self.activeWeapon or 0) + 1]
      if #weapons == 2 and active and active.overheated then action.weapon = 1 - (obs.self.activeWeapon or 0) end
    end,
  }),
})
