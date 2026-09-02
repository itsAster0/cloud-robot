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
- `POST /api/me/box/release`
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
`aggressive|evasive|camper|mixed` (`mixed` derives a stable per-bot persona
from each robot's ID, so one match fields varied opponents), `regenPerTick` is
0–10 (regen applies after 50 ticks without taking damage).

`mode` accepts `duel` (default), `squad`, and `solo`:

- `squad` ignores `bots` and seeds ten bots, five per side, on the Corridors
  map. Players register with `POST /api/matches/{matchId}/robots` and choose
  red or blue; the newest bot on that team is removed and the player takes its
  slot. A team accepts at most five robots. Auto-start waits for a human on
  each side.
- `solo` uses `bots` (0–7) as free-for-all opponents. Every robot gets its own
  `solo-NN` team and the requested team is ignored. With zero bots the match
  is a single-robot sandbox that ends at the tick limit or when the robot dies.
- Squad and solo matches are flagged `practice` and never update ratings.

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
requeue their human players automatically instead of dropping them. A
`matched` entry is removed once its match starts running, so `GET /api/queue`
reports `idle` again as soon as the match is over and players can requeue.

## Viewer WebSocket

`GET /ws/matches/{matchId}` is anonymous. The server sends current match state
on connection, then versioned snapshots while the match runs. Snapshot version
3 includes robots (each carrying its per-tick `visionRange`, which viewers can
draw as a radar ring), projectiles, map bounds, obstacles, items, mines,
turrets, recent events, zone state with collapse stage, announcements, and
overtime state.

The viewer WebSocket sends an `arena_layout` message before its first dynamic
snapshot. It contains `matchId`, `mapId`, `width`, `height`, and `obstacles`.
Later `snapshot` messages omit `obstacles`; clients cache the layout by match.
The server encodes each dynamic snapshot once and reuses those bytes for every
viewer connected to that match.

## Agent WebSocket

`GET /agent/connect/{robotId}` uses the `robot-arena.v1` subprotocol and a robot
bearer token. The token belongs to one robot and never reaches the browser.

The server sends an observation every tick. The first observation on each
connection includes static `obstacles` and `hazards`. After an action reports
SDK 0.3.2 or newer, later observations omit those arrays; the SDK caches and
restores them before calling robot code. Older SDK versions keep receiving the
arrays. The agent returns an action with the
same `requestId`. Actions may contain movement, turning, targeting, firing,
telemetry, equipment, SDK version, pickup preferences, dash, mine deployment,
area scans, and team messages. Observations add mines, projectiles, scan
reports, charge counts, recent per-robot events, and relayed team messages on
top of the shared world state. The server clamps movement and remains
authoritative for position, damage, score, items, and match state.

The response deadline is 150 ms. A missed deadline reuses the last action. A
disconnected agent has a 30-second reconnect window before the engine fails it.
