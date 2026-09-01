local arena = require "arena"

local function decide(observation)
  local enemy = arena.nearest_enemy(observation)
  if not enemy then
    return arena.action({ turn = 12, logs = { "scanning" } })
  end
  return arena.approach(observation, enemy, 8)
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})

