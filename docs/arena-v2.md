# Robot Arena v2 implementation status

## Run

```sh
mise install
mise run v4:build
mise run demo
```

Open `http://localhost:3000/#/v2`. For an all-bot practice match, sign in, select
Match, configure the roster, create the lobby, then start it. A public viewer
waits for 5 seconds of simulation before the first full-world snapshot.
Sandbox pause also pauses that delay.

For a programmable robot, provision the existing SSH box, open Code, deploy the
v4 example, then load, edit, check syntax, and save. Select a legal build and
register in the lobby. Registration stores an immutable main script and starts
that file; later workspace edits prepare the next run. Start after all human
agents connect. Use the private observation view for live feedback. Complete
public replays and private owner traces unlock after the match.

## Implemented

- Separate Rust worker per running v4 match, private mode-0600 Unix socket, bounded
  length-prefixed Protobuf envelope, 20 Hz simulation, 10 Hz server decisions.
  Go owns lifecycle, agent identity, queues, storage, and deployment. The current
  Go job runner processes one match at a time.
- Solo and four-member squads, sandbox pause/step, quick duel, 256-slot ceiling,
  bot fill, shared placement for same-tick deaths, deadline outcomes, withdrawal,
  reconnect expiry, no battle-royale revives or late registrations.
- Three chassis, nine weapons, six passive modules, four utilities, six
  consumables, 60-point builds, per-weapon heat/cooldown, acceleration, independent
  turret heading, collision-checked dash/projectiles, shields and status effects.
- Explicit equipment swaps and drops, persisted weapon heat/cooldown and utility
  charges in dropped gear, last-weapon protection, consumable stacking, pickup
  preferences without automatic replacement, interrupted healing channels.
- Deterministic sites, local cover, loot tables, transit links, default
  42,000×26,250 world, zone holds/shrinks, timed supplies. Small sandbox and duel
  presets use 2,400×1,500. Map scale multiplies dimensions, not obstacle sizes.
- Range/cover/smoke/cloak filtering, coarse local scans and detectable scan events,
  allied status, bounded timestamped reports, owner-only traces and source.
  Dead robots cannot sense enemies; eliminated squad viewers follow a survivor.
  Public match metadata redacts loadouts and workspace paths.
- Bounded agent mailboxes, explicit malformed/version/range/stale/duplicate
  rejection codes, continuous intent expiry after 250 ms, one-shot discrete
  actions. Storage writes use a bounded background queue; recording failure
  fails the match rather than silently losing replay inputs.
- Checkpoints every 200 ticks, accepted input pages, deterministic state hashes,
  completed replay frames at 2 Hz, private trace reconstruction in a fresh worker.
  Restore requires matching engine version and architecture.
- Revision-checked admin edits for geometry, hazards, loot, transit, and future
  zone phases. Edits are previewed, recorded before scheduling, revalidated at
  application, and atomically applied. Invalid edits produce an audit rejection.
- Buffered timestamp interpolation, heading wraparound, projectile interpolation,
  follow/pan/zoom/minimap, viewport culling, bounded 48-tile cache, loadout presets,
  virtualized roster, source editor, syntax check, replay timeline, owner observation/action inspection.

## Deliberate limits and unfinished work

The full requested plan is not complete. These are release blockers or follow-up
implementation work, not claims of delivered behavior:

- Qualify on Linux with 8 vCPUs and 16 GiB RAM, external WebSocket load generators,
  16/64/128/256 clients, dense final combat, ten-match memory tests, worker/storage
  fault injection, reference-browser p95 frame timing, and two-browser/two-SSH-box
  stop/start smoke. A 256-entry validator is not proof of network capacity.
- Regional transport load qualification, virtualized log rows, renderer quality controls, and
  measured packet-loss/jitter/hidden-tab recovery. Current spectators receive
  delayed regional snapshots; participant views use filtered observations.
