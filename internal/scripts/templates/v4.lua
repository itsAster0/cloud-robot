-- Balanced: fight at weapon range, retreat to cover when hurt, heal and loot
-- when safe, hunt remembered contacts, and patrol sites inside the zone.
-- Loadout: generalist, plasma.
local arena = require "arena"
arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")), token = assert(os.getenv("ROBOT_TOKEN")),
  decide = arena.tactics({}),
})
