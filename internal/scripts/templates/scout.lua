-- Scout: fast recon. Scans constantly, rides transit toward distant patrol
-- goals, reports contacts to its squad, fights at long machine-gun range, and
-- breaks off early. Loadout: scout, machine_gun, optics, mobility_tuning.
local arena = require "arena"
local reported = -100
arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = arena.tactics({
    name = "SCOUT", scan = "always", preferred = 380, retreat_hp = 0.45,
    idle = function(obs, ctx)
      local goal = arena.patrol(obs, ctx.state.patrol)
      local route = arena.transit_route(obs, goal)
      if not route.entry then return goal end
      if arena.distance(obs.self, route.entry) < 30 then
        return arena.control({ brake = true, transit = route.entry.id, label = "SCOUT_RIDE" })
      end
      return ctx.travel(obs, route.entry, "TRANSIT")
    end,
    decorate = function(obs, action)
      local enemy = arena.best_target(obs, { range = 1200 })
      if enemy and obs.tick - reported >= 40 then
        reported = obs.tick
        action.message = string.format("TARGET %s %.0f %.0f", enemy.robotId, enemy.x, enemy.y)
      end
    end,
  }),
})
