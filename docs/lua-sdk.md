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

## Observation

`observation.self` contains position, heading, HP, cooldown, response latency, memory report, equipment, and last action. `observation.robots` contains visible match robot state. Phase 1 exposes complete state; field-of-view filtering comes later.

## Functions

- `arena.run(config)` connects, reconnects with bounded backoff, and runs `config.decide`.
- `arena.action(options)` builds bounded movement, rotation, fire, equipment, log, and metric fields.
- `arena.nearest_enemy(observation)` returns nearest live opponent and distance.
- `arena.distance(a, b)` returns Euclidean distance.
- `arena.bearing(a, b)` returns target angle in degrees.
- `arena.fire_at(target, options)` turns toward target and fires.
- `arena.approach(observation, target, speed)` chases and fires.
- `arena.strafe(observation, target, direction)` circles target and fires.

`action` options: `move`, `turn`, `fire`, `target_x`, `target_y`, `logs`, `equipment`, and `memory_mb`. Server clamps unsafe movement and turn values. Server measures end-to-end response time independently from agent-reported compute time.

## Equipment

Phase 1 reports equipment for telemetry and UI: `cannon`, `scanner`, `light-armor`, and `shield`. Cannon and scanner are default. Equipment-specific game effects remain planned until balance tests exist.

## Limits

- Response deadline: 150 ms. Last valid action is reused on timeout.
- Maximum movement: 8 units per update.
- Maximum turn: 18 degrees per update.
- Agent token authenticates one robot. Never commit it.
- Box limit: 1 CPU, 512 MB RAM, 128 PIDs, and monitored 1 GB workspace.
- Production uses `wss://`; plain `ws://` is local-only.
