local arena = require "arena"

-- Demolisher: a trap-layer with a flair for drama. It drops mines while
-- backing away under pressure, gardens the map center with fresh charges
-- when the field is quiet, and leaves a parting gift on the zone rim every
-- time the circle starts closing. It hunts the grenade launcher, keeps a
-- grenade standoff band once it has one, and narrates its work in telemetry
-- and on the team channel. Firing is gated on full sight: inside the vision
-- range with clear line of sight. Randomness is seeded from the robot ID.

local seed = 0
local robot_id = os.getenv("ROBOT_ID") or "demolisher"
for index = 1, #robot_id do
  seed = (seed * 131 + robot_id:byte(index)) % 2147483647
end
math.randomseed(seed)

-- Projectile speeds mirror the server weapon table; grenades crawl at 10, so
-- aiming leans on the SDK lead helper, and railgun is hitscan.
local PROJECTILE_SPEED = { plasma = 24, cannon = 14, machine_gun = 32, incendiary = 20, cryo = 20, emp = 18, railgun = 0, shotgun = 30, grenade = 10 }
-- Grenade launcher outranks everything; mine layers feed the deploy loop.
local ITEM_SCORE = {
  heal = 90, ["repair-core"] = 60, medkit = 85, nano_repair = 75, scope = 55,
  shield = 70, battery = 40, overdrive = 65, rapid_fire = 65, scanner = 55, dash_cell = 70,
  weapon_grenade = 120, weapon_railgun = 100, weapon_mine_layer = 95, weapon_cannon = 80,
  weapon_shotgun = 75, weapon_incendiary = 55, weapon_cryo = 50, weapon_emp = 45,
  weapon_machine_gun = 35, weapon_plasma = 30,
}
local BOOM_LINES = { "boom. encore.", "someone stepped on my art.", "the floor bites.", "did you hear that too?" }

