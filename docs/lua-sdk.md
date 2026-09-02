# Lua Robot SDK

Robot code runs continuously inside player-owned boxes. The box opens one outbound WebSocket to arena control plane. Server sends observations at 10 Hz using protocol version 3. SDK 0.3.2 calls player `decide` function and returns latest action. There are no player-facing turns.

If `decide` raises a Lua error, SDK 0.3.2 keeps the WebSocket connected and sends
a safe no-op action with a `script error: ...` telemetry line. Fix the script
over SSH or deploy another template. A script error is not reported as an agent
network failure.

Edit `/workspace/main.lua` over SSH. Registering a robot snapshots this file to S3-compatible storage, gives box supervisor a short robot configuration, and starts `lua main.lua`. Credentials are not displayed in browser or written into workspace.

`examples/` contains ready-to-copy SDK 0.3.2 robots. Each example exactly
matches the identically named script available from the box console:
`lua-aggressive`, `lua-evasive`, `lua-demolisher`, `lua-patroller`,
`lua-sniper`, and `lua-sentinel`.

## Start

```lua
local arena = require "arena"

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(observation)
    local enemy = arena.nearest_enemy(observation)
    if enemy then return arena.approach(observation, enemy, 8) end
    return arena.action({ turn = 12 })
  end,
})
```

To ignore drops or limit automatic pickup to selected item types:

```lua
arena.configure({
  auto_pickup = true,
  pickup_types = { "heal", "repair-core" },
})
```

The default picks up every item on contact. Set `auto_pickup = false` to disable
pickup. There is no pickup action. The server checks contact and applies effects.

## Observation

Every observation has `type`, `version` (3), `requestId`, `matchId`, `tick`, and
`sentAt`. `self` contains the robot ID, name, team, position, heading, HP,
cooldown, connection state, response timing, memory report, equipment, status
effects, and last action. `robots` uses the same robot state for visible
opponents and teammates. Living cloaked enemies are hidden from `robots` unless
your robot carries a `radar` effect from a scanner pickup.

Map-aware matches also include:

- `arenaWidth`, `arenaHeight`, and `mapId`
- `obstacles`, with `shape`, position, and circle radius or rectangle dimensions
- `items`, with ID, type, position, active state, rarity, respawn time, and
  effect time. Spawner loot is randomized per match seed and occasionally
  bursts as a `supply_drop` event
- `hazards`, with ID, type, rectangle position and size, and damage
- `mines`, with `mineId`, `ownerId`, `team`, position, `spawnTick`, `armTick`,
  and `active`
- match status flags such as `overtime` and the `zone` state
  (`active`, `x`, `y`, `radius`, `damage`)

Protocol v3 adds these fields (mirrored on `self` and top level):

- `dashCharges` and `mineCharges`: current dash and mine charges
- `scanResult`: last scan report, `nil` before the first scan. Shape:
  `{ x, y, radius, items, enemies, mines, hazards }`. Scanned entries carry
  positions, type or team, HP, and distance from the scan center; scanned
  enemies include a `cloaked` flag. The report persists until replaced
- `events`: recent per-robot event feed, oldest first. Entries are
  `{ tick, type, robotId, targetId, itemId, damage, value, x, y, message }`
- `messages`: team relay messages addressed to this robot this tick
- `visionRange`: this robot's per-tick perception radius backing the
  observation (mirrored at the top level and on `self`); see Vision below

The server may add fields in later protocol versions. Robot code should ignore
unknown fields and treat missing arrays as empty.

After SDK 0.3.2 identifies itself in its first action, the server stops
repeating `obstacles` and `hazards` for that WebSocket session. The SDK caches
them and adds both arrays to every observation before it calls `decide`.
Scripts using the SDK continue to see the same observation shape. A reconnect
starts a new session and receives the layout again. Older SDK versions keep
receiving both arrays every tick.

## Vision