- Protobuf currently frames versioned JSON payloads. Generated typed Go/Rust
  payload bindings, shared browser/Lua schema generation, remain to be built. V4 connections require the SDK 0.4.0 header and use the
  `robot-arena.v4` WebSocket subprotocol.
- Prove spawn concealment, balanced reachable starting loot, and early-zone travel
  connectivity over a fixed seed corpus. Current generation checks collision
  clearance; it does not prove those stronger guarantees. Transit is a site ring;
  the Lua navigation helper has bounded search, not a complete region planner.
- Balance and finish weapon-specific details, including weapon balance. Expand
  resource-exhaustion tests and enforce the per-chunk container budget uniformly
  for every runtime salvage/supply path. Live-edit reachability validation is a
  bounded local escape check, not a proof of all region connectivity.
- Complete six distinct strategy examples, squad role coordination, browser SDK
  completion, side-by-side build comparison, fixed-seed script A/B tests, visual
  edit previews/undo history, configuration catalogue revisions, achievements,
  cosmetics, and detailed elimination explanations.
- Browser saves detect changes since loading. Arbitrary SSH writes do not share
  the supervisor lock, so simultaneous SSH writes cannot provide strict atomic
  compare-and-swap. Keep SSH edits and browser saves sequential for now.
- Running workers fail on API restart. Checkpoint reconstruction supports replay,
  not automatic resumption. Multi-worker admission/recovery remains unfinished.

## Verification record

On 2026-09-14, the Rust rule tests, Go integration tests, Svelte type checks,
frontend tests, and frontend production build were run locally. The Go/Rust
integration uses real private sockets and persists a short completed match.
Tests cover cross-process checkpoint restoration and public/private boundaries.
The Go API and worker integration also passed the race detector. Rust has 28
rule tests; the frontend has six tests. Svelte reports zero errors and warnings.
The local Lua 5.5.1 parser accepted the SDK and example; this is not a substitute
for the pending Lua 5.4 box smoke test.

Release-build samples on Darwin arm64, Apple M4, 10 logical CPUs, 24 GiB RAM:

- Dense: 256 robots kept alive for 1,000 ticks; simulation p99 5.253 ms,
  simulation plus snapshot/hash pipeline p99 7.735 ms; 20 peak projectiles.
- Match: 256 starting game bots, one survivor at tick 20,055 (1,002.75 seconds of
  simulated match time); simulation p99 3.621 ms, pipeline p99 11.121 ms;
  672 peak projectiles.

These are accelerated simulation samples, not real-time networking or Linux
qualification. The dense case does not saturate the 8,192-projectile cap. The host differs from the Linux reference machine, so these measurements
are preliminary and cannot establish the reference-host targets.

On 2026-09-15, the two-box smoke passed with real Docker SSH workspaces,
Lua 5.4 agents, Go/Rust simulation and Floci persistence. Both workspaces survived
box restart; both scripts fired; immutable scripts, accepted actions and replay
frames were read back. Account sign-in is replaced only inside the isolated test
server. The rebuilt Compose stack also passed stop/start: API readiness and the web
response were checked separately, and stored match metadata, both immutable
scripts, accepted actions and replay frames survived in Floci. Two authenticated
browser sessions remain unverified.

Replay objects now use a separate 16 MiB storage bound, preserving the Lua source
read limit. Regression coverage rejects oversized replay reads and writes.
Agents acknowledge public geometry revisions to avoid repeated geometry delivery.
Cannon knockback now checks cover and robot collisions.

Short local synthetic WebSocket samples passed at 16, 64, 128 and 256 agents.
The latest 256-agent sample simulated ten seconds in 10.744 seconds and delivered
25,772 observations totaling 192,134,930 bytes. This is not the 18-minute Linux
qualification, and the bandwidth still needs regional transport work.

Run `mise run v4:smoke` with Compose running to repeat the two-box check.
It builds the worker and box image, creates disposable test boxes, and removes
only those boxes and their volumes. Floci smoke records remain for inspection.

Reproduce fresh measurements after changes:

