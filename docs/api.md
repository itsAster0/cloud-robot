# API reference

The browser uses JSON over HTTP. Match viewers and robot agents use separate
WebSocket endpoints. Public read endpoints do not require a WorkOS session.
Mutations and player-owned resources do.

## Public HTTP endpoints

- `GET /healthz`
- `GET /readyz`
- `GET /api/cloud/status`
- `GET /api/scripts`
- `GET /api/matches?status=<status>&limit=<n>`
- `GET /api/matches/{matchId}`
- `GET /api/matches/{matchId}/replay`
- `GET /api/profiles/{handle}`
- `GET /api/leaderboard?mode=duel&limit=<n>`

Match status accepts `lobby`, `queued`, `running`, `finished`, or `failed`.
Running entries in the matches list carry a `viewers` count.
List limits range from 1 to 100.

## Authenticated HTTP endpoints

- `POST /api/matches`
- `POST /api/matches/{matchId}/robots`
- `DELETE /api/matches/{matchId}/robots`
- `POST /api/matches/{matchId}/start`
- `GET /api/queue`
- `POST /api/queue`
- `DELETE /api/queue`
- `POST /api/me/box`
- `GET /api/me/box`
- `PUT /api/me/box/ssh-key`
- `POST /api/me/box/restart`
- `GET /api/me/box/main.lua`
- `PUT /api/me/box/main.lua`
- `GET /api/me/box/scripts`
- `POST /api/me/box/scripts/{versionId}/restore`

`GET /api/admin/status` also requires the user ID in `ADMIN_USER_IDS`.

Create a match with map, arena, practice, bot, and combat-rule settings:

```json
{
  "mode": "duel",
  "mapId": "corridors",
  "arenaWidth": 1000,
  "arenaHeight": 600,
  "practice": true,
  "bots": 1,
  "botDifficulty": "fighter",
  "botPersonality": "aggressive",
  "friendlyFire": false,
  "regenPerTick": 1,
  "rammingDamage": true
}
```

Combat options are optional and default to today's baseline: friendly fire off,
no regen, no ramming. Valid values: `botPersonality` is
`aggressive|evasive|camper`, `regenPerTick` is 0–10 (regen applies after 50
ticks without taking damage).

Queue a box-backed Lua robot:

```json
{
  "displayName": "Waypoint",
  "mode": "duel",
  "runtime": "lua5.4",
  "startCommand": "lua main.lua"
}
```

The queue pairs the first two players. It adds a server bot after
`QUEUE_BOT_FILL_SECONDS` and cancels a match when agents miss the
`QUEUE_CONNECT_GRACE_SECONDS` connection window. Cancelled queue matches
requeue their human players automatically instead of dropping them.

## Viewer WebSocket

`GET /ws/matches/{matchId}` is anonymous. The server sends current match state
on connection, then versioned snapshots while the match runs. Snapshot version
2 includes robots, projectiles, map bounds, obstacles, items, recent events,
zone state, announcements, and overtime state.

## Agent WebSocket

`GET /agent/connect/{robotId}` uses the `robot-arena.v1` subprotocol and a robot
bearer token. The token belongs to one robot and never reaches the browser.

The server sends an observation every tick. The agent returns an action with the
same `requestId`. Actions may contain movement, turning, targeting, firing,
telemetry, equipment, SDK version, and pickup preferences. The server clamps
movement and remains authoritative for position, damage, score, items, and match
state.

The response deadline is 150 ms. A missed deadline reuses the last action. A
disconnected agent has a 30-second reconnect window before the engine fails it.
