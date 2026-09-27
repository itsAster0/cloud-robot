# Cloud Robot Arena

Programmable robot combat as a cloud service. Players sign in, get a persistent
Docker SSH box, write a Lua 5.4 robot, and send it into a server-authoritative
arena. Anyone can watch live or replay finished matches in the browser. Local
development uses Floci for AWS-compatible S3, DynamoDB, and SQS.

This is a trusted-reviewer prototype: Docker boxes are not a security boundary
for hostile code. Firecracker isolation is future work.

## Setup

Requirements: Docker with Compose, mise, and a WorkOS AuthKit client ID
(public value).

```sh
mise install            # Go 1.26.7, Rust 1.98.0, Node 26.8.1, pnpm 11.24.0
mise run install        # frontend dependencies
cp .env.example .env    # optional; Compose defaults already match
mise run demo           # build box image and stack, start everything
```

WorkOS dashboard (required before sign-in works):

1. Enable AuthKit on the project.
2. **Redirects:** add `http://localhost:3000` (and `http://localhost:5173` for
   the Vite dev server).
3. **Authentication → Sessions → CORS:** add `http://localhost:3000` as an
   allowed origin. Without it the browser token exchange fails with a CORS
   error.

Services: web `http://localhost:3000`, API `:8080`, Floci `:4566`, provisioner
`:8090` (internal). Box SSH ports start at `22000` on `127.0.0.1`. Check
readiness with `curl http://localhost:3000/readyz` → `{"status":"ready"}`.

Stop and restart without losing workspaces:

```sh
mise run demo:stop      # stops stack and boxes, keeps volumes
mise run demo:start     # boxes are rediscovered on next sign-in
```

## Demo

1. `mise run demo`, open `http://localhost:3000` in two browsers, and sign in
   with two WorkOS accounts.
2. In each browser open **More → SSH & runtime**, wait for the box, and add an
   SSH public key.
3. **Code:** deploy a strategy (balanced, scout, assault, sniper, support,
   sentinel, scavenger) or edit `main.lua`, check syntax, and save. SSH editing
   also works: `ssh -p <port> developer@localhost`.
4. **Loadout:** pick a legal 60-point build.
5. **Matches:** choose a mode, robot count, world size, length, and map
   density; the live preview shows the generated world. Create the lobby,
   share its ID with the second browser, register both robots, and start.
   Empty slots become server bots.
6. Watch both browsers receive the same match. Click any robot on the map or
   in the roster to inspect it. Enable **My robot's live view** to see your
   robot's own observation, weapon heat, and inventory.
7. When it finishes, open **Results** to scrub the replay. Signed-in owners
   can inspect their recorded decision at any tick.
8. Check Floci (`http://localhost:4566`) for the script snapshot, match
   record, replay pages, and box metadata.
9. `mise run demo:stop`, then `mise run demo:start`: workspaces, scripts, and
   finished matches survive.

Logged-out visitors can open public match history, spectate, replays, and the
SDK docs. Signing in is required only for boxes, lobbies, and registration.

## Admin console

Open **More → Admin console** (`/#/admin`) and log in with `ADMIN_USERNAME` /
`ADMIN_PASSWORD` (local default `admin` / `local-admin-change-me`). This
login is separate from WorkOS player sign-in and is disabled when
`ADMIN_PASSWORD` is empty. Change the password before exposing the stack;
the console shows a warning while the default is set. Sessions are signed
tokens that last 8 hours. Five failed logins in a minute pause login.

- **Overview:** uptime, memory, live match workers with their ticks,
  connected agents, viewers, queue, match counts, error and warning totals,
  cloud endpoints.
- **Logs:** a live tail of API server logs, match worker output (Rust stderr
  plus start, finish, and failure events), agent connects and rejected inputs,
  and admin logins. Filter by source, level, match, or text. Logs are kept in
  memory: the last 5,000 entries since the API started.
- **Matches:** recent matches with owners, status, and errors; open a match
  or jump to its worker logs.
- **Boxes:** every robot box container and its recent supervisor and Lua
  agent output, read through the provisioner.

Players manage their box on **More → SSH & runtime**: live CPU, memory,
process, and network graphs; container details; the process list; a
read-only `/workspace` file browser; and box output with filter, errors
only, follow, tail size, clear, copy, and download. Clear hides older lines
in that browser; it does not erase the container log.

