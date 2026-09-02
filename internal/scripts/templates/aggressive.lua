local arena = require "arena"

-- Brawler: a relentless hunter. It remembers where prey was last seen and
-- keeps pushing that point after contact breaks, feeds heading-based drift
-- into the SDK aim predictor for lead shots, and spends dash charges to run
-- down wounded enemies. Kills are announced on the team channel; the first
-- kill of the match gets its own line. Firing is gated on full sight: inside
-- the vision range with clear line of sight. Randomness is seeded from the
-- robot ID so two boxes hunt differently without wall-clock time.

local seed = 0
local robot_id = os.getenv("ROBOT_ID") or "brawler"
for index = 1, #robot_id do
  seed = (seed * 131 + robot_id:byte(index)) % 2147483647
end
math.randomseed(seed)

-- Projectile speeds mirror the server weapon table; railgun is hitscan.
local PROJECTILE_SPEED = { plasma = 24, cannon = 14, machine_gun = 32, incendiary = 20, cryo = 20, emp = 18, railgun = 0 }
local ITEM_SCORE = {
  heal = 90, medkit = 85, ["repair-core"] = 60, nano_repair = 65, scope = 60,
  shield = 70, overdrive = 65, rapid_fire = 65,
  weapon_railgun = 100, weapon_cannon = 80, weapon_incendiary = 55,
  weapon_cryo = 50, weapon_emp = 45, weapon_machine_gun = 35, weapon_plasma = 30,
}
local TAUNTS = { "too slow.", "stay down.", "that all you have?", "next.", "keep running." }
local FIRST_BLOOD = "first blood. it will not be the last."

local state = {
  strafe = 1, flip_at = 0, last_x = nil, last_y = nil, stuck = 0, escape = 0, escape_turn = 0,
  wander = nil, kills = 0, taunt = nil, known = nil, known_at = 0,
}

local function has_effect(self, name)
  for _, effect in ipairs(self.effects or {}) do
    if effect.type == name and effect.ticks > 0 then return true end
  end
  return false
end

-- Detects a wedged robot: tried to move for several ticks but barely moved.
-- Backs off with a fresh random turn instead of grinding into the obstacle.
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

-- Steers toward a point, taking the SDK detour waypoint when geometry blocks
-- the straight line. Always returns a move action, never fire.
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

-- Worth of an item right now: heals only matter when hurt or absent in
-- overtime, shields when the existing one decayed, weapons when not carried.
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

-- Value density beats raw distance: a railgun three times farther than a
-- machine gun is still worth the trip.
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

-- Prefers enemies we can actually see: inside the vision range with clear
-- line of sight. Enemies behind cover are still worth chasing down.
local function pick_enemy(obs)
  local seen, unseen, seen_distance, unseen_distance
  for _, robot in ipairs(obs.robots or {}) do
    if robot.alive and robot.team ~= obs.self.team then
      local distance = arena.distance(obs.self, robot)
      if arena.can_see(obs, robot) then
        if not seen_distance or distance < seen_distance then seen, seen_distance = robot, distance end
      elseif not unseen_distance or distance < unseen_distance then
        unseen, unseen_distance = robot, distance
      end
    end
  end
  return seen or unseen, seen_distance or unseen_distance, seen ~= nil
end

-- Aims where the enemy will be: heading-based drift damped 30% and fed
-- through the SDK lead helper, because prey rarely holds a straight line.
local function lead_point(obs, enemy)
  local speed = PROJECTILE_SPEED[obs.self.weapon or "plasma"] or 14
  if speed <= 0 then return enemy.x, enemy.y end
  local radians = enemy.heading * math.pi / 180
  local lead = arena.aim_predict(obs.self, enemy, speed, math.cos(radians) * 5.6, math.sin(radians) * 5.6)
  return lead.x, lead.y
end