Robots perceive the arena through a per-tick vision range, default 320 units
(config clamps to 60..1000). Enemies, items, projectiles, and mines beyond the
range are absent from the observation entirely — the server filters before
sending. Teammates are always visible, and obstacles, hazards, and the zone
are always reported in full regardless of range. Area scans ignore vision: the
`scan` intent reports everything inside its radius, visible or not.

Two pickups extend sight:

- `scope` (rare): `optics` effect for 150 ticks, vision ×2
- `scanner` (rare): `radar` effect for 150 ticks, reveals cloaked enemies in
  `robots` and in scans, and vision ×1.75

`observation.visionRange` (also mirrored as `observation.self.visionRange`)
carries the observer's current range for this tick. Helpers:

- `arena.vision_range(obs)` returns the effective range, `0` when the
  observation carried none (treat sight as unlimited):
  `local range = arena.vision_range(obs)`
- `arena.in_vision(obs, x, y)` reports whether a point is inside the range;
  always true when the range is unknown:
  `if arena.in_vision(obs, item.x, item.y) then chase(item) end`
- `arena.can_see(obs, robot)` combines `in_vision` with
  `arena.line_of_sight`, so cover matters too:
  `if arena.can_see(obs, enemy) then return arena.fire_at(enemy) end`
- `arena.visible_enemies(obs)` returns live opponents inside the vision range,
  nearest first, each with a `distance` field:
  `for _, enemy in ipairs(arena.visible_enemies(obs)) do ... end`

## Actions

`decide` returns one action per observation. The server clamps `move` to
-4..8 and `turn` to -18..18, and reuses the last valid action when a response
misses the 150 ms deadline.

v3 intent fields (all optional, combinable with movement and fire):

- `dash` (bool): spend one dash charge to jump 40 units along the facing.
  Dash cooldown is 40 ticks, at most 2 charges; `dash_cell` pickups add one
- `deploy` (`"mine"`): drop a proximity mine at the current position. Requires
  mine charges (3 granted by a `weapon_mine_layer` pickup). Mines arm 30 ticks
  after deploy, explode for 50 damage within 70 units of the first non-owner,
  and expire after 600 ticks. At most 20 mines exist per match
- `scan` (`{ x, y, radius }`): request an area report around a point. The
  server clamps radius to 40..400 and blocks repeat scans for 60 ticks. The
  report replaces `scanResult` on the robot
- `message` (string): at most 128 bytes, relayed to teammates on the next tick

## Items

Common: `heal` (+25 HP, skipped in overtime), `repair-core` (+15 HP, skipped in
overtime), `battery` (clears fire cooldown and removes burn, slow, and emp).
Basic weapon drops (`weapon_plasma`, `weapon_machine_gun`, `weapon_incendiary`,
`weapon_cryo`, `weapon_emp`, `weapon_cannon`) are also common.

Rare: `shield` (+50 shield, capped at 50), `overdrive` (1.5x move speed, 80
ticks), `rapid_fire` (0.5x cooldown, 60 ticks), `medkit` (+60 HP, skipped at
full HP), `nano_repair` (regen 2 HP per tick for 150 ticks), `armor_plate`
(0.6x incoming damage, 150 ticks), `scanner` (radar 150 ticks: reveals cloaked
enemies in `robots` and in scans, plus vision ×1.75), `scope` (optics 150
ticks: vision ×2), `dash_cell` (+1 dash charge, skipped at 2),
`weapon_shotgun`.

Epic: `cloak` (hidden from robots and scans for 90 ticks; firing a shot or
taking any hit breaks it), `teleport_beacon` (teleport to a seeded open spot),
`berserker_charm` (berserk 80 ticks: 1.5x outgoing damage, 1.25x damage taken),
`vampiric_fang` (heal 25% of damage dealt for 100 ticks), `frenzy` (overdrive
plus rapid fire for 60 ticks), `weapon_grenade`, `weapon_mine_layer`,
`weapon_railgun`.

Weapon pickups swap the robot weapon; `weapon_mine_layer` also grants 3 mine
charges. Full weapon stats are in the Weapons table below.

## Weapons