## What the arena does

- **The Arena:** one endless free-for-all the server keeps running. Players
  join and leave at any time from **Play → The Arena**. The safe zone drifts
  across a 42000×26250 world: every 90 seconds it glides to a new circle
  that overlaps the old one, so the fight keeps moving; outside it robots
  take 4 HP a second. The zone's radius follows the live population. Bots
  keep at least `ARENA_ROBOTS` (128) robots in play and step aside for
  players; missing bots return together in a wave every 90 seconds;
  players can join until the engine limit of 256 robots. Destroyed robots
  respawn inside the zone after 5 seconds with 3 seconds of spawn
  protection and keep their score. Score: +100 per kill, +5 per second for
  holding the Uplink objective alone (a site inside the zone, moving every
  minute), and a bounty of 50 per streak kill for ending a 3+ kill streak.
  Salvage fades after 30 seconds and site loot restocks every 30 seconds.
  The viewer shows the zone, where it goes next, a kill feed, Uplink and
  bounty markers, and camera modes (selected robot, leader, my robot, free).
- **Modes:** solo and duo/trio/squad battle royale (up to 256 slots, optional
  bot count), quick duel, and sandbox with pause and single-tick stepping.
  Lobbies have invite links. Players can leave any match to free their box.
  All matches are unranked.
- **World:** deterministic from the seed. Seeded climate noise (temperature,
  moisture, development, elevation) picks each site's biome, so biomes form
  regions: urban rooms, industrial container yards, forest groves, desert
  rock and sandbag outposts with burning heat vents, snowfields with pine
  stands, ice boulders, and slowing drifts, and swamps with ponds, dead trees,
  reeds, and bogs. Between sites, high ground raises mountain ridges with a
  pass and wet ground forms lakes; water blocks movement but not shots or
  sight. Roads, hazards, transit links, loot, and a four-phase closing zone
  (none in the Arena) round it out.
- **Robots:** three chassis, nine weapons with heat and cooldown, modules,
  utilities, consumables, equipment swaps, and a 60-point build budget.
- **Server bots** patrol, loot, investigate gunfire, hunt scan contacts, and
  fight at their weapon's range.
- **Lua SDK 0.4:** observations at 10 Hz, intent-only actions, geometry
  helpers backed by a spatial index, cover finding, contact memory, target
  choice, pathing with recovery, and `arena.tactics`, the decision loop every
  shipped strategy builds on. Robots can span several files; registration
  snapshots every module. Two multi-file templates (Zone Runner, Bounty
  Hunter) show the layout. See [SDK 0.4](docs/lua-sdk-v4.md).
- **Browser:** textured renderer with regional streaming, interpolation,
  follow, zoom, minimap, robot inspector, guided match setup, replays, and
  owner decision traces. Clicking a loot crate shows what it holds (crates are
  coloured by contents); clicking a site, hazard, obstacle, or open ground
  explains it and its biome.
- **Players:** an all-time leaderboard (Arena score or all-mode wins),
  public player profiles with recent matches, and an end-of-session podium
  that hands viewers to the next Arena session.

## Architecture

```text
Svelte 5 browser ──HTTP/WS──▶ Go API ──Unix socket──▶ Rust arena worker (one per match)
                                 │
                                 ├──▶ provisioner ──▶ Docker ──▶ robot boxes (SSH, Lua agent)
                                 │                                   │
                                 │◀──────────── agent WebSocket ─────┘
                                 └──▶ Floci: S3 scripts and replays, DynamoDB state, SQS jobs
```

- `crates/arena-engine`: Rust simulation at 20 Hz: world generation, rules,
  bots, checkpoints, and state hashes. The Go API talks to it over a private
  Unix socket.
- `cmd/server`, `internal/api`: HTTP API, viewer and agent WebSockets, match
  worker, replay recording. The server owns all state; agents return intent.
- `cmd/provisioner`: private Docker-socket service that creates and
  configures boxes.
- `cmd/box-supervisor`: runs inside each box, holds robot credentials outside
  `/workspace`, and restarts the agent when configuration changes.
- `sdk/lua/arena.lua`: Lua SDK bundled into every box.
- `apps/web`: Svelte 5 client. UI primitives live in
  `src/lib/components/ui` (shadcn-style, Tailwind tokens).
