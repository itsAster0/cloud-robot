# Lua Robot SDK

Robot code runs continuously inside player-owned boxes. The box opens one outbound WebSocket to arena control plane. Server sends observations at 10 Hz. SDK calls player `decide` function and returns latest action. There are no player-facing turns.

Edit `/workspace/main.lua` over SSH. Registering a robot snapshots this file to S3-compatible storage, gives box supervisor a short robot configuration, and starts `lua main.lua`. Credentials are not displayed in browser or written into workspace.

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

Every observation has `type`, `version`, `requestId`, `matchId`, and `sentAt`.
`self` contains the robot ID, name, team, position, heading, HP, cooldown,
connection state, response timing, memory report, equipment, status effects,
and last action. `robots` uses the same robot state for visible opponents and
teammates.

Map-aware matches also include:

- `arenaWidth`, `arenaHeight`, and `mapId`
- `obstacles`, with `kind`, position, and circle radius or rectangle dimensions
- `items`, with ID, type, position, active state, respawn time, and effect time. Types include `heal`, `repair-core`, `shield`, `overdrive`, `rapid_fire`, and `weapon_*` drops (`weapon_railgun` is the rarest). Spawner loot is randomized per match seed and occasionally bursts as a `supply_drop` event
- map hazards; some maps (for example Crater) deal periodic `hazard_damage`
  while a robot stands inside the zone
- match status flags such as zone closing or overtime when enabled

The server may add fields in later protocol versions. Robot code should ignore
unknown fields and treat missing arrays as empty.

## Functions

- `arena.run(config)` connects, reconnects with bounded backoff, and runs `config.decide`.
- `arena.action(options)` builds bounded movement, rotation, fire, equipment, log, and metric fields.
- `arena.nearest_enemy(observation)` returns nearest live opponent and distance.
- `arena.nearest_item(observation, type)` returns nearest active item and distance. Omit `type` to accept any item.
- `arena.distance(a, b)` returns Euclidean distance.
- `arena.bearing(a, b)` returns target angle in degrees.
- `arena.line_of_sight(x1, y1, x2, y2, obstacles)` reports whether map geometry blocks a segment. The last observation supplies obstacles when the final argument is omitted.
- `arena.path_to(x, y, observation)` returns a small waypoint list around the first blocking obstacle. It is a local helper, not a server command.
- `arena.fire_at(target, options)` turns toward target and fires.
- `arena.approach(observation, target, speed)` chases and fires.
- `arena.strafe(observation, target, direction)` circles target and fires.

`action` options are `move`, `turn`, `fire`, `target_x`, `target_y`, `logs`,
`equipment`, and `memory_mb`. The SDK also sends its version and current pickup
configuration. The server clamps unsafe movement and turn values. It measures
response time independently from agent-reported compute time.

## Equipment

Phase 1 reports equipment for telemetry and UI: `cannon`, `scanner`, `light-armor`, and `shield`. Cannon and scanner are default. Equipment-specific game effects remain planned until balance tests exist.

## Limits

- Response deadline: 150 ms. Last valid action is reused on timeout.
- Maximum movement: 8 units per update.
- Maximum turn: 18 degrees per update.
- Agent token authenticates one robot. Never commit it.
- Box limit: 1 CPU, 512 MB RAM, 128 PIDs, and monitored 1 GB workspace.
- Production uses `wss://`; plain `ws://` is local-only.
