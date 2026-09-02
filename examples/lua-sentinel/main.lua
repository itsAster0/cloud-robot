local arena = require "arena"

-- Sentinel: a guardian that plants itself on defensible ground near the
-- middle of the map, hugs cover, sweeps radar scans to expose cloaked
-- enemies, slips into cloak when hurt, sidesteps incoming fire, and relays
-- enemy positions to teammates over the team channel. It only leaves the
-- post for prey that is close or already bleeding; everything else is fired
-- on from where it stands. Terse robot-speech only. Randomness is seeded
-- from the robot ID without wall-clock time.

local seed = 0
local robot_id = os.getenv("ROBOT_ID") or "sentinel"
for index = 1, #robot_id do
  seed = (seed * 131 + robot_id:byte(index)) % 2147483647
end
math.randomseed(seed)

local PROJECTILE_SPEED = { plasma = 24, cannon = 14, machine_gun = 32, incendiary = 20, cryo = 20, emp = 18, railgun = 0, shotgun = 30, grenade = 10 }
-- Scanner, scope, and cloak outrank weapons: the sentinel wins by information.
local ITEM_SCORE = {
  scanner = 110, cloak = 100, scope = 75, heal = 90, medkit = 85, nano_repair = 65,
  ["repair-core"] = 60, shield = 70, battery = 40, overdrive = 65, rapid_fire = 65, dash_cell = 60,
  weapon_railgun = 100, weapon_cannon = 80, weapon_shotgun = 70, weapon_incendiary = 55,
  weapon_cryo = 50, weapon_emp = 45, weapon_machine_gun = 35, weapon_plasma = 30,
}
local RELAY_INTERVAL = 30
local DOWN_LINES = { "target down.", "threat removed.", "silenced." }

local state = {
  last_x = nil, last_y = nil, stuck = 0, escape = 0, escape_turn = 0, anchor = nil,
  relay_at = 0, next_scan = 0, scanned_at = nil, kills = 0, line = nil,
}

local function has_effect(self, name)
  for _, effect in ipairs(self.effects or {}) do
    if effect.type == name and effect.ticks > 0 then return true end
  end
  return false
end

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

local function item_score(obs, item)
  local self, base = obs.self, ITEM_SCORE[item.type]
  if not base then return 0 end
  if item.type == "heal" or item.type == "repair-core" or item.type == "medkit" then
    if obs.overtime or self.hp >= self.maxHp then return 0 end
    if self.hp < self.maxHp * 0.5 then base = base * 2 end
  end
  if item.type == "nano_repair" and self.hp >= self.maxHp then return 0 end
  if item.type == "shield" and self.shield >= 50 then return 0 end
  if item.type == "scanner" and has_effect(self, "radar") then return 0 end
  if item.type == "cloak" and has_effect(self, "cloak") then return 0 end
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

-- Aims where the enemy will be; damped because enemies rarely hold a line.
local function aim_point(obs, enemy)
  local speed = PROJECTILE_SPEED[obs.self.weapon or "plasma"] or 14
  if speed <= 0 then return enemy.x, enemy.y end
  local radians = enemy.heading * math.pi / 180
  local lead = arena.aim_predict(obs.self, enemy, speed, math.cos(radians) * 5.6, math.sin(radians) * 5.6)
  return lead.x, lead.y
end

-- The post is defensible ground: hugging the obstacle nearest the map center
-- gives one covered flank, with the center itself as the fallback.
local function pick_anchor(obs)
  local center = { x = obs.arenaWidth / 2, y = obs.arenaHeight / 2 }
  local cover, cover_distance
  for _, obstacle in ipairs(obs.obstacles or {}) do
    local ox, oy = obstacle.x + (obstacle.width or 0) / 2, obstacle.y + (obstacle.height or 0) / 2
    if obstacle.radius then ox, oy = obstacle.x, obstacle.y end
    local distance = arena.distance(center, { x = ox, y = oy })
    if not cover_distance or distance < cover_distance then cover, cover_distance = { x = ox, y = oy }, distance end
  end
  if cover and cover_distance < 220 then
    return arena.random_safe_point(obs, cover.x, cover.y, 90, 12)
  end
  return arena.random_safe_point(obs, center.x, center.y, 80, 12)
end

-- Cloak discipline: firing or taking a hit breaks the cloak, so while hidden
-- the sentinel holds fire and drifts back to its post.
local function cloaked_hold(obs)
  local self = obs.self
  local turn = ((arena.bearing(self, state.anchor) - self.heading + 540) % 360) - 180
  local move = arena.distance(self, state.anchor) > 60 and 5 or 0
  return arena.action({ move = move, turn = move > 0 and turn or 0, logs = { "holding under cloak" } })
end

