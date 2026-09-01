local arena = require "arena"

-- Keeps the enemy at mid range: closes when far, backs off when crowded,
-- and always aims at the target while inside firing range.
local function decide(observation)
  local enemy, distance = arena.nearest_enemy(observation)
  if not enemy then
    return arena.action({ turn = 9 })
  end
  if distance > 60 then
    return arena.approach(observation, enemy, 5)
  end
  if distance < 18 then
    return arena.fire_at(enemy, { move = -4 })
  end
  return arena.fire_at(enemy)
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