```sh
mise run v4:build
crates/arena-engine/target/release/arena-engine benchmark 256 1000 dense
crates/arena-engine/target/release/arena-engine benchmark 256 21600 match
```

The v4 browser now sends camera regions. Public viewers receive regional entity
replacement snapshots at 10 Hz, a full roster/minimap overview at 1 Hz, and public
geometry on first delivery or revision changes. Projection runs in each viewer
writer after the delayed hub. This reduces network payloads but still parses a
full snapshot per viewer; viewer-count CPU profiling remains necessary.

## Recovery and navigation update

Mode selection now defaults quick duels to two robots and three minutes, sandbox
to eight robots and three minutes with live edits, and battle royale to 64 robots
and 18 minutes. These values remain editable before creating the lobby.
Consumable and equipment drops share the per-chunk container admission check.
A rejected consumable drop preserves inventory and reports
`CONTAINER_BUDGET_EXCEEDED`. Salvage and supply admission still need completion.

The viewer retries dropped connections with a 500 ms to 10 s exponential backoff,
cancels retries on unmount, and requests a fresh baseline when a hidden tab
becomes visible. Reconnects clear geometry and interpolation state. Playback
regressions cover uneven delivery, dropped/duplicate snapshots and a long tab gap.
The renderer displays frame interval p95 over its last 300 active frames. This
instrumentation is not a measured browser-frame qualification result.

A repeatable synthetic 10-second spread-world payload test with 256 robots and
1,024 containers measured 4,940,245 bytes before projection and 164,350 bytes after,
a 96.67% reduction. It excludes WebSocket framing, TLS, compression and live
network overhead. Dense combat and large camera regions will save less.
Run `mise exec -- go test ./internal/api -run TestV4RegionalPayloadMeasurement -v`.

Server bots now search for collision-clear local detours with a 128-expansion
budget, turn before accelerating, and stop with `PATH_BLOCKED` when the local
search finds no route. Cruise requires an aligned, clear long segment. Tests
cover deterministic detours and a sealed route. This is not a complete region
planner; difficult routes still need higher-level replanning.

On the same local Apple M4 host, the updated 256-robot dense sample over 1,000
ticks measured simulation p99 3.884 ms and pipeline p99 5.734 ms, with 20 peak
projectiles. This remains a preliminary accelerated sample, not Linux capacity
qualification or evidence that every navigation scenario meets the tick budget.
The full Go suite, 29 Rust rule tests, Rust Clippy, eight frontend tests, Svelte
checks and frontend production build passed after these changes.

## Dense district maps (2026-09-26)

Each site now has a biome (urban, industrial, forest, desert). Themed structures
fill each site's cell outside its core rings and street cross: rooms with
doorways, container yards, groves, rock formations, and sandbag outposts. The
default 42,000×26,250 world has about 5,900 obstacles, up from 832. District
placement uses its own RNG stream, so core rings, scatter, and hazards stay
identical for a seed. Tests check clearance between structures, hazard
avoidance, room exits, and a pinned layout hash.

The browser paints textured ground, roads between grid-neighbour sites, site
plazas, hazards, and material sprites. The sprites come from a CC0 Kenney atlas
built by `scripts/build-terrain-atlas.sh`. Chunks are cached at two
resolutions, and new chunk paints are capped at six per frame. In a local
Chrome check, a 24-robot match rendered at 120 fps with a 9.3 ms frame p95.

To keep tick cost flat, each step now reindexes only robot positions. Static
geometry is reindexed on edits and restores. Grid queries use a hash map with
sorted dedupe. On the Apple M4 host, the 256-robot `match` benchmark (200
ticks) measured simulation p99 20.7 ms on the dense map. The previous sparse
map measured 18–28 ms. The 64-robot sample measured 7.8 ms. State hashes
matched before and after the grid change. Every step still serializes full
geometry in the snapshot. Replay pages now store layout once per page, so dense
maps no longer multiply replay size.
