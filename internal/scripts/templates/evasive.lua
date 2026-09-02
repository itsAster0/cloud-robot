local arena = require "arena"

-- Kiter: paranoid and proud of it. Anything aimed at us is dodged the tick
-- it appears, healthy or not; projectiles already inside 60 units trigger a
-- panic dash; retreats juke through a random safe point instead of running
-- a straight line; and every close call earns either a nervous squawk or an
-- unconvincing boast on the team channel. It holds enemies in a 90-190 unit
-- band, flips strafe on random intervals, grabs heals and scopes early, and
-- starts disengaging at two-thirds health — long before things get dramatic.
-- Randomness is seeded from the robot ID so two boxes dodge differently.

local seed = 0
local robot_id = os.getenv("ROBOT_ID") or "kiter"
for index = 1, #robot_id do
  seed = (seed * 131 + robot_id:byte(index)) % 2147483647
end
math.randomseed(seed)

local PROJECTILE_SPEED = { plasma = 24, cannon = 14, machine_gun = 32, incendiary = 20, cryo = 20, emp = 18, railgun = 0 }
-- Scope ranks high: a runner that spots threats early lives longer.
local UPGRADE_SCORE = { scope = 80, shield = 70, medkit = 75, overdrive = 65, nano_repair = 60, rapid_fire = 65, weapon_railgun = 100, weapon_cannon = 80, weapon_incendiary = 55, weapon_cryo = 50, weapon_emp = 45, weapon_machine_gun = 35, weapon_plasma = 30 }
local NERVOUS = { "nope nope nope", "that was CLOSE", "do not want", "who authorized that?!" }
local COCKY = { "too slow, metal breath", "can't even corner me", "miss me?", "is that all you have?" }

