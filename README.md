# Cloud Robot Arena

Multiplayer robot arena prototype for Review 1. Players sign in with WorkOS,
receive one persistent Docker SSH development box, and deploy a Lua robot over
SSH. The box agent connects to the authoritative Go server over one outbound
WebSocket. Public viewers watch synchronized 10 Hz matches. Floci provides local
S3, DynamoDB, and SQS-compatible APIs.

This is a trusted-reviewer prototype: Docker boxes are not a security boundary for hostile code. Firecracker isolation is a later phase.

## Verified behavior

- WorkOS AuthKit sign-in gates box operations, match creation, robot registration, and match start. Match viewing stays public.
- `POST /api/me/box` provisions one box per user, named by a SHA-256 hash of the user ID, with a persistent `/workspace` volume.
- SSH public keys are validated (single line, ≤ 8 KiB, OpenSSH format) and replaceable; only the fingerprint is stored.
- Boxes run with 1 CPU, 512 MB RAM, and 128 PIDs. The supervisor monitors workspace usage and stops the agent above 1 GB while SSH stays available.
- Every box boots with a working demo robot in `/workspace/main.lua`. The box console (MY BOX) shows the file and can deploy curated variants (aggressive, evasive, sniper, patroller) with one click; SSH editing keeps working.
- Robot registration snapshots `/workspace/main.lua` to S3, stores the SHA-256-hashed robot token in DynamoDB, configures the box supervisor, and starts the agent. The token never reaches the browser.
- One active robot per box and one robot per user per match; a box is reusable after its match finishes or fails. When a lingering binding blocks queueing or registration with "box already has an active robot", the fault banner and the Play page offer EXIT ACTIVE MATCH (`POST /api/me/box/release`): lobby registrations are dropped, running matches are conceded, and stale bindings are cleared.
- Matches enqueue through SQS, run on the server-authoritative engine, and publish versioned snapshots to browsers. When both teams are registered and every agent is connected, the match queues itself; the owner's START NOW button remains as a manual fallback.
- A player can withdraw mid-match (`POST /api/matches/{matchID}/withdraw`, WITHDRAW FROM MATCH button in the match view and on the box console). The robot is destroyed at the next tick without crediting a kill, and the match resolves by normal elimination rules. The box console button also releases a box bound to a match that never left the lobby.
- Matches can use five starter maps, custom arena dimensions, static obstacles, hazards, item spawns, server bots with selectable personalities, weapon rules, optional friendly fire, regen, and ramming, zone collapse, and overtime.
- Healing, shields, overdrive, rapid fire, repair cores, weapon drops, and randomized spawner loot (including rare weapons and occasional supply-drop bursts) use deterministic server-side spawning and pickup rules; weapon hits can crit for 1.5x damage (10% by default, per-match config).
- Dummy, Rookie, Fighter, and Sharpshooter bots run inside the server. Practice matches can start without a second SSH box.
- Three lobby modes exist besides the duel queue. `squad` opens with ten bots, five per side, on the Corridors map; registering a robot on a team replaces one of that team's bots, so each side always fields five robots. `solo` is a free-for-all where the player plus 0–7 bots each fight on their own team; with zero bots it is an empty sandbox that runs until the tick limit or the robot dies. Squad and solo matches are unranked practice results. Squad auto-start waits until at least one human per side has joined (the owner can force-start earlier); solo auto-starts when the player's agent connects.
- The duel queue pairs two players, adds a bot after a configurable wait, cancels matches whose agents miss the connection grace window, and automatically requeues players from cancelled queue matches.
- Public routes cover Home, Match, Match history, Profile, Leaderboard, Spectate, SDK docs, and API docs. Authenticated routes cover Play, My Box, Settings, Create, and Admin.
- Finished matches store event replays and per-robot stats. Player wins, losses, damage, and duel ratings update after non-practice results.
- Viewer reconnects receive current state. Robot agents can reconnect within 30 seconds while the engine reuses their last intent.
- Each script deploy and registration creates a versioned S3 snapshot. The API can list and restore versions.
- Stop/start of the stack keeps box volumes; boxes are rediscovered on the next sign-in.

## Setup

Requirements: Docker with Compose, mise, and a WorkOS account client ID (public value only).

```sh
mise install            # pins Go 1.26.7, Node 26.8.1, pnpm 11.24.0
mise run install        # frontend dependencies
cp .env.example .env    # optional; compose defaults already match
mise run demo           # build box image, build stack, start everything
```

WorkOS dashboard prerequisites (required before sign-in works; the demo client ID is public):

