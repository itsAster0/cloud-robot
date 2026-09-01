# Phase 1 plan

## Goal

Demo a continuous multiplayer robot arena for Review 1. A player gets an SSH-enabled Docker box, deploys a Lua agent, selects its start command, and keeps an outbound WebSocket connected. The Go server owns the simulation. The Svelte 5 site shows combat, connection health, response latency, runtime telemetry, equipment, projectiles, developer docs, and WorkOS login.

## Fixed decisions

| Area | Phase 1 choice |
| --- | --- |
| Web | Svelte 5, TypeScript, Vite, pnpm |
| Server | Go HTTP API, WebSocket gateway, authoritative 10 Hz engine |
| Agent | Persistent external process; Lua 5.4 SDK supplied |
| Developer access | Key-only SSH into one Docker box per robot |
| Local cloud | Floci with SQS-compatible match jobs and DynamoDB-compatible state |
| Auth | WorkOS AuthKit PKCE in browser; RS256 access-token validation in API |
| Toolchain | mise-pinned Go, Node, and pnpm |
| Deployment | Docker Compose now; one cloud VPS later |

S3 remains reserved for replay and artifact storage. Robot source is not uploaded through the website. Floci is an AWS API emulator, not evidence of AWS scale or production security.

## Data flow

```text
Svelte browser --HTTP/WS--> Go API + arena worker --SQS/DynamoDB--> Floci
                              ^
                              |
                    authenticated WebSocket
                              |
                 SSH development box + Lua SDK
```

1. Owner signs in or uses guest mode locally and creates a match.
2. Players register red and blue boxes. The API returns each robot token once and stores only its SHA-256 hash.
3. Players SSH into the boxes, deploy code, and start their declared command.
4. SDK opens an outbound WebSocket and reconnects after network loss.
5. Server sends an observation every tick. Agent returns movement, aim, fire intent, logs, equipment, and optional runtime telemetry within 150 ms.
6. Owner starts once both teams exist and every agent is connected.
7. SQS starts the match worker. Server resolves movement, collision, projectiles, damage, death, and result.
8. Viewers receive versioned snapshots. DynamoDB stores match state and summaries.

## Server-owned rules

- Arena: 800 × 500 units.
- Tick rate: 10 Hz.
- Movement: at most 8 units per tick; reverse at half speed.
- Rotation: at most 18 degrees per tick.
- Weapon: visible plasma projectile, 25 damage, 8-tick cooldown.
- Robot: 100 HP. Friendly fire is disabled.
- Agent deadline: 150 ms. A missed deadline reuses the last action and records the miss.
- Agent disconnect during a match fails that robot without stopping the worker.
- Match ends when one team remains or the tick limit is reached.

Agents never set coordinates, HP, damage, winner, or another robot's state.

## API and protocol

HTTP endpoints:

- `POST /api/matches`
- `GET /api/matches/{matchId}`
- `POST /api/matches/{matchId}/robots`
- `POST /api/matches/{matchId}/start`
- `GET /api/cloud/status`
- `GET /healthz`
- `GET /readyz`

WebSockets:

- `/ws/matches/{matchId}` streams viewer snapshots and agent connection state.
- `/agent/connect/{robotId}` accepts the `robot-arena.v1` protocol and a robot bearer token.

See `docs/lua-sdk.md` for message schema and Lua helpers.

## Review 1 run

1. Start base stack with `mise run demo`.
2. Open two browser sessions at `http://localhost:3000` and sign in with two WorkOS accounts.
3. Each account provisions its persistent box through the UI and adds one SSH public key.
4. SSH into each box, edit `/workspace/main.lua` with the Lua SDK, and keep the default `lua main.lua` command.
5. Create one match, share the link, and register one red and one blue robot. Registration auto-configures and starts each box agent; no token is returned to the browser.
6. When both roster cards show CONNECTED the match auto-starts; the owner's START NOW button is a fallback.
7. Show movement, visible projectiles, HP, latency, compute, memory, equipment, and winner in both browsers.
8. Verify script snapshots, match records, and results in Floci, then stop and restart the stack with `mise run demo:stop` / `mise run demo:start` and confirm workspaces survive.

## Acceptance checks

- Clean clone starts with documented commands.
- Svelte type check, unit tests, Go tests, and Docker configuration pass.
- Floci readiness succeeds before a match is created.
- Two persistent agents connect, fight, and produce a stored result.
- Late browser viewers receive current agent connection state.
- Robot tokens are hashed in DynamoDB and never returned to browsers or logs.
- `mise run demo:stop` / `demo:start` keeps workspaces and rediscovers boxes.

## Known limits

- Docker boxes are safe only for trusted Review 1 users. Public untrusted code requires Firecracker/KVM or equivalent hardened isolation, quotas, network egress policy, read-only images, credential rotation, cleanup, and adversarial tests.
- Live sessions and viewer fan-out are in process. API restart ends active matches.
- One API process consumes match jobs. Horizontal coordination is deferred.
- Auth defaults to optional locally. VPS deployment must set `AUTH_REQUIRED=true`, HTTPS/WSS, and a configured WorkOS redirect URI.
- No reconnect resume after a robot's running match has already failed it.

## Next phase

1. Provision VPS, DNS, TLS, firewall, backups, and monitoring.
2. Verify KVM before choosing Firecracker.
3. Add hardened per-user isolation and resource accounting.
4. Persist replay events to S3 and add playback.
5. Add invitations, spectator access rules, ranking, and multi-match scheduling.