- `internal/cloud`: S3, DynamoDB, and SQS adapters behind one store
  interface, pointing at Floci by default.

More detail: [API](docs/api.md), [Arena V2 status](docs/arena-v2.md),
[VPS deployment](docs/vps-deployment.md). The Review 1 Go arena is hidden but
kept; see [classic arena](docs/legacy-arena.md).

## Configuration

All endpoints, credentials, ports, and limits come from environment variables (see `.env.example`):

| Variable | Purpose | Local default |
| --- | --- | --- |
| `PROVISIONER_TOKEN` | Shared secret between API and provisioner | `local-review-token-change-me` |
| `ROBOT_BOX_IMAGE` | Box image built by `mise run box:build` | `cloud-robot-box:local` |
| `SSH_PUBLIC_HOST` | Host shown to users for SSH | `localhost` |
| `SSH_PORT_MIN` / `SSH_PORT_COUNT` | Box SSH port range base and size | `22000` / `1000` |
| `SSH_PORT_START` / `SSH_PORT_END` | Optional explicit range overrides | unset |
| `EXPECTED_BOX_COUNT` | Minimum ports required at provisioner startup | `2` |
| `BOX_CPUS` / `BOX_MEMORY_MB` / `BOX_PIDS` | Docker limits per box | `1` / `512` / `128` |
| `BOX_STORAGE_BYTES` | Workspace quota monitored by the supervisor | `1073741824` (1 GiB) |
| `ROBOT_AGENT_BASE_URL` | WebSocket URL boxes use to reach the API | `ws://host.docker.internal:8080` |
| `AUTH_REQUIRED` | Reject anonymous API mutations | `true` |
| `WORKOS_CLIENT_ID` / `VITE_WORKOS_CLIENT_ID` | AuthKit client (public) | demo client ID |
| `WORKOS_API_KEY` | Reserved for future server-side WorkOS calls; not used by Phase 1 token validation | unset |
| `WORKOS_ISSUER` | Expected JWT issuer; when unset the API derives `https://api.workos.com/user_management/<client-id>`, which is what AuthKit browser tokens carry | derived |
| `AWS_ENDPOINT_URL` | Floci endpoint | `http://floci:4566` |
| `QUEUE_CONNECT_GRACE_SECONDS` | Agent connection deadline after pairing | `30` |
| `QUEUE_BOT_FILL_SECONDS` | Wait before filling a duel with a bot | `45` |
| `ADMIN_USER_IDS` | Comma-separated WorkOS IDs allowed on admin status | unset |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | Admin console login; empty password disables it | `admin` / `local-admin-change-me` |
| `ARENA_ENABLED` | Keep the endless arena running | `true` |
| `ARENA_ROBOTS` | Minimum arena population; bots fill up to it and step aside for players (2..256) | `128` |
| `ADMIN_SESSION_SECRET` | HMAC key for admin sessions (16+ chars); random per start when unset | unset |
| `MAINTENANCE_MODE` | Refuse new matches while current matches finish | `false` |

Never commit `WORKOS_API_KEY` or any robot token.

## Checks

```sh
mise run check          # svelte-check, cargo fmt + clippy, go vet
mise run test           # vitest, Rust rule and bot-behaviour tests, go test
mise run sdk:test       # Lua SDK helper tests under the box's Lua 5.4
mise run v4:smoke       # two real SSH boxes, Lua agents, Floci replay (stack running)
mise run v4:behaviour   # every shipped strategy moves, fights, and fires (stack running)
mise run v4:load        # synthetic WebSocket load; ARENA_LOAD_ROBOTS=256 for the cap
crates/arena-engine/target/release/arena-engine benchmark 256 1000 match
```

## Known limits

- Boxes are for trusted reviewers only. Hostile code needs Firecracker/KVM,
  quotas, egress policy, and adversarial testing.
- The Go job runner runs one match at a time; other lobbies queue.
- A running match fails if the API restarts. Checkpoints support replay and
  traces, not resumption.
- 256 networked players are not qualified. Local benchmarks and synthetic
  load samples pass, but the Linux reference-hardware run is still to do.
- The worker still serializes full map geometry every step. Replays store it
  once per page.
- Two authenticated browser sessions have not been verified end to end by
  automated tests; the reviewer demo covers it.
- Workspace quota is monitored, not enforced by the filesystem.
- Authentication needs real WorkOS JWKS access; there is no offline login.