1. AuthKit enabled on the WorkOS project.
2. **Redirects** page: add `http://localhost:3000` as a redirect URI (and `http://localhost:5173` when using the Vite dev server).
3. On the Authentication, Sessions, Cross-Origin Resource Sharing page, add `http://localhost:3000` as an allowed origin. Without it, WorkOS omits `Access-Control-Allow-Origin` during the browser token exchange. Sign-in then fails with a CORS error, and the UI shows the required setting.

Services: web on `http://localhost:3000`, API on `:8080`, Floci on `:4566`, provisioner on `:8090` (internal only). Box SSH ports start at `22000` and bind to `127.0.0.1`.

Floci runs with `FLOCI_STORAGE_MODE=hybrid`, so matches, scripts, and results survive stack restarts through the `floci-data` volume. On API startup, matches left `running` by a restart are failed and `queued` matches are re-enqueued, so no box stays blocked as "already in a match".

Stop and restart without losing workspaces:

```sh
mise run demo:stop      # stops compose stack and robot boxes, keeps volumes
mise run demo:start     # restarts; boxes rediscovered on next sign-in
```

## Review 1 demo

1. `mise run demo`, then open `http://localhost:3000` in two browsers and sign in with two different WorkOS accounts.
2. Each browser: the box card appears once `POST /api/me/box` provisions the box. Paste one SSH public key per account and press ADD KEY.
3. Edit the robot in My Box by deploying a built-in variant. You can also run `ssh -p <port> developer@localhost` and edit `/workspace/main.lua`. `examples/lua-aggressive` and `examples/lua-evasive` contain ready robots. The box page previews the file before registration snapshots it.
4. Create a match in one browser, then either share the copied link or paste the match ID into JOIN MATCH BY CODE in the second browser, and register one RED and one BLUE robot. The agent card appears automatically; the robot token is never shown.
5. Once both roster cards show CONNECTED the match queues itself and both browsers receive the same 10 Hz snapshots until a winner is stored. START NOW remains as a manual fallback.
6. Check Floci (`http://localhost:4566`) for the script snapshot, match record, and result; box metadata persists in DynamoDB.
7. `mise run demo:stop`, then `mise run demo:start` and sign in again: both workspaces and box assignments survive.

## Architecture

```text
Svelte 5 browser ──HTTP/WS──▶ Go API ──▶ provisioner ──▶ Docker socket ──▶ robot boxes
                                   │                                      (SSH :22000+, Lua agent)
                                   ├──▶ Floci (S3 scripts, DynamoDB state, SQS match jobs)
```

- `cmd/server` + `internal/api`: HTTP API, WebSocket hub, agent gateway, match worker. The server owns all simulation state; agents only return intent.
- `cmd/provisioner`: private service with Docker socket access. Creates, starts, and reconfigures boxes. Never exposed publicly.
- `cmd/box-supervisor`: root-owned inside each box. Holds robot credentials in `/var/lib/robot-box` (outside `/workspace`), runs the saved start command as `developer`, restarts it when the match configuration changes, and enforces the workspace quota.
- `internal/engine`: maps, obstacles, items, weapons, bots, zones, overtime, events, and deterministic 10 Hz simulation.
- `sdk/lua/arena.lua`: Lua 5.4 SDK with reconnect, item filtering, line-of-sight, and waypoint helpers.
- `internal/cloud`: S3/DynamoDB/SQS adapters behind one store interface, pointing at Floci by default.

See [API reference](docs/api.md) and [Lua SDK](docs/lua-sdk.md) for protocol details.

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
| `MAINTENANCE_MODE` | Refuse new matches while current matches finish | `false` |

Never commit `WORKOS_API_KEY` or any robot token.

## Checks

```sh
mise run check          # svelte-check + go vet
mise run test           # vitest + go test
mise run build          # frontend + server binaries
./scripts/box-integration.sh <box-id> <public-key-file>   # Docker-level box checks
mise run vps:doctor -- user@host [port]                   # read-only VPS inspection
```

## Known limits

- Boxes are for trusted reviewers only; hostile code needs Firecracker/KVM plus quotas, egress policy, and adversarial testing.
- Live matches and viewer fan-out live in one API process; a restart ends active matches.
- Workspace quota is monitored, not a hard filesystem limit. VPS deployment needs Linux project quotas or `storage-opt` (see `docs/vps-deployment.md`).
- Queue state is in memory. API restart loses waiting entries. Queue supports duel mode only; team queues and rating-window pairing remain planned.
- Replays store engine events. They do not yet re-simulate saved inputs.
- Map definitions ship with the server. S3 map upload and review are not implemented.
- Authentication requires real WorkOS JWKS access; there is no offline local auth.
