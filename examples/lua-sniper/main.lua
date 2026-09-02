local arena = require "arena"

-- Marksman: patient, cold, and always at the far end of the range chart.
-- It holds a 110-320 unit band, tracks each target's real drift between
-- ticks, and only spends a shot when the SDK lead solution is trustworthy:
-- a target that holds its line, a stationary target, a close target, or a
-- railgun. While it waits it aims, sidesteps on random intervals, and
-- repositions when the perch goes stale. Enemies that close the distance
-- are backed away from, never brawled. Randomness is seeded from the
-- robot ID.

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

local state = {
  last_x = nil, last_y = nil, stuck = 0, escape = 0, escape_turn = 0,
  sidestep = 0, sidestep_turn = 0, wander = nil, tracks = {}, kills = 0, line = nil,
}

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
      elseif item.type == "weapon_" .. (self.weapon or "plasma") then
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

-- The kill counter on self is the kill feed. The only celebration on record
-- is a shorter sentence than usual.
local function check_kills(obs)
  local kills = obs.self.kills or 0
  if kills > state.kills then
    state.line = kills == 1 and "target down." or "target down. next."
    state.kills = kills
  end
end

-- Trigger discipline. Observations carry no robot velocity, so the marksman
-- keeps each target's last position and derives per-tick drift, then trusts
-- the SDK lead helper only when that drift held steady since the previous
-- tick, the target is nearly stopped, or the point blank shot cannot miss.
local function firing_solution(obs, enemy, distance)
  local speed = PROJECTILE_SPEED[obs.self.weapon or "plasma"] or 14
  if speed <= 0 then return true, enemy.x, enemy.y end
  local record = state.tracks[enemy.robotId]
  state.tracks[enemy.robotId] = { x = enemy.x, y = enemy.y, tick = obs.tick }
  if not record or not record.tick or obs.tick - record.tick > 2 then
    -- Fresh contact or a stale track: no drift model yet, shoot the snapshot.
    return true, enemy.x, enemy.y
  end
  local span = math.max(obs.tick - record.tick, 1)
  local vx, vy = (enemy.x - record.x) / span, (enemy.y - record.y) / span
  if vx * vx + vy * vy > 2500 then
    -- Teleport-sized jump: the model is garbage, wait for a fresh one.
    return false, enemy.x, enemy.y
  end
  local pace = math.sqrt(vx * vx + vy * vy)
  local stability = math.abs(vx - (record.vx or 0)) + math.abs(vy - (record.vy or 0))
  local lead = arena.aim_predict(obs.self, enemy, speed, vx * 0.85, vy * 0.85)
  return pace < 1.5 or stability < 2.5 or distance < 90, lead.x, lead.y
end

local function decide(observation)
  local self = observation.self
  check_kills(observation)

  if not zone_safe(observation) then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, observation.zone.x, observation.zone.y, 7, "returning to zone")
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

  local enemy, enemy_distance
  for _, robot in ipairs(observation.robots or {}) do
    if robot.alive and robot.team ~= self.team then
      local distance = arena.distance(self, robot)
      if not enemy_distance or distance < enemy_distance then enemy, enemy_distance = robot, distance end
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
    -- An enemy inside the retreat band is the one thing that breaks patience.
    if enemy_distance < 110 then
      local confident, x, y = firing_solution(observation, enemy, enemy_distance)
      local line = state.line
      state.line = nil
      if enemy_distance < 70 and (self.dashCharges or 0) > 0 then
        local away = { x = self.x + (self.x - enemy.x), y = self.y + (self.y - enemy.y) }
        local turn = ((arena.bearing(self, away) - self.heading + 540) % 360) - 180
        local spec = { move = 8, turn = turn, fire = confident, target_x = x, target_y = y, logs = { line or "disengaging" } }
        if line then return arena.send_message(line, spec) end
        return arena.dash(spec)
      end
      local hold = unstick(observation, { moving = true })
      if hold then return hold end
      local spec = { move = -6, fire = confident, target_x = x, target_y = y, logs = { line or "backing off" } }
      if line then return arena.send_message(line, spec) end
      return arena.action(spec)
    end
    local confident, x, y = firing_solution(observation, enemy, enemy_distance)
    -- Patience: aim at the projected point and wait for the target to hold
    -- a line. A still barrel with a live solution beats a spray.
    if not confident then
      if observation.tick >= state.sidestep then
        state.sidestep_turn = (math.random() < 0.5 and -1 or 1) * (10 + math.random(10))
        state.sidestep = observation.tick + 12 + math.random(20)
      end
      local hold = unstick(observation, { moving = false })
      if hold then return hold end
      local turn = ((arena.bearing(self, { x = x, y = y }) - self.heading + 540) % 360) - 180
      return arena.action({ move = 0, turn = turn, fire = false, target_x = x, target_y = y, logs = { "waiting for a clean shot" } })
    end
    -- In band: hold and shoot; sidestep so the perch never goes stale.
    if enemy_distance <= 320 then
      if observation.tick >= state.sidestep then
        state.sidestep_turn = (math.random() < 0.5 and -1 or 1) * (10 + math.random(10))
        state.sidestep = observation.tick + 12 + math.random(20)
      end
      local hold = unstick(observation, { moving = false })
      if hold then return hold end
      local line = state.line
      state.line = nil
      local spec = { turn = state.sidestep_turn, fire = true, target_x = x, target_y = y, logs = { line or ("range " .. math.floor(enemy_distance) .. ". holding.") } }
      if line then return arena.send_message(line, spec) end
      return arena.action(spec)
    end
    local hold = unstick(observation, { moving = true })
    if hold then return hold end
    local line = state.line
    state.line = nil
    local spec = { move = 5, fire = true, target_x = x, target_y = y, logs = { line or "closing to range" } }
    if line then return arena.send_message(line, spec) end
    return arena.action(spec)
  end

  -- Quiet field: relocate the perch now and then; a sniper that never moves
  -- is a coordinate, not a threat.
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
