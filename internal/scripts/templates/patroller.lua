local arena = require "arena"

-- Patrols the four corners of the 800x500 arena until an enemy shows up,
-- then chases and fires like the aggressive bot.
local waypoints = {
  { x = 120, y = 100 },
  { x = 680, y = 100 },
  { x = 680, y = 400 },
  { x = 120, y = 400 },
}
local index = 1

local function decide(observation)
  local enemy = arena.nearest_enemy(observation)
  if enemy then
    return arena.approach(observation, enemy, 6)
  end
  local goal = waypoints[index]
  local dx, dy = goal.x - observation.self.x, goal.y - observation.self.y
  if math.sqrt(dx * dx + dy * dy) < 30 then
    index = index % #waypoints + 1
    goal = waypoints[index]
  end
  return arena.action({ move = 5, target_x = goal.x, target_y = goal.y })
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
