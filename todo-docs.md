# Feature roadmap / TODO

Goal: turn the Review 1 prototype into a real programmable-robot platform. This doc lists every feature worth building, grouped by system, with the pages each system lives on. Nothing here is implemented yet unless marked done.

## Current state (baseline)

- Single page (`App.svelte`): arena view, box console, match creation, SDK docs all live together.
- Arena: flat 800 × 500 rectangle, no obstacles, no items, no hazards (`internal/engine/engine.go`).
- Combat: one weapon (plasma, 25 dmg, 8-tick cooldown), 100 HP, friendly fire off.
- Matches: manual creation + join by code, exactly 1v1 red vs blue, no queue, no bots.
- Persistence: Floci (S3/DynamoDB/SQS) for snapshots, match records, results.
- No profiles, no stats, no leaderboards, no history, no replays, no social features.

Priority labels: **P0** = next demo, **P1** = makes it feel like a real game, **P2** = platform polish.

## Access model: watching is always free

Anyone can open the website and watch without signing in. Sign-in is only required for actions that own resources.

- [ ] **P0** Public (no login): home page, live match view, match list, match detail/results, leaderboards, profiles, SDK docs, spectate page.
- [ ] **P0** Login required (matches today's behavior): box provisioning, SSH keys, queueing/playing, match creation, settings.
- [ ] **P1** Viewer WebSocket for public pages works anonymously; the API must not gate `/ws/matches/{id}` behind auth (viewer fan-out already public — keep it that way as pages split out of `App.svelte`).
- [ ] **P1** "Sign in to play" CTAs on public pages: watching a match shows JOIN QUEUE / SIGN IN buttons only if the viewer is authenticated; otherwise a sign-in prompt.
- [ ] **P2** Guest identity for social features: anonymous viewers get a read-only name like `Guest-1234`; posting chat or anything that mutates requires sign-in.
- [ ] **P2** Share preview: match links render a title card (map, robots, status) even when opened logged-out.

---

## 1. Maps & arenas

Every match should pick a map, not reuse one flat rectangle.

- [ ] **P0** Map data model: JSON map definition (name, size, walls, obstacles, spawn points, item spawn zones) stored in S3 and referenced by match config.
- [ ] **P0** Engine support for static obstacles: circle/AABB collision, projectile blocking, line-of-sight checks.
- [ ] **P1** 3–5 starter maps:
  - Open Field (current rectangle, kept as the training map).
  - Four Corners (center cover, corner spawns).
  - The Corridors (walls forming lanes; rewards pathing).
  - Pillars (symmetric pillar field).
  - Crater (danger zones on the edges).
- [ ] **P1** Spawn point system: per-map spawn sets, random selection within a team's side, no-overlap guarantee, facing the arena center.
- [ ] **P1** Arena variants beyond size: different dimensions per map (small duel arena vs large team arena).
- [ ] **P0** Match-creatable arena size: when creating a match, pick boundary size — Small (600 × 400), Medium (800 × 500, current), Large (1200 × 800), or custom width/height with sane min/max clamps. Engine constants (`ArenaWidth`/`ArenaHeight`) become per-match config carried in the match snapshot, same as map ID; bots and item spawn zones scale to it.
- [ ] **P2** Visual map themes (background color/texture only; engine stays 2D geometry).
- [ ] **P2** Map hazards: damage zones, low-friction patches, one-way gates.
- [ ] **P2** Map editor page (draw walls/spawns, export JSON, submit for review).
- [ ] **P2** Map pick/ban or veto in match creation.

Engine note: map ID must be part of the match config snapshot so all clients and the worker render/simulate the same layout deterministically.

## 2. Items & pickups

Healing and power-ups that spawn during a match. Server-authoritative, like everything else.

- [ ] **P0** Item entity in engine: type, position, spawn tick, active/inactive, pickup radius.
- [ ] **P0** Item spawner: per-map spawn zones, spawn interval (e.g., every 100–200 ticks), max concurrent items, respawn delay after pickup.
- [ ] **P0** Healing pack: +25 HP (capped at max HP), disappears on pickup, 10–15 s respawn.
- [ ] **P1** Shield: temporary 50-point overshield that decays over 20 s.
- [ ] **P1** Overdrive: +50 % move speed for 8 s.
- [ ] **P1** Rapid fire: cooldown halved for 6 s.
- [ ] **P2** Weapon variants as pickups (see §3).
- [ ] **P2** Ammo/energy economy: firing costs energy; energy cells as items; regen when idle.
- [ ] **P2** Negative items / traps (decoy health pack) for P2 maps.
- [ ] **P1** SDK additions: observation includes visible items (position, type, time-to-spawn estimate). Pickup is automatic on contact — decided, no `pick_up` intent exists. Robots configure pickup behavior in their Lua code via an arena config knob (e.g., `arena.configure({ auto_pickup = true, pickup_types = { "heal", "repair-core" } })`); default picks up everything, filtering lets a robot ignore drops or only take specific types. Documented in `/docs/sdk` when implemented.
- [ ] **P1** Client rendering: item glyphs on the arena canvas, pickup flash, effect timers on HUD.
- [ ] **P1** Balance config: item rates/effects in a single tunable config file so tests and demos can tweak them.

Determinism note: item spawn RNG must be seeded from match ID so replays and all viewers agree.

### Item drops

Where items come from, beyond the fixed spawner.

- [ ] **P1** Drop on death: a destroyed robot drops a repair core (+15 HP) at its death spot; killer-adjacent robots fight over it. Classic comeback mechanic.
- [ ] **P1** Weapon drops on death: the destroyed robot's equipped pickup (if any) falls to the ground and is lootable.
- [ ] **P2** Supply airdrop: once or twice per match, a marker appears with a 5 s countdown, then lands a high-tier item (railgun, full shield). Announced to all robots in the observation — creates a contested moment.
- [ ] **P2** Rarity tiers: common (heal, ammo) / rare (shield, overdrive) / epic (railgun, airdrop-only). Drop tables per map, weight-configurable.
- [ ] **P2** Crate blockers: crates with HP that must be shot open, dropping 2–3 items.
- [ ] **P1** Drop physics note: engine stays grid-simple — drops land at death/landing coordinates clamped away from walls, no bounce simulation.
- [ ] **P1** Drops in observations: dropped items use the same item schema as spawner items so SDK/clients need one code path.

## 3. Combat depth

- [ ] **P1** Weapon set: plasma (current), cannon (slow, 60 dmg, knockback), machine gun (fast, 8 dmg), railgun (hitscan after charge-up). Chosen in loadout or picked up on map.
- [ ] **P1** Per-weapon properties table: damage, cooldown, projectile speed, range, spread, knockback.
- [ ] **P2** Armor pieces: trade speed for damage reduction.
- [ ] **P2** Status effects: burn (DoT), slow, EMP (brief weapon lock).
- [ ] **P2** Equipment slots (loadout): primary weapon, utility, passive — configured per robot before match start.
- [ ] **P2** Friendly fire toggle per match (currently hardcoded off).
- [ ] **P1** HP model: expose regen config (none vs slow out-of-combat regen).
- [ ] **P2** Ramming damage: collision with an enemy robot deals contact damage scaled by relative speed, with a short cooldown per pair so robots can't grind damage by pushing.

## 4. Bots

Server-side AI robots so a single player can demo, test scripts, and fill rosters. Bots run the same intent protocol as real agents — the server generates their intents internally, so they need no SSH box and no token.

- [ ] **P0** Bot controller implementing the engine `Controller` interface (engine-side only; indistinguishable from an agent in the simulation loop).
- [ ] **P0** Difficulty levels:
  - *Dummy*: stands still or moves in a fixed pattern (scripted target practice).
  - *Rookie*: random walk + fire when enemy in line of sight.
  - *Fighter*: seek enemy, aim with lead prediction, retreat when low HP, seek health packs.
  - *Sharpshooter*: adds strafing, cover usage, item denial (takes items before the player can).
- [ ] **P1** Bot personalities: aggressive / evasive / camper presets that tune the same controller knobs.
- [ ] **P1** Bots use maps properly: obstacle-aware movement (simple A* or potential fields on the grid; no physics-heavy pathfinding).
- [ ] **P1** Add bots in match creation: fill RED/BLUE roster with N bots before or after humans register.
- [ ] **P1** Solo practice mode: player + N bots, no result recorded (or recorded as `practice`).
- [ ] **P2** Bots as matchmaking fillers (see §5): start a match when queue is short on players.
- [ ] **P2** Bot skins/names so viewers can tell them apart in roster and leaderboard.
- [ ] **P2** Bot-vs-bot exhibition mode for the landing page.

## 5. Matchmaking

Replace "create match + share code" with a queue that works for humans and bots.

- [ ] **P0** Match queue API: `POST /api/queue` (join with robot), `DELETE /api/queue` (leave), queue status in UI.
- [ ] **P0** Simple pairing: first two queued players form a match; configurable mode selects roster size.
- [ ] **P0** Auto-start: match created, both robots registered, agents must connect within a grace window (30 s) or the match is cancelled and players requeued.
- [ ] **P1** Engine support for N robots per team: roster arrays end-to-end (snapshot, events, HUD, results). Prerequisite for the team sizes below; today's engine is 1v1-shaped.
- [ ] **P1** Team support: queue for 1v1, 2v2, 3v3; queue fills teams fairly (alternate team assignment by queue order).
- [ ] **P1** Skill rating: Glicko-2 (or simple ELO) stored per player per mode; updated on result. Rating shown on profile.
- [ ] **P1** Rating-based pairing: within ±150 rating window, widen the window the longer a player waits.
- [ ] **P1** Ranked vs casual queues: ranked updates rating; casual doesn't.
- [ ] **P1** Bot fill: after configurable wait (e.g., 45 s), fill empty roster slots with bots and start.
- [ ] **P1** Queue cancellation and timeout handling (agent never connects, duplicate queue attempts, disconnect during countdown).
- [ ] **P2** Parties: group with a friend, queue together, placed on the same team.
- [ ] **P2** Seasons: rating soft resets, season badges, archived season leaderboards.
- [ ] **P2** Placement matches: rating unprovisioned until 5 ranked games.
- [ ] **P2** Matchmaking telemetry: queue wait times, pairing deltas (admin page).
- [ ] **P2** Alternative win conditions: FFA (everyone vs everyone) and King of the Hill (hold the center zone for N ticks). Each is a mode flag with its own winner logic in the engine, not a special case in the queue.
- [ ] **P2** Private matches: optional join password or unlisted flag so shared-code matches can be gated.

Implementation note: queue state belongs in DynamoDB (atomic updates) or in-memory single-process to start; the SQS match-job path is already the right handoff to the worker.

## 6. Multi-page UI (information architecture)

Split `App.svelte` into real routes, one system per page. Svelte 5 + a lightweight router (hash routing is fine for the prototype; no SSR needed).

- [ ] **P0** Router scaffold + shared layout (nav bar, auth state, connection status).
- [ ] **P0** Public pages (no login, see access model): Home, Match, Match history/detail, Profile, Leaderboard, Spectate, SDK docs.
- [ ] **P0** Auth pages: Play (queue), My Box, Settings, Create, Admin.
- [ ] **P0** `/` **Home**: landing page — live matches list, quick-start buttons (Play Now → queue, Practice vs bots), platform blurb, featured recent match. Fully viewable logged out; Play buttons become sign-in prompts.
- [ ] **P0** `/play` **Play**: matchmaking page — mode picker, arena size picker (§1), queue join/leave, countdown, roster preview, link into the match view when found. Practice-vs-bots setup lives here too.
- [ ] **P0** `/match/[id]` **Match**: current arena experience — canvas, HUD (HP, cooldown, telemetry), roster/connection state, start controls when owner, viewer list later.
- [ ] **P0** `/matches` **Match history**: past matches list (mode, map, result, robots, date), filterable, links to detail.
- [ ] **P0** `/match/[id]/detail` (or the same route when finished): result summary, damage stats, replay link.
- [ ] **P0** `/box` **My Box**: current box console (status, SSH command, key management, quota, agent logs, `main.lua` preview) — extract what exists today.
- [ ] **P0** `/docs/sdk` **SDK docs**: existing Lua SDK reference, plus items/bots/map API as they land.
- [ ] **P1** `/profile/[handle]` **Profile**: player card — rating per mode, W/L, recent matches, robots and their equipment presets.
- [ ] **P1** `/leaderboard` **Leaderboard**: ranked ladder per mode/season; personal rank card when signed in.
- [ ] **P1** `/create` **Create / Workshop**: map editor and loadout/equipment editor (P2 items inside).
- [ ] **P1** `/settings` **Settings**: SSH keys (moved here from box page), UI prefs, notification toggles.
- [ ] **P1** `/spectate` **Spectate**: browse live matches with viewer counts; click into any match as viewer. Public, no sign-in.
- [ ] **P2** `/tournaments` **Tournaments**: bracket view, signup, admin-run events.
- [ ] **P2** `/admin` **Admin**: service health, cloud status, queue telemetry, feature flags, kill switch for matches.
- [ ] **P1** Per-page state discipline: each page owns its data loading and WebSocket subscriptions; no page leaks state into another. Shared auth/session and layout in a Svelte 5 store/rune module.
- [ ] **P0** Snapshot interpolation: decouple canvas rendering from the 10 Hz snapshot rate (rAF loop + position interpolation between ticks) so movement looks smooth. View-only; never affects simulation state.
- [ ] **P1** Guided onboarding: first-run wizard on Home (provision box → add key → deploy a variant → join a practice match) with per-step completion state, skippable and dismissible.
- [ ] **P2** Audio & visual juice: hit sounds, pickup chime, death explosion, screen shake; master mute in Settings.
- [ ] **P2** Responsive layout for public watch pages (Home, Match, Spectate, Leaderboard) down to phone widths.
- [ ] **P2** Accessibility: colorblind-safe team glyphs/shapes (never color alone), reduced-motion toggle honored by camera and effects.
- [ ] **P2** `/docs/api` **API reference**: HTTP endpoints and WS message schemas for viewer and agent protocols, documented next to `/docs/sdk`.

Rule: a page may show summaries of another system (e.g., profile shows recent matches) but never hosts another system's controls.

## 7. Persistence & progression

- [ ] **P0** Match records carry mode, map ID, item/event log summary, and per-robot damage stats.
- [ ] **P0** Per-player aggregate stats (wins, losses, damage dealt/taken, matches) updated on result.
- [ ] **P1** Replays: record full event stream to S3 per match; playback in match detail page (deterministic engine makes this cheap — store inputs + seed, re-simulate).
- [ ] **P1** Rating persistence (with §5).
- [ ] **P2** Seasons and badges (with §5).
- [ ] **P2** Achievements (first kill, survivor, medic — heal X HP via packs).
- [ ] **P2** XP/account level independent of rating; cosmetic unlocks only.

## 8. Social

- [ ] **P1** Presence: online/in-match/offline status for signed-in players.
- [ ] **P2** Friends list + invites.
- [ ] **P2** In-match chat (viewer chat first, robot-owning players can't chat while agents run).
- [ ] **P2** Clubs/teams with tags shown on leaderboard.
- [ ] **P2** Share buttons: match result link, replay link.

## 9. Spectating & streaming

- [ ] **P1** Viewer count per live match.
- [ ] **P1** Follow cam: viewer clicks a robot (or picks from the roster) and the camera locks to it, panning as it moves; ESC or click-empty releases back to full-arena view. Essential once Large arenas (§1) exceed the viewport — implies a camera with pan/zoom on the canvas.
- [ ] **P1** Auto-director option: camera follows the robot with the most recent kill or lowest HP; switches on big events (kill, airdrop, streak).
- [ ] **P1** Zoom controls: fit-all / 100 % / follow presets; zoom is view-only and never affects the server snapshot.
- [ ] **P1** Delayed spectate option (anti-scouting for ranked, e.g., 30 s delay).
- [ ] **P2** Observer camera modes: follow robot, free camera, item-layer overlay, damage overlay.
- [ ] **P0** Anonymous viewing is guaranteed by the access model (top of this doc); this section only adds viewer extras on top.

## 10. SDK & agent experience

- [ ] **P0** Document the full observation schema on `/docs/sdk` (items, obstacles, hazards as they land).
- [ ] **P1** SDK helpers: `nearest_item(type)`, `line_of_sight(x1,y1,x2,y2)`, `path_to(x,y)` (uses same data as bots).
- [ ] **P1** Better error feedback in agent messages (invalid intent reasons surfaced in box logs and UI).
- [ ] **P1** Reconnect resume: agent can rejoin a running match if the WebSocket drops (removes the "disconnect = fail" limitation in README).
- [ ] **P2** Local simulator: run the engine headless against a robot script without the server (fast iteration; pairs with bot difficulty presets).
- [ ] **P2** Multi-file workspace support (`main.lua` + modules) in snapshot.
- [ ] **P1** Script version history: every registration and deploy snapshots `/workspace/main.lua` to S3 with a timestamp; the box console lists versions and can restore one as the current workspace file.
- [ ] **P1** Lua syntax validation on deploy and registration: parse-check `/workspace/main.lua` and surface errors in box logs and the box console before the robot can join a match.
- [ ] **P1** SDK version handshake: the agent announces its SDK version on connect; the server warns in box logs and the UI when it predates the current protocol (e.g., after observation schema changes).

## 11. Ops, scaling & robustness

- [ ] **P0** Reconnect resume for viewers (join mid-match without missing state — mostly exists; keep).
- [ ] **P1** Multiple matches per process today, but one process total: extract match worker into horizontally scalable processes (SQS consumer group semantics).
- [ ] **P1** Match recovery on API restart: persist live match state each N ticks; resume or cleanly end on restart instead of dropping.
- [ ] **P1** Monitoring: queue depth, tick latency, agent deadline misses, WS fan-out lag on `/admin`.
- [ ] **P2** Rate limits per player on queue/match creation.
- [ ] **P2** Graceful maintenance mode (drain queue, refuse new matches, finish live ones).
- [ ] **P1** Determinism CI test: two engine runs with the same seed and inputs must produce byte-identical event streams; guards every replay, zone, and RNG feature in this doc.
- [ ] **P2** Degraded read-only mode: when Floci is unreachable, spectate/history pages serve cached state and new matches are refused with a clear banner instead of hard errors.

### Ports from config, not hardcoded

- [ ] **P1** Single source of truth for the SSH box port range: `SSH_PORT_START` / `SSH_PORT_END` in env. Provisioner, compose, firewall rules, VPS docs, and the SSH command shown in the UI must all derive from it — no hardcoded `22000`–`22999` anywhere else.
- [ ] **P1** Single-knob form: accept `SSH_PORT_MIN` + `SSH_PORT_COUNT` (default count 1000) and derive START/END, so one config value defines the whole range; keep explicit START/END as the override.
- [ ] **P1** Validation at startup: fail fast if the range is inverted, overlaps host SSH (22), or is too small for expected box count.
- [ ] **P1** `mise run vps:doctor` (or a new `vps:ports`) prints the exact open-port range and ready-to-paste firewall commands computed from the current config (see `docs/vps-deployment.md` "SSH box port range restricted by firewall").
- [ ] **P2** Dynamic port assignment audit: warn when free ports in the configured range run low relative to expected box growth.

## 12. Security & isolation (later phase, per AGENTS.md)

- [ ] **P2** Firecracker microVMs replacing Docker boxes (Linux + KVM required).
- [ ] **P2** Network egress policy for boxes, credential rotation, read-only base images.
- [ ] **P2** Adversarial testing of untrusted robot code (CPU/Mem/PID/IO abuse, token exfiltration attempts).
- [ ] **P2** Hard workspace quota via Linux project quotas or `storage-opt` on VPS.

## 13. Dynamic match moments

Scripted and emergent moments that make matches worth watching, not just two circles shooting. All server-side and deterministic.

- [ ] **P0** Kill feed: event stream already exists (`Event` in the engine) — render kills, pickups, and disconnects as a scrollable feed on the match page.
- [ ] **P0** Damage numbers / hit markers on the arena canvas.
- [ ] **P0** Match end recap card: winner, kills, damage dealt, items picked up, match duration.
- [ ] **P1** Shrinking arena (zone collapse): safe area shrinks after N ticks; outside it robots take DoT. Forces late-game contact instead of turtle standoffs. Configurable per match.
- [ ] **P1** Overtime / sudden death: if the tick limit hits, double damage + no healing for a final 100-tick window, then lowest-HP-percentage wins instead of a draw.
- [ ] **P1** Kill streak announcements: streaks of 3/5/7 get named states in the snapshot (`RAMPAGE`) and a HUD banner; dying resets the streak. Purely cosmetic to keep the engine rules simple.
- [ ] **P1** Bounty: a robot on a 3+ streak is marked and worth a shield pickup on kill.
- [ ] **P2** Low-HP comeback mechanics: brief overdrive after taking a killing-blow-survived hit, or rally aura near a dropped repair core.
- [ ] **P2** Mid-match world events (timer-driven, seeded RNG):
  - *Power surge*: weapons deal +25 % for 20 s, announced 10 s ahead.
  - *Blackout*: radar/observation range halved for 15 s — rewards scripted dead-reckoning.
  - *Meteor shards*: damaging debris lands at random spots for 30 s.
  - *Gold rush*: all item respawn timers halved for 60 s.
- [ ] **P2** Boss crate event: a neutral heavy drone spawns mid-map for 45 s; killing it drops an epic item. Gives passive robots something to contest.
- [ ] **P2** Respawn modes: casual/FFA variants where destroyed robots respawn after 10 s with reduced HP (mode flag, not default — elimination stays the ranked default).
- [ ] **P2** Highlight generation: from the event log, auto-clip "top moment" (most damage in a 5 s window) into the match recap.
- [ ] **P2** Announcer/HUD states in snapshot: `OVERTIME`, `ZONE CLOSING`, `AIRDROP INBOUND` as match-level status flags so clients don't re-derive them.

Design rule: every moment must be derivable from the event stream so replays (§7) and spectate delay (§9) show the same thing without extra storage.

---

## Suggested build order

Dependency-ordered; each step is demoable on its own.

1. **P0 core loop expansion**: router scaffold + Home/Play/Match/Box/Docs pages (§6) with snapshot interpolation on the match page — pure refactor of what exists.
2. **P0 gameplay**: maps data model + obstacles (§1), items with healing packs (§2), dummy/rookie bots (§4), basic queue (§5), kill feed + damage numbers + recap (§13). These together make "Practice vs bots on The Corridors with health packs and a kill feed" the new default demo.
3. **P1 systems**: matchmaking with ratings + bot fill (§5), fighter/sharpshooter bots (§4), weapons (§3), maps 3–5 (§1), drop-on-death + repair cores (§2), shrinking arena + overtime (§13), profile/leaderboard/history pages (§6, §7).
4. **P1 platform**: replays (§7), spectate page (§9), SDK helpers + reconnect resume (§10), worker scaling (§11).
5. **P2 polish**: tournaments, seasons, social, map editor, achievements, admin page, Firecracker.

## Assumptions

- Multi-page means client-side routes in the existing Svelte 5 app; no new backend framework.
- Bots run server-side as engine controllers; they never consume SSH boxes or robot tokens.
- Item pickup is automatic on contact — decided. No intent-based pickup; instead robots configure pickup in code (`auto_pickup` on/off, `pickup_types` filter) so they can ignore or target specific drops. Revisit only if playtesting shows filtering is not enough.
- Glicko-2 chosen over plain ELO for rating uncertainty handling; revisit if simpler is enough for a college demo.
- Map format is JSON in S3 so Floci → AWS swap needs no engine changes.
- Match moments (zone, overtime, events) are per-match config flags, never hardcoded, so tests can exercise each one deterministically.