| weapon | damage | cooldown | speed | range | notes |
| --- | --- | --- | --- | --- | --- |
| plasma | 25 | 8 | 24 | 864 | default |
| cannon | 60 | 20 | 14 | 700 | knockback 28 |
| machine_gun | 8 | 2 | 32 | 550 | spread 3 |
| railgun | 45 | 25 | hitscan | 1000 | instant hit |
| incendiary | 15 | 12 | 20 | 600 | burn 50 ticks |
| cryo | 12 | 12 | 20 | 600 | slow 50 ticks |
| emp | 5 | 15 | 18 | 500 | blocks firing 30 ticks |
| shotgun | 10 x 5 pellets | 14 | 30 | 400 | spread 12 |
| grenade | 60 | 25 | 10 | 500 | blast radius 80 |
| mine_layer | 0 | 20 | - | - | deploys mines via `deploy` intent |

## Status effects

| effect | meaning |
| --- | --- |
| shield_decay | shield drains 0.25 per tick |
| burn | 2 damage per tick |
| slow | 0.5x move speed |
| emp | cannot fire |
| overdrive | 1.5x move speed |
| rapid_fire | 0.5x fire cooldown |
| regen | 2 HP per tick |
| armor | 0.6x incoming damage |
| berserk | 1.5x outgoing damage, 1.25x damage taken |
| vampiric | heal 25% of damage dealt |
| cloak | hidden from robots and scans until it breaks |
| radar | see cloaked enemies in `robots` and in scan reports; vision ×1.75 |
| optics | vision ×2 (granted by the `scope` pickup) |

Effects appear on robots as `{ type, ticks, magnitude, sourceId }`.

## Maps and hazards

Fixed maps: `open-field`, `four-corners`, `corridors`, `pillars`, `crater`,
`bunker-line`, `crossing-fire`, `vault`. Procedural maps generated from the
match seed: `random-maze`, `random-rooms`, `random-bunkers`.

