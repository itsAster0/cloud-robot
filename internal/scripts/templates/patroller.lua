local arena = require "arena"

-- Sentry: walks a randomized corner loop sized from the actual arena, detours
-- for nearby items, and engages anything it can see. Patrol direction is
-- chosen once per run from the seeded robot ID RNG.

local seed = 0
local robot_id = os.getenv("ROBOT_ID") or "sentry"
for index = 1, #robot_id do
  seed = (seed * 131 + robot_id:byte(index)) % 2147483647
end
math.randomseed(seed)

local PROJECTILE_SPEED = { plasma = 24, cannon = 14, machine_gun = 32, incendiary = 20, cryo = 20, emp = 18, railgun = 0 }
local ITEM_SCORE = {
  heal = 90, ["repair-core"] = 60, shield = 70, overdrive = 65, rapid_fire = 65,
  weapon_railgun = 100, weapon_cannon = 80, weapon_incendiary = 55,
  weapon_cryo = 50, weapon_emp = 45, weapon_machine_gun = 35, weapon_plasma = 30,
}

local state = { index = 1, last_x = nil, last_y = nil, stuck = 0, escape = 0, escape_turn = 0 }
-- Half the sentries walk the loop the other way.
local direction = math.random() < 0.5 and 1 or -1

local function unstick(obs, action)
  local self = obs.self
  if state.last_x then
    local moved = arena.distance({ x = state.last_x, y = state.last_y }, self)
    if action.moving and moved < 1.5 then state.stuck = state.stuck + 1 else state.stuck = 0 end
  end
  state.last_x, state.last_y = self.x, self.y
  if state.stuck > 6 then
    state.stuck, state.escape = 0, 8
    state.escape_turn = (math.random() < 0.5 and -1 or 1) * (10 + math.random() * 8)
  end
  if state.escape > 0 then
    state.escape = state.escape - 1
    return arena.action({ move = -4, turn = state.escape_turn, logs = { "backing off obstacle" } })
  end
  return nil
end

local function navigate(obs, x, y, speed, log)
  local self = obs.self
  local goal = { x = x, y = y }
  if not arena.line_of_sight(self.x, self.y, x, y, obs.obstacles) then
    local detour = arena.path_to(x, y, obs)[1]
    if detour then goal = detour end
  end
  local turn = ((arena.bearing(self, goal) - self.heading + 540) % 360) - 180
  return arena.action({ move = speed, turn = turn, logs = { log or "moving" } })
end

-- Aims where the enemy will be; damped because enemies rarely hold a line.
local function lead_target(obs, enemy)
  local speed = PROJECTILE_SPEED[obs.self.weapon] or 14
  if speed <= 0 then return enemy.x, enemy.y end
  local flight = math.min(arena.distance(obs.self, enemy) / speed, 8)
  local radians = enemy.heading * math.pi / 180
  return enemy.x + math.cos(radians) * flight * 8 * 0.7, enemy.y + math.sin(radians) * flight * 8 * 0.7
end

local function item_score(obs, item)
  local self, base = obs.self, ITEM_SCORE[item.type]
  if not base then return 0 end
  if item.type == "heal" or item.type == "repair-core" then
    if obs.overtime or self.hp >= self.maxHp then return 0 end
    if self.hp < self.maxHp * 0.5 then base = base * 2 end
  end
  if item.type == "shield" and self.shield >= 50 then return 0 end
  if self.weapon ~= "" and item.type == "weapon_" .. self.weapon then return 0 end
  return base
end

local function best_item(obs, max_distance)
  local best, best_value
  for _, item in ipairs(obs.items or {}) do
    if item.active ~= false then
      local score = item_score(obs, item)
      local distance = arena.distance(obs.self, item)
      if score > 0 and distance <= (max_distance or math.huge) then
        local value = score / (distance + 20)
        if not best_value or value > best_value then best, best_value = item, value end
      end
    end
  end
  return best
end

-- Corner loop sized from the real arena so other maps work without edits.
local waypoints
local function patrol_route(obs)
  if not waypoints then
    local width, height = obs.arenaWidth, obs.arenaHeight
    local margin_x, margin_y = width * 0.15, height * 0.2
    waypoints = {
      { x = margin_x, y = margin_y },
      { x = width - margin_x, y = margin_y },
      { x = width - margin_x, y = height - margin_y },
      { x = margin_x, y = height - margin_y },
    }
  end
  return waypoints
end

-- Waypoints outside the collapsing zone are skipped until one is inside.
local function next_waypoint(obs)
  local route = patrol_route(obs)
  local zone = obs.zone
  for _ = 1, #route do
    state.index = ((state.index - 1 + direction) % #route) + 1
    local waypoint = route[state.index]
    if not zone or not zone.active or arena.distance(waypoint, zone) < zone.radius - 30 then
      return waypoint
    end
  end
  zone = zone or { x = obs.arenaWidth / 2, y = obs.arenaHeight / 2 }
  return { x = zone.x, y = zone.y }
end

local function zone_safe(obs)
  local zone = obs.zone
  if not zone or not zone.active then return true end
  return arena.distance(obs.self, zone) < zone.radius - 40
end

local function decide(observation)
  local self = observation.self

  if not zone_safe(observation) then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, observation.zone.x, observation.zone.y, 7, "returning to zone")
  end

  -- Engage the nearest visible enemy; ignore distant ghosts.
  local enemy, enemy_distance, visible
  for _, robot in ipairs(observation.robots or {}) do
    if robot.alive and robot.team ~= self.team then
      local distance = arena.distance(self, robot)
      if not enemy_distance or distance < enemy_distance then
        enemy, enemy_distance = robot, distance
        visible = arena.line_of_sight(self.x, self.y, robot.x, robot.y, observation.obstacles)
      end
    end
  end
  if enemy and visible and enemy_distance < 220 then
    if enemy_distance > 45 then
      local held = unstick(observation, { moving = true })
      if held then return held end
      return arena.approach(observation, enemy, 7)
    end
    local x, y = lead_target(observation, enemy)
    local held = unstick(observation, { moving = true })
    if held then return held end
    return arena.action({ move = 5, turn = 10, fire = true, target_x = x, target_y = y, logs = { "engaging " .. enemy.name } })
  end

  -- Items close to the route are worth a small detour.
  local item = best_item(observation, 150)
  if item then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, item.x, item.y, 6, "detour for " .. item.type)
  end

  local waypoint = next_waypoint(observation)
  if arena.distance(self, waypoint) < 40 then
    waypoint = next_waypoint(observation)
  end
  local held = unstick(observation, { moving = true })
  if held then return held end
  return navigate(observation, waypoint.x, waypoint.y, 5, "patrolling")
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