-- The kill counter on self is the kill feed: a rising count means something
-- died to us this tick. Queue the next taunt; the first kill of the match
-- gets its own line.
local function check_kills(obs)
  local kills = obs.self.kills or 0
  if kills <= state.kills then return end
  state.taunt = state.kills == 0 and FIRST_BLOOD or TAUNTS[(kills % #TAUNTS) + 1]
  state.kills = kills
end

local function zone_safe(obs)
  local zone = obs.zone
  if not zone or not zone.active then return true end
  return arena.distance(obs.self, zone) < zone.radius - 40
end

local function decide(observation)
  local self = observation.self
  check_kills(observation)

  -- Zone collapse outranks everything; standing outside is free damage.
  if not zone_safe(observation) then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, observation.zone.x, observation.zone.y, 7, "returning to zone")
  end

  local enemy, distance, visible = pick_enemy(observation)
  if enemy and visible then
    state.known = { x = enemy.x, y = enemy.y }
    state.known_at = observation.tick
  end

  -- Detour for heals when genuinely hurt and no enemy is breathing on us.
  if self.hp < self.maxHp * 0.45 and (not enemy or not visible or distance > 60) then
    local heal = arena.nearest_item(observation, "heal") or arena.nearest_item(observation, "medkit")
      or arena.nearest_item(observation, "repair-core") or arena.nearest_item(observation, "nano_repair")
    if heal then
      local held = unstick(observation, { moving = true })
      if held then return held end
      return navigate(observation, heal.x, heal.y, 8, "grabbing " .. heal.type)
    end
  end

  if enemy then
    local hold
    -- Wounded prey inside dash range: burn a charge and finish it.
    if visible and distance > 45 and distance < 240 and (self.dashCharges or 0) > 0
      and (enemy.hp or 0) < (enemy.maxHp or 1) * 0.6 then
      local x, y = lead_point(observation, enemy)
      local turn = ((arena.bearing(self, enemy) - self.heading + 540) % 360) - 180
      local taunt = state.taunt
      state.taunt = nil
      local spec = { move = 8, turn = turn, fire = true, target_x = x, target_y = y, logs = { taunt or ("running down " .. enemy.name) } }
      if taunt then return arena.send_message(taunt, spec) end
      return arena.dash(spec)
    end
    if distance > 30 and visible then
      hold = unstick(observation, { moving = true })
      if hold then return hold end
      -- Approach charges and fires straight; lead shots take over in close range.
      return arena.approach(observation, enemy, 8)
    end
    -- Close range: circle the enemy, flip strafe direction at random intervals
    -- so opponents cannot predict the orbit.
    if observation.tick >= state.flip_at then
      state.strafe = math.random() < 0.5 and 1 or -1
      state.flip_at = observation.tick + 20 + math.random(30)
    end
    -- Point blank: the SDK strafe orbits and fires down the current heading,
    -- so the shot only goes out when the barrel is roughly on target.
    if distance < 55 then
      local error_turn = ((arena.bearing(self, enemy) - self.heading + 540) % 360) - 180
      if math.abs(error_turn) < 25 then
        return arena.strafe(observation, enemy, state.strafe)
      end
      hold = unstick(observation, { moving = true })
      if hold then return hold end
      return arena.action({ move = 6, turn = error_turn, logs = { "circling " .. enemy.name } })
    end
    local x, y = lead_point(observation, enemy)
    hold = unstick(observation, { moving = true })
    if hold then return hold end
    local taunt = state.taunt
    state.taunt = nil
    local spec = { move = 6, turn = 14 * state.strafe, fire = visible, target_x = x, target_y = y, logs = { taunt or ("brawling " .. enemy.name) } }
    if taunt then return arena.send_message(taunt, spec) end
    return arena.action(spec)
  end

  -- Prey broke sight: push the position it was really seen at before giving
  -- up and going back to the sweep.
  if state.known and observation.tick - state.known_at < 240 and arena.distance(self, state.known) > 50 then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, state.known.x, state.known.y, 7, "pushing last known position")
  end
  state.known = nil

  -- No prey, no trail: sweep up items, then push toward the middle of the map
  -- where fights happen. Overdrive pushes the pace.
  local item = best_item(observation)
  if item then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, item.x, item.y, has_effect(self, "overdrive") and 8 or 7, "looting " .. item.type)
  end
  if not state.wander or arena.distance(self, state.wander) < 40 then
    state.wander = arena.random_safe_point(observation, observation.arenaWidth / 2, observation.arenaHeight / 2, 120, 8)
  end
  local held = unstick(observation, { moving = true })
  if held then return held end
  return navigate(observation, state.wander.x, state.wander.y, 6, "hunting")
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
