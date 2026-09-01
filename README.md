# Cloud Robot Arena

Multiplayer robot arena prototype for Review 1. Players sign in with WorkOS, receive one persistent Docker SSH development box, deploy a Lua robot over SSH, and the box agent connects to the authoritative Go server over one outbound WebSocket. Browsers watch a synchronized 10 Hz match simulation. All cloud storage runs against Floci, a local AWS-compatible emulator.

This is a trusted-reviewer prototype: Docker boxes are not a security boundary for hostile code. Firecracker isolation is a later phase.

## Verified behavior

- WorkOS AuthKit sign-in gates box operations, match creation, robot registration, and match start. Match viewing stays public.
- `POST /api/me/box` provisions one box per user, named by a SHA-256 hash of the user ID, with a persistent `/workspace` volume.
- SSH public keys are validated (single line, ≤ 8 KiB, OpenSSH format) and replaceable; only the fingerprint is stored.
- Boxes run with 1 CPU, 512 MB RAM, and 128 PIDs. The supervisor monitors workspace usage and stops the agent above 1 GB while SSH stays available.
- Every box boots with a working demo robot in `/workspace/main.lua`. The box console (MY BOX) shows the file and can deploy curated variants (aggressive, evasive, sniper, patroller) with one click; SSH editing keeps working.
- Robot registration snapshots `/workspace/main.lua` to S3, stores the SHA-256-hashed robot token in DynamoDB, configures the box supervisor, and starts the agent. The token never reaches the browser.
- One active robot per box and one robot per user per match; a box is reusable after its match finishes or fails.
- Matches enqueue through SQS, run on the server-authoritative engine, and publish versioned snapshots to browsers. When both teams are registered and every agent is connected, the match queues itself; the owner's START NOW button remains as a manual fallback.
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
3. **Authentication → Sessions → Cross-Origin Resource Sharing (CORS)**: add `http://localhost:3000` as an allowed origin. Without it, WorkOS omits `Access-Control-Allow-Origin` on the browser token exchange and sign-in fails with a CORS error — the UI shows this hint when it happens.

Services: web on `http://localhost:3000`, API on `:8080`, Floci on `:4566`, provisioner on `:8090` (internal only). Box SSH ports start at `22000` and bind to `127.0.0.1`.

Stop and restart without losing workspaces:

```sh
mise run demo:stop      # stops compose stack and robot boxes, keeps volumes
mise run demo:start     # restarts; boxes rediscovered on next sign-in
```

## Review 1 demo

1. `mise run demo`, then open `http://localhost:3000` in two browsers and sign in with two different WorkOS accounts.
2. Each browser: the box card appears once `POST /api/me/box` provisions the box. Paste one SSH public key per account and press ADD KEY.
3. Edit the robot: open MY BOX → ROBOT SCRIPTS and press DEPLOY on a variant, or `ssh -p <port> developer@localhost` and edit `/workspace/main.lua` by hand (`examples/lua-aggressive` and `lua-evasive` are ready robots). The /WORKSPACE/MAIN.LUA card previews what the server snapshots.
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
- `internal/engine`: deterministic 10 Hz simulation (movement, collision, projectiles, damage, winner).
- `sdk/lua/arena.lua`: Lua 5.4 SDK with reconnecting WebSocket loop.
- `internal/cloud`: S3/DynamoDB/SQS adapters behind one store interface, pointing at Floci by default.

## Configuration

All endpoints, credentials, ports, and limits come from environment variables (see `.env.example`):

| Variable | Purpose | Local default |
| --- | --- | --- |
| `PROVISIONER_TOKEN` | Shared secret between API and provisioner | `local-review-token-change-me` |
| `ROBOT_BOX_IMAGE` | Box image built by `mise run box:build` | `cloud-robot-box:local` |
| `SSH_PUBLIC_HOST` | Host shown to users for SSH | `localhost` |
| `SSH_PORT_START` / `SSH_PORT_END` | Box SSH port range | `22000`–`22999` |
| `BOX_CPUS` / `BOX_MEMORY_MB` / `BOX_PIDS` | Docker limits per box | `1` / `512` / `128` |
| `BOX_STORAGE_BYTES` | Workspace quota monitored by the supervisor | `1073741824` (1 GiB) |
| `ROBOT_AGENT_BASE_URL` | WebSocket URL boxes use to reach the API | `ws://host.docker.internal:8080` |
| `AUTH_REQUIRED` | Reject anonymous API mutations | `true` |
| `WORKOS_CLIENT_ID` / `VITE_WORKOS_CLIENT_ID` | AuthKit client (public) | demo client ID |
| `WORKOS_API_KEY` | Reserved for future server-side WorkOS calls; not used by Phase 1 token validation | unset |
| `WORKOS_ISSUER` | Expected JWT issuer; when unset the API derives `https://api.workos.com/user_management/<client-id>`, which is what AuthKit browser tokens carry | derived |
| `AWS_ENDPOINT_URL` | Floci endpoint | `http://floci:4566` |

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
- A robot whose agent disconnects mid-match is failed; there is no reconnect resume into a running match.
- Authentication requires real WorkOS JWKS access; there is no offline local auth.
