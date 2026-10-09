-- Sentinel: zone denial. Holds a post at the site nearest the safe-zone
-- centre, sweeps with scans, mines approaches to its post, and prefers
-- shields. Loadout: heavy, cannon, shield_reservoir, mine_dispenser.
local arena = require "arena"
arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = arena.tactics({
    name = "SENTINEL", scan = "always", pickup = { "shield_cell", "repair_pack", "medkit" },
    idle = function(obs)
      local zone = obs.zone or arena.safe_zone_goal(obs)
      local post, best
      for _, site in ipairs(obs.sites or {}) do
        local d = arena.distance(site, zone)
        if not best or d < best then post, best = site, d end
      end
      post = post or zone
      -- Walk a slow beat around the post so the robot is never parked.
      local angle = math.floor(obs.tick / 120) * 1.9
      return { x = post.x + math.cos(angle) * 180, y = post.y + math.sin(angle) * 180 }
    end,
    decorate = function(obs, action)
      local enemy = arena.best_target(obs, { range = 350 })
      if enemy then action.utility = "mine_dispenser" end
    end,
  }),
})