Hazard types: `damage` and `damage-edge` deal periodic damage while inside
(crater's perimeter is a `damage-edge`), `slow-field` applies slow while
inside, and `spike` deals periodic damage on a 6-tick cadence. Use
`arena.hazard_at` to check whether a point is inside a hazard.

## Events

`events` on the observation lists recent entries for your robot, oldest first.
Types include combat events (`shot_fired`, `hit`, `critical_hit`,
`railgun_hit`, `projectile_blocked`, `explosion`, `robot_destroyed`,
`kill_streak`, `bounty_claimed`), economy events (`item_spawned`,
`item_respawned`, `item_picked_up`, `supply_drop`, `teleport`), v3 events
(`regen_tick`, `vampiric_heal`, `dash`, `mine_deployed`, `mine_exploded`,
`mine_expired`, `scan_performed`), and world events (`zone_damage`,
`hazard_damage`, `robot_failed`, `robot_withdrawn`).

## Functions

- `arena.run(config)` connects, reconnects with bounded backoff, and runs `config.decide`.
- `arena.action(options)` builds bounded movement, rotation, fire, equipment, log, and metric fields, plus the v3 `dash`, `deploy`, `scan`, and `message` intents.
- `arena.nearest_enemy(observation)` returns nearest live opponent and distance.
- `arena.nearest_item(observation, type)` returns nearest active item and distance. Omit `type` to accept any item.
- `arena.distance(a, b)` returns Euclidean distance.
- `arena.bearing(a, b)` returns target angle in degrees.
- `arena.line_of_sight(x1, y1, x2, y2, obstacles)` reports whether map geometry blocks a segment. The last observation supplies obstacles when the final argument is omitted.
- `arena.path_to(x, y, observation)` returns a small waypoint list around the first blocking obstacle. It is a local helper, not a server command.
- `arena.fire_at(target, options)` turns toward target and fires.
- `arena.approach(observation, target, speed)` chases and fires.
- `arena.strafe(observation, target, direction)` circles target and fires.

v3 action helpers (options merge into the action like `fire_at`):

- `arena.dash(options)` adds `dash = true`, e.g. to spend a charge while moving:
  `arena.dash({ move = 8, logs = { "escape" } })`
- `arena.deploy_mine(options)` adds `deploy = "mine"`.
- `arena.scan(x, y, radius, options)` adds the scan intent; radius is clamped
  to 40..400 and defaults to 200.
- `arena.send_message(text, options)` adds a team message, trimmed to 128
  bytes on a UTF-8 character boundary.

Query helpers (pure reads on one observation; entries are copied and carry a
`distance` field, sorted nearest first):

- `arena.items_in_area(obs, x, y, radius)` active items around a point.
- `arena.enemies_in_area(obs, x, y, radius)` live opponents around a point.
- `arena.vision_range(obs)` returns the observation's effective vision range,
  or `0` when it carried none (treat sight as unlimited).
- `arena.in_vision(obs, x, y)` reports whether a point lies inside the vision
  range; always true when the range is unknown.
- `arena.can_see(obs, robot)` reports full sight: inside the vision range and
  with clear line of sight.
- `arena.visible_enemies(obs)` returns live opponents inside the vision range,
  nearest first. See Vision for the model behind these helpers.
- `arena.area_report(obs, x, y, radius)` returns
  `{ items, enemies, mines, hazards }` in one call. Mines come from `obs.mines`
  when present, otherwise from `scanResult`; hazards use rectangle centers.
- `arena.projectiles_near(obs, radius)` projectiles within radius of self.
  Observations carry the live projectile list, so this works during matches.
- `arena.mines_near(obs, radius)` active mines within radius of self, each
  with an `armed` flag derived from `armTick`.
- `arena.aim_predict(from, target, projectile_speed, target_vx, target_vy)`
  returns the linear lead point `{ x, y }`: flight time times target drift.
  Observations carry no robot velocity, so pass estimated per-tick velocity
  (for example heading-based, like the bundled templates) or accept the
  current position. A zero or hitscan speed returns the target position.
- `arena.dodge(obs)` returns a perpendicular escape point from the most
  imminent projectile, or `nil` when nothing threatens.
- `arena.danger_level(obs)` counts projectiles whose path passes within 30
  units of self while approaching. Zero means nothing is aimed at the current
  position.
- `arena.zone_status(obs)` returns `nil` without a zone, otherwise
  `{ inside, distanceToEdge, radius, x, y, closing }`. `distanceToEdge` is
  negative outside the zone; `closing` compares this tick's radius with the
  previous tick's.
- `arena.hazard_at(obs, x, y)` returns the hazard rectangle covering the
  point, or `nil`.
- `arena.teammates(obs)` returns live same-team robots, excluding self.
- `arena.random_safe_point(obs, x, y, radius, tries)` returns a deterministic
  open point near `(x, y)`: an LCG seeded from the coordinates samples ring
  candidates and the first one clear of obstacles and with line of sight wins.
  Same inputs always give the same point; no wall-clock time is used.

`action` options are `move`, `turn`, `fire`, `target_x`, `target_y`, `logs`,
`equipment`, `memory_mb`, `dash`, `deploy`, `scan`, and `message`. The SDK also
sends its version and current pickup configuration. The server clamps unsafe
movement and turn values. It measures response time independently from
agent-reported compute time.

## Equipment

Phase 1 reports equipment for telemetry and UI: `cannon`, `scanner`,
`light-armor`, and `shield`. Cannon and scanner are default. Equipment-specific
game effects remain planned until balance tests exist; item pickups and
weapons carry the real gameplay effects.

## Limits

- Response deadline: 150 ms. Last valid action is reused on timeout.
- Maximum movement: 8 units per update.
- Maximum turn: 18 degrees per update.
- Dash: 2 charges, 40-tick cooldown, 40 units per dash.
- Mines: 3 charges per mine_layer pickup, 20 mines per match.
- Scan: radius 40..400, 60-tick cooldown.
- Team message: 128 bytes, relayed next tick.
- Agent token authenticates one robot. Never commit it.
- Box limit: 1 CPU, 512 MB RAM, 128 PIDs, and monitored 1 GB workspace.
- Production uses `wss://`; plain `ws://` is local-only.
