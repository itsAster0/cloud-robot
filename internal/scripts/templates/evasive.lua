local arena = require "arena"

local function decide(observation)
  local enemy = arena.nearest_enemy(observation)
  if not enemy then
    return arena.action({ move = 4, turn = -10 })
  end
  return arena.strafe(observation, enemy, observation.self.hp % 2 == 0 and 1 or -1)
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