local state = {
  strafe = 1, flip_at = 0, last_x = nil, last_y = nil, stuck = 0, escape = 0, escape_turn = 0,
  wander = nil, hp = nil, nervous_at = 0, cocky_at = 0, chased = false,
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

local function best_upgrade(obs, max_distance)
  local best, best_value
  for _, item in ipairs(obs.items or {}) do
    if item.active ~= false then
      local score = UPGRADE_SCORE[item.type] or 0
      if (item.type == "shield" and obs.self.shield >= 50) or (item.type == "weapon_" .. (obs.self.weapon or "plasma")) then
        score = 0
      end
      -- Heals are worthless at full HP; the server skips them in overtime.
      if (item.type == "medkit" and (obs.overtime or obs.self.hp >= obs.self.maxHp)) or (item.type == "nano_repair" and obs.self.hp >= obs.self.maxHp) then
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

-- Finds the closest point behind an obstacle that breaks line of sight to the
-- shooter: the obstacle center pushed slightly past, away from the shooter,
-- kept only when the segment from there back to the shooter is really blocked.
local function cover_point(obs, shooter)
  local self, best, best_distance = obs.self, nil, nil
  for _, obstacle in ipairs(obs.obstacles or {}) do
    local cx, cy
    if obstacle.radius then
      cx, cy = obstacle.x, obstacle.y
    else
      cx, cy = obstacle.x + (obstacle.width or 0) / 2, obstacle.y + (obstacle.height or 0) / 2
    end
    local x, y = cx + (cx - shooter.x) * 0.35, cy + (cy - shooter.y) * 0.35
    if obs.arenaWidth then x = math.max(20, math.min(obs.arenaWidth - 20, x)) end
    if obs.arenaHeight then y = math.max(20, math.min(obs.arenaHeight - 20, y)) end
    if not arena.line_of_sight(x, y, shooter.x, shooter.y, obs.obstacles) then
      local distance = arena.distance(self, { x = x, y = y })
      if not best_distance or distance < best_distance then best, best_distance = { x = x, y = y }, distance end
    end
  end
  return best
end

-- Aims where the enemy will be; damped because enemies rarely hold a line.
local function lead_target(obs, enemy)
  local speed = PROJECTILE_SPEED[obs.self.weapon or "plasma"] or 14
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

-- Health dropping between ticks means something just bit us.
local function took_damage(obs)
  local self = obs.self
  local damaged = state.hp and self.hp < state.hp - 1
  state.hp = self.hp
  return damaged
end

local function decide(observation)
  local self = observation.self
  local damaged = took_damage(observation)

  if not zone_safe(observation) then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, observation.zone.x, observation.zone.y, 8, "returning to zone")
  end

  -- Nearest enemy plus how exposed we are to it.
  local enemy, enemy_distance
  for _, robot in ipairs(observation.robots or {}) do
    if robot.alive and robot.team ~= self.team then
      local distance = arena.distance(self, robot)
      if not enemy_distance or distance < enemy_distance then enemy, enemy_distance = robot, distance end
    end
  end

  -- Paranoid: anything aimed at us is dodged immediately, healthy or not.
  -- Hurt and under fire, a real hole in the line of sight beats a dodge
  -- point; a healthy kiter just slips the shot and keeps the band.
  if arena.danger_level(observation) > 0 then
    local target, log
    if self.hp < self.maxHp * 0.65 and enemy then
      target, log = cover_point(observation, enemy) or arena.dodge(observation), "breaking line of sight"
    else
      target, log = arena.dodge(observation), "dodging projectile"
    end
    if target then
      -- A projectile already inside 60 units and a spare charge: panic dash.
      if #arena.projectiles_near(observation, 60) > 0 and (self.dashCharges or 0) > 0 then
        local turn = ((arena.bearing(self, target) - self.heading + 540) % 360) - 180
        return arena.dash({ move = 8, turn = turn, logs = { "panic dash" } })
      end
      local held = unstick(observation, { moving = true })
      if held then return held end
      return navigate(observation, target.x, target.y, 8, log)
    end
  end

  -- Just took a hit: juke sideways through a random safe point while saying
  -- something unconvincing about it.
  if damaged and enemy then
    local away = { x = self.x + (self.x - enemy.x) * 0.6, y = self.y + (self.y - enemy.y) * 0.6 }
    local juke = arena.random_safe_point(observation, away.x, away.y, 110, 8)
    local held = unstick(observation, { moving = true })
    if held then return held end
    local turn = ((arena.bearing(self, juke) - self.heading + 540) % 360) - 180
    local spec = { move = 8, turn = turn, logs = { "juking aside" } }
    if observation.tick >= state.nervous_at then
      state.nervous_at = observation.tick + 120
      local line = NERVOUS[math.random(#NERVOUS)]
      spec.logs = { line }
      return arena.send_message(line, spec)
    end
    return arena.action(spec)
  end

  -- Two-thirds health is dramatic enough: heals first, then a juking
  -- retreat toward the far side instead of a straight, predictable line.
  if self.hp < self.maxHp * 0.65 then
    local heal = arena.nearest_item(observation, "heal") or arena.nearest_item(observation, "medkit")
      or arena.nearest_item(observation, "repair-core") or arena.nearest_item(observation, "nano_repair")
    if heal and not observation.overtime then
      local held = unstick(observation, { moving = true })
      if held then return held end
      return navigate(observation, heal.x, heal.y, 8, "grabbing " .. heal.type)
    end
    if enemy then
      local away = { x = self.x + (self.x - enemy.x), y = self.y + (self.y - enemy.y) }
      local juke = arena.random_safe_point(observation, away.x, away.y, 120, 8)
      local held = unstick(observation, { moving = true })
      if held then return held end
      return navigate(observation, juke.x, juke.y, 8, "disengaging")
    end
  end

  -- Safe upgrades within a short detour: shield first, then weapons.
  if enemy_distance == nil or enemy_distance > 90 then
    local upgrade = best_upgrade(observation, 140)
    if upgrade then
      local held = unstick(observation, { moving = true })
      if held then return held end
      return navigate(observation, upgrade.x, upgrade.y, 7, "grabbing " .. upgrade.type)
    end
  end

  if enemy then
    local visible = arena.line_of_sight(self.x, self.y, enemy.x, enemy.y, observation.obstacles)
    if observation.tick >= state.flip_at then
      state.strafe = math.random() < 0.5 and 1 or -1
      state.flip_at = observation.tick + 15 + math.random(25)
    end
    local x, y = lead_target(observation, enemy)
    -- Inside 90 they out-brawl us: back away while firing. Past 190 close in.
    local move, log = 0, "kiting"
    if enemy_distance < 90 then
      move, log = -6, "opening distance"
    elseif enemy_distance > 190 or not visible then
      move, log = 6, "closing in"
    else
      move, log = 4, "strafing"
    end
    -- Track whether we were really chased, so the boasts are at least earned.
    if enemy_distance < 120 then
      state.chased = true
    elseif enemy_distance > 300 then
      state.chased = false
    end
    local hold = unstick(observation, { moving = move >= 0 })
    if hold then return hold end
    -- Cocky when the pursuer gives up: throttled, and only after a chase.
    if state.chased and enemy_distance > 240 and visible and observation.tick >= state.cocky_at then
      state.chased = false
      state.cocky_at = observation.tick + 200
      local line = COCKY[math.random(#COCKY)]
      return arena.send_message(line, { move = move, turn = 12 * state.strafe, fire = visible, target_x = x, target_y = y, logs = { line } })
    end
    return arena.action({ move = move, turn = 12 * state.strafe, fire = visible, target_x = x, target_y = y, logs = { log } })
  end

  -- No pressure: sweep upgrades anywhere on the map, otherwise wander.
  local upgrade = best_upgrade(observation, math.huge)
  if upgrade then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, upgrade.x, upgrade.y, 6, "looting " .. upgrade.type)
  end
  if not state.wander or arena.distance(self, state.wander) < 40 then
    state.wander = { x = 60 + math.random() * (observation.arenaWidth - 120), y = 60 + math.random() * (observation.arenaHeight - 120) }
  end
  local held = unstick(observation, { moving = true })
  if held then return held end
  return navigate(observation, state.wander.x, state.wander.y, 5, "roaming")
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
