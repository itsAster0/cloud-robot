local arena = require "arena"

-- Sentry: a disciplined route walker with radio procedure. It walks a
-- deliberate corner loop sized from the arena, sweeps a scan at every
-- checkpoint, investigates disturbances near the route, and reports each
-- step over the team channel in its flattest military voice. Contact inside
-- the vision range with clear line of sight is engaged; everything else is
-- paperwork. Randomness is seeded from the robot ID.

local seed = 0
local robot_id = os.getenv("ROBOT_ID") or "sentry"
for index = 1, #robot_id do
  seed = (seed * 131 + robot_id:byte(index)) % 2147483647
end
math.randomseed(seed)

local PROJECTILE_SPEED = { plasma = 24, cannon = 14, machine_gun = 32, incendiary = 20, cryo = 20, emp = 18, railgun = 0 }
local ITEM_SCORE = {
  heal = 90, medkit = 85, ["repair-core"] = 60, nano_repair = 65, scope = 60,
  shield = 70, overdrive = 65, rapid_fire = 65,
  weapon_railgun = 100, weapon_cannon = 80, weapon_incendiary = 55,
  weapon_cryo = 50, weapon_emp = 45, weapon_machine_gun = 35, weapon_plasma = 30,
}

local state = {
  index = 1, swept = false, swept_at = nil, engaged = nil,
  last_x = nil, last_y = nil, stuck = 0, escape = 0, escape_turn = 0,
  radio_at = 0, search = nil, search_until = 0, search_reported = false, hp = nil,
}
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
  local speed = PROJECTILE_SPEED[obs.self.weapon or "plasma"] or 14
  if speed <= 0 then return enemy.x, enemy.y end
  local flight = math.min(arena.distance(obs.self, enemy) / speed, 8)
  local radians = enemy.heading * math.pi / 180
  return enemy.x + math.cos(radians) * flight * 8 * 0.7, enemy.y + math.sin(radians) * flight * 8 * 0.7
end

local function item_score(obs, item)
  local self, base = obs.self, ITEM_SCORE[item.type]
  if not base then return 0 end
  if item.type == "heal" or item.type == "repair-core" or item.type == "medkit" then
    if obs.overtime or self.hp >= self.maxHp then return 0 end
    if self.hp < self.maxHp * 0.5 then base = base * 2 end
  end
  if item.type == "nano_repair" and self.hp >= self.maxHp then return 0 end
  if item.type == "shield" and self.shield >= 50 then return 0 end
  if item.type == "weapon_" .. (self.weapon or "plasma") then return 0 end
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