-- The kill counter on self is the kill feed. Celebrations are one sentence.
local function check_kills(obs)
  local kills = obs.self.kills or 0
  if kills > state.kills then
    state.line = DOWN_LINES[(kills % #DOWN_LINES) + 1]
    state.kills = kills
  end
end

local function decide(obs)
  local self = obs.self
  check_kills(obs)

  if not state.anchor then
    state.anchor = pick_anchor(obs)
  end

  -- Zone collapse outranks everything; standing outside is free damage.
  if not zone_safe(obs) then
    local held = unstick(obs, { moving = true })
    if held then return held end
    return navigate(obs, obs.zone.x, obs.zone.y, 7, "returning to zone")
  end

  -- Sidestep incoming fire: dash along the dodge point when a cell is
  -- stored, otherwise walk there the slow way.
  if arena.danger_level(obs) > 0 then
    local escape = arena.dodge(obs)
    if escape then
      if (self.dashCharges or 0) > 0 then
        local turn = ((arena.bearing(self, escape) - self.heading + 540) % 360) - 180
        return arena.dash({ move = 8, turn = turn, logs = { "dodging with dash" } })
      end
      local held = unstick(obs, { moving = true })
      if held then return held end
      return navigate(obs, escape.x, escape.y, 8, "dodging projectile")
    end
  end

  -- Hurt: slip into a cloak or grab a heal before rejoining the fight.
  if self.hp < self.maxHp * 0.5 then
    local recover = arena.nearest_item(obs, "cloak") or arena.nearest_item(obs, "heal") or arena.nearest_item(obs, "repair-core")
    if recover and not obs.overtime then
      local held = unstick(obs, { moving = true })
      if held then return held end
      return navigate(obs, recover.x, recover.y, 7, "grabbing " .. recover.type)
    end
  end

  -- Vision gating: only robots inside our sight can be engaged or relayed.
  -- visible_enemies returns copies sorted nearest first, each with a distance.
  local visible = arena.visible_enemies(obs)
  local enemy = visible[1]
  local enemy_distance = enemy and enemy.distance or nil

  -- Keep teammates informed: relay the nearest enemy position on a fixed
  -- cadence, merged into the combat action so no tick is wasted.
  local relay = nil
  if enemy and obs.tick >= state.relay_at and not has_effect(self, "cloak") then
    state.relay_at = obs.tick + RELAY_INTERVAL
    relay = string.format("%s@%d,%d hp%d d%d", enemy.name, math.floor(enemy.x), math.floor(enemy.y), enemy.hp, math.floor(enemy_distance))
  end

  if enemy then
    if has_effect(self, "cloak") then
      return cloaked_hold(obs)
    end
    -- Guardians only leave the post for prey that is close or bleeding.
    local wounded = (enemy.hp or 0) < (enemy.maxHp or 1) * 0.5
    local pursuit = enemy_distance <= 160 or wounded
    local seen = arena.can_see(obs, enemy)
    if pursuit and not seen then
      local held = unstick(obs, { moving = true })
      if held then return held end
      return navigate(obs, enemy.x, enemy.y, 6, "closing to sightline")
    end
    if seen then
      local held = unstick(obs, { moving = true })
      if held then return held end
      local x, y = aim_point(obs, enemy)
      -- Healthy and far: fire from the post, but do not chase.
      if not pursuit then
        local line = state.line
        state.line = nil
        local spec = { move = 0, turn = 6, fire = true, target_x = x, target_y = y, logs = { line or ("holding. target at " .. math.floor(enemy_distance)) } }
        if relay or line then return arena.send_message(relay or line, spec) end
        return arena.action(spec)
      end
      local line = state.line
      state.line = nil
      if enemy_distance > 160 then
        local spec = { move = 6, fire = true, target_x = enemy.x, target_y = enemy.y, logs = { line or ("closing on " .. enemy.name) } }
        if relay or line then return arena.send_message(relay or line, spec) end
        return arena.action(spec)
      end
      local spec = { move = 4, turn = 8, fire = true, target_x = x, target_y = y, logs = { line or ("engaging " .. enemy.name) } }
      if relay or line then return arena.send_message(relay or line, spec) end
      return arena.action(spec)
    end
  end

  -- Quiet field: short detours for scanner, cloak, and heals only.
  local item = best_item(obs, 200)
  if item then
    local held = unstick(obs, { moving = true })
    if held then return held end
    return navigate(obs, item.x, item.y, 6, "detour for " .. item.type)
  end

  -- Fresh radar sweep around the post; radar exposes cloaked contacts.
  if obs.tick >= state.next_scan then
    state.next_scan, state.scanned_at = obs.tick + 60, obs.tick
    local held = unstick(obs, { moving = true })
    if held then return held end
    return arena.scan(state.anchor.x, state.anchor.y, 300, { move = 4, logs = { "sweeping the perimeter" } })
  end
  local report = obs.scanResult
  if report and state.scanned_at and obs.tick > state.scanned_at and obs.tick - state.scanned_at <= 50 then
    for _, contact in ipairs(report.enemies or {}) do
      local held = unstick(obs, { moving = true })
      if held then return held end
      return navigate(obs, contact.x, contact.y, 5, "pressing scan contact")
    end
  end

  -- Hold the post: re-anchor when drifted, otherwise orbit it slowly.
  if arena.distance(self, state.anchor) > 90 then
    local held = unstick(obs, { moving = true })
    if held then return held end
    return navigate(obs, state.anchor.x, state.anchor.y, 6, "returning to post")
  end
  local orbit = arena.random_safe_point(obs, state.anchor.x, state.anchor.y, 60, 8)
  local held = unstick(obs, { moving = true })
  if held then return held end
  return navigate(obs, orbit.x, orbit.y, 4, "holding post")
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
