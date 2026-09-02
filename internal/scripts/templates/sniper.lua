local arena = require "arena"

-- Marksman: holds a 40-130 unit band, only fires when the target is inside
-- its vision range with clear line of sight, leads shots from the enemy
-- heading, sidesteps randomly so it never sits still, grabs scopes and heals
-- when hurt, and races for railguns when the field is quiet.

local seed = 0
local robot_id = os.getenv("ROBOT_ID") or "marksman"
for index = 1, #robot_id do
  seed = (seed * 131 + robot_id:byte(index)) % 2147483647
end
math.randomseed(seed)

local PROJECTILE_SPEED = { plasma = 24, cannon = 14, machine_gun = 32, incendiary = 20, cryo = 20, emp = 18, railgun = 0 }
-- Upgrades worth breaking stance for, plus heals that only matter when hurt.
local ITEM_VALUE = {
  scope = 60, medkit = 75, heal = 70, nano_repair = 55, ["repair-core"] = 50,
  weapon_railgun = 100, weapon_cannon = 80, weapon_incendiary = 55,
  weapon_cryo = 50, weapon_emp = 45, weapon_machine_gun = 35, weapon_plasma = 30,
}

local state = { last_x = nil, last_y = nil, stuck = 0, escape = 0, escape_turn = 0, sidestep = 0, sidestep_turn = 0, wander = nil }

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

local function zone_safe(obs)
  local zone = obs.zone
  if not zone or not zone.active then return true end
  return arena.distance(obs.self, zone) < zone.radius - 40
end

local function best_pickup(obs, max_distance)
  local best, best_value
  for _, item in ipairs(obs.items or {}) do
    if item.active ~= false then
      local score, self = ITEM_VALUE[item.type] or 0, obs.self
      if item.type == "heal" or item.type == "medkit" or item.type == "repair-core" then
        -- The server skips these at full HP and in overtime anyway.
        if obs.overtime or self.hp >= self.maxHp then score = 0 end
      elseif item.type == "nano_repair" and self.hp >= self.maxHp then
        score = 0
      elseif self.weapon ~= "" and item.type == "weapon_" .. self.weapon then
        score = 0
      end
      local distance = arena.distance(obs.self, item)
      if score > 0 and distance <= (max_distance or math.huge) then
        local value = score / (distance + 20)
        if not best_value or value > best_value then best, best_value = item, value end
      end
    end
  end
  return best
end

local function decide(observation)
  local self = observation.self

  if not zone_safe(observation) then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, observation.zone.x, observation.zone.y, 7, "returning to zone")
  end

  local enemy, enemy_distance
  for _, robot in ipairs(observation.robots or {}) do
    if robot.alive and robot.team ~= self.team then
      local distance = arena.distance(self, robot)
      if not enemy_distance or distance < enemy_distance then enemy, enemy_distance = robot, distance end
    end
  end

  -- Hurt: grab the nearest heal before rejoining the duel.
  if self.hp < self.maxHp * 0.45 then
    local heal = arena.nearest_item(observation, "heal") or arena.nearest_item(observation, "medkit")
      or arena.nearest_item(observation, "repair-core") or arena.nearest_item(observation, "nano_repair")
    if heal and not observation.overtime then
      local held = unstick(observation, { moving = true })
      if held then return held end
      return navigate(observation, heal.x, heal.y, 6, "grabbing " .. heal.type)
    end
  end

  -- Grab an upgrade whenever the field is quiet enough.
  if enemy_distance == nil or enemy_distance > 120 then
    local pickup = best_pickup(observation, enemy_distance == nil and math.huge or 160)
    if pickup then
      local held = unstick(observation, { moving = true })
      if held then return held end
      return navigate(observation, pickup.x, pickup.y, 6, "claiming " .. pickup.type)
    end
  end

  if enemy then
    -- Full sight, not just line of sight: outside the vision range a robot
    -- is not in the list at all, so this mainly refuses cover-blind shots.
    local visible = arena.can_see(observation, enemy)
    if not visible then
      -- Cover between us: push toward the enemy until the angle opens.
      local held = unstick(observation, { moving = true })
      if held then return held end
      return navigate(observation, enemy.x, enemy.y, 5, "flanking for sight")
    end
    -- Refuse to chase an enemy that fled outside the collapsing zone.
    local zone = observation.zone
    if zone and zone.active and arena.distance(enemy, zone) > zone.radius + 30 then
      local hold = unstick(observation, { moving = false })
      if hold then return hold end
      return arena.action({ logs = { "holding zone edge" } })
    end
    local x, y = lead_target(observation, enemy)
    local move, log = 0, "firing"
    if enemy_distance > 130 then
      move, log = 5, "closing to range"
    elseif enemy_distance < 40 then
      move, log = -5, "creating distance"
    end
    -- While holding position, sidestep on random intervals: a still sniper
    -- is a free target for lead shots.
    if move == 0 then
      if observation.tick >= state.sidestep then
        state.sidestep_turn = (math.random() < 0.5 and -1 or 1) * (10 + math.random(10))
        state.sidestep = observation.tick + 12 + math.random(20)
      end
      local hold = unstick(observation, { moving = false })
      if hold then return hold end
      return arena.action({ turn = state.sidestep_turn, fire = true, target_x = x, target_y = y, logs = { "holding " .. math.floor(enemy_distance) } })
    end
    local hold = unstick(observation, { moving = true })
    if hold then return hold end
    return arena.action({ move = move, fire = true, target_x = x, target_y = y, logs = { log } })
  end

  -- Quiet field: hold near map center for short travel to any fight.
  if not state.wander or arena.distance(self, state.wander) < 40 then
    state.wander = { x = observation.arenaWidth * (0.3 + math.random() * 0.4), y = observation.arenaHeight * (0.3 + math.random() * 0.4) }
  end
  local held = unstick(observation, { moving = true })
  if held then return held end
  return navigate(observation, state.wander.x, state.wander.y, 5, "repositioning")
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