-- The current post: advance only past waypoints the collapsing zone has
-- eaten, so the loop is actually walked instead of averaged.
local function current_waypoint(obs)
  local route = patrol_route(obs)
  local zone = obs.zone
  local waypoint = route[state.index]
  if not zone or not zone.active then return waypoint end
  for _ = 1, #route do
    if arena.distance(waypoint, zone) < zone.radius - 30 then return waypoint end
    state.index = ((state.index - 1 + direction) % #route) + 1
    waypoint = route[state.index]
  end
  return { x = zone.x, y = zone.y }
end

-- Radio discipline: every report is logged for telemetry, and relayed over
-- the team channel when someone is listening and the channel is not clogged.
local function transmit(obs, spec, text)
  spec.logs = { "radio: " .. text }
  if obs.tick >= state.radio_at then
    state.radio_at = obs.tick + 25
    if #arena.teammates(obs) > 0 then return arena.send_message(text, spec) end
  end
  return arena.action(spec)
end

-- Health dropping between ticks means we are taking fire from somewhere.
local function took_damage(obs)
  local self = obs.self
  local damaged = state.hp and self.hp < state.hp - 1
  state.hp = self.hp
  return damaged
end

local function zone_safe(obs)
  local zone = obs.zone
  if not zone or not zone.active then return true end
  return arena.distance(obs.self, zone) < zone.radius - 40
end

local function decide(observation)
  local self = observation.self
  local damaged = took_damage(observation)

  if not zone_safe(observation) then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, observation.zone.x, observation.zone.y, 7, "returning to zone")
  end

  -- Engage the nearest enemy we can actually see: inside the vision range
  -- with clear line of sight. Distant or covered robots are ignored.
  local enemy, enemy_distance, visible
  for _, robot in ipairs(observation.robots or {}) do
    if robot.alive and robot.team ~= self.team then
      local distance = arena.distance(self, robot)
      if not enemy_distance or distance < enemy_distance then
        enemy, enemy_distance = robot, distance
        visible = arena.can_see(observation, robot)
      end
    end
  end
  if enemy and visible and enemy_distance < 220 then
    -- First tick of an engagement goes out over the radio.
    if state.engaged ~= enemy.robotId then
      state.engaged = enemy.robotId
      local turn = ((arena.bearing(self, enemy) - self.heading + 540) % 360) - 180
      local held = unstick(observation, { moving = true })
      if held then return held end
      local spec = { move = 7, turn = turn, fire = true, target_x = enemy.x, target_y = enemy.y }
      return transmit(observation, spec, "contact " .. enemy.name .. ". engaging.")
    end
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
  state.engaged = nil

  -- Disturbance handling: incoming fire or a sweep contact opens a search of
  -- the sector. After the search, the route resumes on its own.
  if state.search then
    if observation.tick > state.search_until or arena.distance(self, state.search) < 30 then
      state.search = nil
      state.search_reported = false
      local waypoint = current_waypoint(observation)
      local turn = ((arena.bearing(self, waypoint) - self.heading + 540) % 360) - 180
      local held = unstick(observation, { moving = true })
      if held then return held end
      return transmit(observation, { move = 6, turn = turn }, "sector searched. resuming patrol.")
    end
    local held = unstick(observation, { moving = true })
    if held then return held end
    local turn = ((arena.bearing(self, state.search) - self.heading + 540) % 360) - 180
    local text = state.search_reported and "searching the sector." or "taking fire. searching the sector."
    state.search_reported = true
    return transmit(observation, { move = 6, turn = turn }, text)
  end
  if damaged then
    state.search = arena.random_safe_point(observation, self.x, self.y, 110, 8)
    state.search_until = observation.tick + 140
  end

  -- Items close to the route are worth a small detour.
  local item = best_item(observation, 150)
  if item then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, item.x, item.y, 6, "detour for " .. item.type)
  end

  -- Checkpoint procedure: on arrival, one sweep before the loop advances.
  local waypoint = current_waypoint(observation)
  if arena.distance(self, waypoint) < 40 then
    if not state.swept then
      state.swept = true
      state.swept_at = observation.tick
      local held = unstick(observation, { moving = true })
      if held then return held end
      return arena.scan(waypoint.x, waypoint.y, 240, { move = 3,
        logs = { "radio: checkpoint " .. state.index .. " of " .. #patrol_route(observation) .. ". sweeping sector." } })
    end
    state.index = ((state.index - 1 + direction) % #patrol_route(observation)) + 1
    state.swept = false
    waypoint = current_waypoint(observation)
    local turn = ((arena.bearing(self, waypoint) - self.heading + 540) % 360) - 180
    local held = unstick(observation, { moving = true })
    if held then return held end
    return transmit(observation, { move = 5, turn = turn }, "checkpoint secure. advancing to checkpoint " .. state.index .. ".")
  end

  -- A fresh sweep that painted contacts off-route becomes an investigation.
  local report = observation.scanResult
  if report and state.swept_at and observation.tick > state.swept_at and observation.tick - state.swept_at <= 60 then
    for _, contact in ipairs(report.enemies or {}) do
      state.search = { x = contact.x, y = contact.y }
      state.search_until = observation.tick + 120
      break
    end
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
