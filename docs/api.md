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

## Protocol v4 migration

The legacy endpoints remain available. New Rust-backed matches use these routes:

- `POST /api/v4/matches`: create an unranked lobby. Body includes `mode`, `seed`
  (0 means random), `capacity` (1..256), `width`, `height`, `durationSeconds`,
  `friendlyFire`, `liveEdit`, plus map density `siteCount` (0 = auto: 4 small /
  64 large, else 1..256), `coverPerSite` (0..12, default 8), and `lootPerSite` (0..32).
  Cover composes open corners, pillars, lanes, arcs, hedgerows, glass panes,
  and rock on slotted rings plus wild mixed clusters between sites. Each site
  also has a `biome` (urban, industrial, forest, desert) that fills the rest of
  its cell with themed structures: walled rooms with doorways, ruined corners,
  container yards, warehouses, barrel stacks, groves, rock formations, and
  sandbag outposts. District pieces keep 90 units of clearance from other
  structures and hazards, and a world-wide budget of 8,000 pieces caps them.
  Street crosses through each site stay open. `coverPerSite` scales district
  density; 0 leaves open ground. Segments carry a visual-only `material`
  (wall, hedge, glass, rock, brick, metal, container, crate, barrel, tree,
  sandbag) with identical collision. Ground textures, roads, and site pads
  render client-side from `sites`.
- v4 snapshots carry static layout (`obstacles`, `hazards`, `transit`,
  `sites`) on a viewer's first delivery and on revision changes. Stored replay
  pages keep layout on their first frame and on revision changes only; readers
  merge the latest earlier layout into later frames.
  Squad capacity must be divisible by four.
- `POST /api/v4/maps/preview`: render the deterministic geometry (sites,
  cover structures, loot, hazard fields, zone phases) for the same config
  without starting a match. The browser shows it instantly on lobby creation;
  a client-side seed of 0 is materialized before creating so preview and lobby
  match exactly.
- `POST /api/matches/{id}/robots`: register the immutable source with
  `sdkVersion: "0.4.0"` and `loadout: { chassis, weapon, modules, utilities }`.
  Existing ownership checks apply. Empty slots become server bots at start.
- `POST /api/matches/{id}/start`: owner starts the registered roster.
- `POST /api/v4/matches/{id}/control`: sandbox owner sends `pause`, `resume`, or
  `step` in `command`.
- `GET /api/v4/matches/{id}/view`: authenticated owner's live observation.
- `GET /ws/matches/{id}`: public full snapshots delayed by 100 simulation ticks.
- `GET /api/v4/matches/{id}/final`: completed public final state.
- `GET /api/v4/matches/{id}/replay?page=N`: completed public frames; each page
  spans 100 simulation ticks and holds up to ten frames.
- `GET /api/v4/matches/{id}/trace?tick=N`: owner-only replay reconstruction;
  returns observation, accepted action, and verified state hash.
- `POST /api/v4/matches/{id}/edit?apply=false`: administrator validates an edit.
  Use `apply=true` to schedule it. Body requires `expectedRevision` and a future
  `effectiveTick`; operations are upserts/removals for supported world objects.
  Scheduled edits are revalidated at application. Rejected edits remain audited.
- `GET /api/v4/me/script`: own workspace source and SHA-256 revision.
- `PUT /api/v4/me/script`: save `{source, revision}` and persist a version.
- `POST /api/v4/me/script/validate`: syntax-check `{source}` in the Lua box.
- `GET/PUT /api/v4/me/loadout`: read/save the account's default build.

V4 agents connect through the existing credential-protected URL with WebSocket
subprotocol `robot-arena.v4` and header `X-Robot-SDK-Version: 0.4.0`. Each action
has `type: "action"`, `version: 4`, `sdkVersion: "0.4.0"`, `sequence`,
`observedTick`, and `action`. The authenticated connection selects the robot;
agent payloads cannot assign identity. See [SDK 0.4](lua-sdk-v4.md).

Implementation limits and unverified qualification targets are listed in
[the v2 status document](arena-v2.md).

### V4 spectator regions

V4 match WebSockets accept camera messages `{x, y, width, height}` in world units.
Centers must be inside 48000 by 30000; dimensions must be at least 100 and no
larger than those bounds. Invalid messages close the connection. Each 10 Hz
snapshot replaces robots, projectiles, containers, mines and fields inside the
camera rectangle with a 256-unit margin. The `overview` robot roster arrives at
1 Hz and on the first snapshot. Public obstacles, hazards and transit arrive on
the first snapshot and whenever `revision` changes. Clients retain that layout
until replacement, and discard it on reconnect. All data comes from the existing
5-second-delayed public stream. Camera subscriptions do not change participant
observation permissions. Legacy WebSocket snapshots retain their existing format.