local state = {
  last_x = nil, last_y = nil, stuck = 0, escape = 0, escape_turn = 0, wander = nil,
  next_scan = 0, scanned_at = nil, anchor = nil, last_mine = -999, kills = 0, taunt = nil,
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

-- Worth of an item right now: heals only matter when hurt or absent in
-- overtime, shields when the existing one decayed, charges when not full.
local function item_score(obs, item)
  local self, base = obs.self, ITEM_SCORE[item.type]
  if not base then return 0 end
  if item.type == "heal" or item.type == "repair-core" or item.type == "medkit" then
    if obs.overtime or self.hp >= self.maxHp then return 0 end
    if self.hp < self.maxHp * 0.5 then base = base * 2 end
  end
  if item.type == "nano_repair" and self.hp >= self.maxHp then return 0 end
  if item.type == "shield" and self.shield >= 50 then return 0 end
  if item.type == "dash_cell" and (self.dashCharges or 0) >= 2 then return 0 end
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

-- Aims where the enemy will be: heading-estimated velocity damped 30%, fed
-- through the SDK lead helper so slow grenades still land near the mark.
local function aim_point(obs, enemy)
  local speed = PROJECTILE_SPEED[obs.self.weapon or "plasma"] or 14
  if speed <= 0 then return enemy.x, enemy.y end
  local radians = enemy.heading * math.pi / 180
  local lead = arena.aim_predict(obs.self, enemy, speed, math.cos(radians) * 5.6, math.sin(radians) * 5.6)
  return lead.x, lead.y
end

-- The kill counter on self is the kill feed; every entry earns a broadcast.
local function check_kills(obs)
  local kills = obs.self.kills or 0
  if kills > state.kills then
    state.taunt = BOOM_LINES[(kills % #BOOM_LINES) + 1]
    state.kills = kills
  end
end

local function decide(obs)
  local self = obs.self
  check_kills(obs)

  if not state.anchor then
    state.anchor = arena.random_safe_point(obs, obs.arenaWidth / 2, obs.arenaHeight / 2, 80, 12)
  end

  -- Zone collapse outranks everything; standing outside is free damage.
  if not zone_safe(obs) then
    local held = unstick(obs, { moving = true })
    if held then return held end
    return navigate(obs, obs.zone.x, obs.zone.y, 7, "returning to zone")
  end

  local enemy, enemy_distance = arena.nearest_enemy(obs)

  -- Incoming fire first: spend a dash cell along the SDK dodge point, or
  -- just step aside when charges are spent.
  if arena.danger_level(obs) > 0 then
    local escape = arena.dodge(obs)
    if escape then
      if (self.dashCharges or 0) > 0 then
        local turn = ((arena.bearing(self, escape) - self.heading + 540) % 360) - 180
        return arena.dash({ move = 8, turn = turn, logs = { "dashing clear" } })
      end
      local held = unstick(obs, { moving = true })
      if held then return held end
      return navigate(obs, escape.x, escape.y, 8, "dodging projectile")
    end
  end

  -- Break contact when critical: dash away from the enemy line.
  if self.hp < self.maxHp * 0.35 and enemy and enemy_distance < 150 and (self.dashCharges or 0) > 0 then
    local away = { x = self.x + (self.x - enemy.x), y = self.y + (self.y - enemy.y) }
    local turn = ((arena.bearing(self, away) - self.heading + 540) % 360) - 180
    return arena.dash({ move = 8, turn = turn, logs = { "dash retreat" } })
  end

  -- Under pressure: seed a mine while backing off. The deploy keeps us
  -- moving away from our own blast radius; the cannon still answers.
  if enemy and enemy_distance < 150 and (self.mineCharges or 0) > 0 then
    local away = { x = self.x + (self.x - enemy.x), y = self.y + (self.y - enemy.y) }
    local turn = ((arena.bearing(self, away) - self.heading + 540) % 360) - 180
    local held = unstick(obs, { moving = true })
    if held then return held end
    state.last_mine = obs.tick
    local x, y = aim_point(obs, enemy)
    return arena.deploy_mine({ move = 6, turn = turn, fire = arena.can_see(obs, enemy), target_x = x, target_y = y,
      logs = { "dropping a surprise at " .. math.floor(self.x) .. "," .. math.floor(self.y) } })
  end

  -- Zone discipline, demolisher style: when the rim starts closing and we
  -- are near it, leave a mine behind for anyone cutting the corner late.
  local zone = obs.zone
  if zone and zone.active and (self.mineCharges or 0) > 0 and obs.tick - state.last_mine >= 60 then
    local status = arena.zone_status(obs)
    if status and status.closing and status.inside and status.distanceToEdge < 110 then
      state.last_mine = obs.tick
      local held = unstick(obs, { moving = true })
      if held then return held end
      return arena.deploy_mine({ move = 7, logs = { "a gift for the zone runners" } })
    end
  end

  if enemy then
    -- Fire only with full sight (in vision range plus clear line of sight);
    -- covered robots get hunted on foot, not shot at blind.
    local seen = arena.can_see(obs, enemy)
    local held = unstick(obs, { moving = true })
    if held then return held end
    if not seen then
      return navigate(obs, enemy.x, enemy.y, 7, "hunting " .. enemy.name)
    end
    -- Grenade launcher: lob from a standoff band so the blast does the work.
    if (self.weapon or "plasma") == "grenade" then
      local x, y = aim_point(obs, enemy)
      if enemy_distance < 110 then
        local away = { x = self.x + (self.x - enemy.x), y = self.y + (self.y - enemy.y) }
        local turn = ((arena.bearing(self, away) - self.heading + 540) % 360) - 180
        return arena.action({ move = 5, turn = turn, fire = true, target_x = x, target_y = y, logs = { "too close for grenades" } })
      end
      if enemy_distance > 260 then
        return arena.approach(obs, enemy, 8)
      end
      local taunt = state.taunt
      state.taunt = nil
      local spec = { move = 4, turn = 10, fire = true, target_x = x, target_y = y, logs = { taunt or ("lobbing at " .. enemy.name) } }
      if taunt then return arena.send_message(taunt, spec) end
      return arena.action(spec)
    end
    if enemy_distance > 220 then
      return arena.approach(obs, enemy, 8)
    end
    local x, y = aim_point(obs, enemy)
    local taunt = state.taunt
    state.taunt = nil
    local spec = { move = 6, turn = 12, fire = true, target_x = x, target_y = y, logs = { taunt or ("blasting " .. enemy.name) } }
    if taunt then return arena.send_message(taunt, spec) end
    return arena.action(spec)
  end

  -- Quiet field: garden duty. Standing charges around the anchor claim the
  -- middle of the map; never stack on a mine that is already there.
  if (self.mineCharges or 0) > 0 and obs.tick - state.last_mine >= 120
    and arena.distance(self, state.anchor) < 140 and #arena.mines_near(obs, 90) == 0 then
    state.last_mine = obs.tick
    local held = unstick(obs, { moving = true })
    if held then return held end
    return arena.deploy_mine({ move = 6, logs = { "seeding the garden at " .. math.floor(self.x) .. "," .. math.floor(self.y) } })
  end

  -- Then sweep items, and chase fresh scan contacts.
  local item = best_item(obs)
  if item then
    local held = unstick(obs, { moving = true })
    if held then return held end
    return navigate(obs, item.x, item.y, 7, "looting " .. item.type)
  end
  local report = obs.scanResult
  if report and state.scanned_at and obs.tick > state.scanned_at and obs.tick - state.scanned_at <= 50 then
    for _, contact in ipairs(report.enemies or {}) do
      local held = unstick(obs, { moving = true })
      if held then return held end
      return navigate(obs, contact.x, contact.y, 6, "hunting scan contact")
    end
    for _, scanned in ipairs(report.items or {}) do
      local held = unstick(obs, { moving = true })
      if held then return held end
      return navigate(obs, scanned.x, scanned.y, 6, "chasing scanned " .. scanned.type)
    end
  end

  -- Nothing on the shared list: refresh the area scan on its cooldown.
  if obs.tick >= state.next_scan then
    state.next_scan, state.scanned_at = obs.tick + 60, obs.tick
    local held = unstick(obs, { moving = true })
    if held then return held end
    return arena.scan(self.x, self.y, 300, { move = 6, logs = { "scanning for loot" } })
  end

  -- Roam around the trap garden so the seeds keep getting planted.
  if not state.wander or arena.distance(self, state.wander) < 40 then
    state.wander = arena.random_safe_point(obs, state.anchor.x, state.anchor.y, 150, 8)
  end
  local held = unstick(obs, { moving = true })
  if held then return held end
  return navigate(obs, state.wander.x, state.wander.y, 6, "hunting")
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
