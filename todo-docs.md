# Feature roadmap / TODO

Goal: turn the Review 1 prototype into a real programmable-robot platform. This
doc tracks shipped work and remaining features. Checked items have tests or an
end-to-end implementation. Unchecked items still need work.

## Current state (baseline)

- Hash-routed Svelte 5 app with public and authenticated pages and an
  interpolated 10 Hz arena canvas.
- Five built-in maps (obstacles, spawn sets, item spawn zones) and custom
  per-match arena width/height with clamps. Crater's edge lava deals periodic
  hazard damage inside its zone.
- Seven weapons (plasma, cannon, machine gun, railgun, incendiary, cryo, EMP),
  burn/slow/EMP status effects, per-match friendly-fire/regen/ramming options,
  selectable bot personalities, zone collapse, and overtime.
- Deterministic items (heal, shield, overdrive, rapid fire) with randomized
  spawner loot (common weapons 8%, cannon 3%, railgun 1%) and supply-drop
  bursts (1-in-8 spawns), repair-core and weapon drops on death, 10% weapon
  crits at 1.5x damage, kill-streak announcements with bounty; all engine RNG
  seeded from match config.
- Four server bot difficulties with collision-probe movement; manual matches,
  practice matches, and an in-memory duel queue with bot fill, connection
  grace, and automatic requeue after cancellation.
- Duel Elo ratings, per-player stats, kill feed, damage numbers, and an
  end-of-match recap.
- Floci stores scripts, versioned script snapshots (list/restore API), match
  records, results, player stats, replay events, and box metadata.
- Public match history, profiles, leaderboard, spectating, SDK docs, and API
  docs.

Priority labels: **P0** = next demo, **P1** = makes it feel like a real game, **P2** = platform polish.

## Access model: watching is always free

Anyone can open the website and watch without signing in. Sign-in is only required for actions that own resources.

- [x] **P0** Public (no login): home page, live match view, match list, match detail/results, leaderboards, profiles, SDK docs, spectate page.
- [x] **P0** Login required (matches today's behavior): box provisioning, SSH keys, queueing/playing, match creation, settings.
- [x] **P1** Viewer WebSocket for public pages works anonymously. The API does not gate `/ws/matches/{id}` behind auth.
- [x] **P1** "Sign in to play" CTAs on public pages: watching a match shows JOIN QUEUE / SIGN IN buttons only if the viewer is authenticated; otherwise a sign-in prompt.
- [ ] **P2** Guest identity for social features: anonymous viewers get a read-only name like `Guest-1234`; posting chat or anything that mutates requires sign-in.
- [ ] **P2** Share preview: match links render a title card (map, robots, status) even when opened logged-out.

---

## 1. Maps & arenas

Every match should pick a map, not reuse one flat rectangle.

- [ ] **P0** Map data model: JSON map definition (name, size, walls, obstacles, spawn points, item spawn zones) stored in S3 and referenced by match config.
- [x] **P0** Engine support for static obstacles: circle/AABB collision, projectile blocking, line-of-sight checks.
- [x] **P1** 3–5 starter maps:
  - Open Field (current rectangle, kept as the training map).
  - Four Corners (center cover, corner spawns).
  - The Corridors (walls forming lanes; rewards pathing).
  - Pillars (symmetric pillar field).
  - Crater (danger zones on the edges).
- [ ] **P1** Spawn point system: per-map spawn sets, no-overlap placement, and center-facing headings work. Seeded random selection within each team's set remains.
- [x] **P1** Arena variants beyond size: different dimensions per map (small duel arena vs large team arena).
- [ ] **P0** Match-creatable arena size: when creating a match, pick Small (600 × 400), Medium (800 × 500), Large (1200 × 800), or custom width and height with sane clamps. Engine constants become per-match config carried in the snapshot. Bots and item spawn zones scale to it.
- [ ] **P2** Visual map themes (background color/texture only; engine stays 2D geometry).
- [ ] **P2** Map hazards: low-friction patches, one-way gates. Damage zones are shipped and tested (Crater edge lava deals periodic `hazard_damage` inside its zone).
- [ ] **P2** Map editor page (draw walls/spawns, export JSON, submit for review).
- [ ] **P2** Map pick/ban or veto in match creation.

Engine note: map ID must be part of the match config snapshot so all clients and the worker render/simulate the same layout deterministically.

## 2. Items & pickups

Healing and power-ups that spawn during a match. Server-authoritative, like everything else.

- [x] **P0** Item entity in engine: type, position, spawn tick, active/inactive, pickup radius.
- [x] **P0** Item spawner: per-map spawn zones, spawn interval (e.g., every 100–200 ticks), max concurrent items, respawn delay after pickup.
- [x] **P0** Healing pack: +25 HP (capped at max HP), disappears on pickup, 10–15 s respawn.
- [x] **P1** Shield: temporary 50-point overshield that decays over 20 s.
- [x] **P1** Overdrive: +50 % move speed for 8 s.
- [x] **P1** Rapid fire: cooldown halved for 6 s.
- [ ] **P2** Weapon variants as pickups (see §3).
- [ ] **P2** Ammo/energy economy: firing costs energy; energy cells as items; regen when idle.
- [ ] **P2** Negative items / traps (decoy health pack) for P2 maps.
- [x] **P1** SDK additions: observation includes visible items with position, type, and respawn tick. Pickup is automatic on contact. No `pick_up` intent exists. Robots configure pickup behavior with `arena.configure({ auto_pickup = true, pickup_types = { "heal", "repair-core" } })`. Default behavior picks up every item.
- [ ] **P1** Client rendering: item glyphs and pickup flashes work. Effect timers on the HUD remain.
- [x] **P1** Balance config: item rates/effects in a single tunable config file so tests and demos can tweak them.

Determinism note: item spawn RNG must be seeded from match ID so replays and all viewers agree.

### Item drops

Where items come from, beyond the fixed spawner.

- [x] **P1** Drop on death: a destroyed robot drops a repair core (+15 HP) at its death spot; killer-adjacent robots fight over it. Classic comeback mechanic.
- [x] **P1** Weapon drops on death: the destroyed robot's equipped pickup (if any) falls to the ground and is lootable.
- [ ] **P2** Supply airdrop: once or twice per match, a marker appears with a 5 s countdown, then lands a high-tier item. Announce it to every robot in the observation.
- [ ] **P2** Rarity tiers: common (heal, ammo) / rare (shield, overdrive) / epic (railgun, airdrop-only). Drop tables per map, weight-configurable.
- [ ] **P2** Crate blockers: crates with HP that must be shot open, dropping 2–3 items.
- [x] **P1** Drop physics note: drops land at death or landing coordinates clamped away from walls. No bounce simulation.
- [x] **P1** Drops in observations: dropped items use the same item schema as spawner items so SDK/clients need one code path.

## 3. Combat depth

- [ ] **P1** Weapon set: plasma, cannon, machine gun, railgun, and weapon pickups work. Railgun charge-up remains.
- [x] **P1** Per-weapon properties table: damage, cooldown, projectile speed, range, spread, knockback.
- [ ] **P2** Armor pieces: trade speed for damage reduction.
- [x] **P2** Status effects: burn (DoT), slow, EMP (brief weapon lock), applied by incendiary/cryo/EMP weapons.
- [ ] **P2** Equipment slots (loadout): configure primary weapon, utility, and passive per robot before match start.
- [x] **P2** Friendly fire toggle per match (API option, default off).
- [x] **P1** Out-of-combat regen per match (API option; regen applies after 50 ticks without damage).
- [x] **P2** Ramming damage toggle per match with pair cooldown (API option).

## 4. Bots

Server-side AI robots let one player demo, test scripts, and fill rosters. The server generates bot intents through the same engine controller contract. Bots need no SSH box or token.

- [x] **P0** Bot controller implementing the engine `Controller` interface (engine-side only; indistinguishable from an agent in the simulation loop).
- [ ] **P0** Difficulty levels exist. Fighter item seeking, Sharpshooter cover use, and item denial remain:
  - *Dummy*: stands still or moves in a fixed pattern (scripted target practice).
  - *Rookie*: random walk + fire when enemy in line of sight.
  - *Fighter*: seek enemy, aim with lead prediction, retreat when low HP, seek health packs.
  - *Sharpshooter*: adds strafing, cover usage, item denial (takes items before the player can).
- [x] **P1** Bot personalities: aggressive / evasive / camper presets that tune the same controller knobs, selectable per match (`botPersonality`).
- [ ] **P1** Bots use maps properly: obstacle-aware movement (simple A* or potential fields on the grid; no physics-heavy pathfinding).
- [ ] **P1** Match creation can add bots before humans register. Adding bots to an existing lobby remains.
- [x] **P1** Solo practice mode: player + N bots, no result recorded (or recorded as `practice`).
- [ ] **P2** Bots as matchmaking fillers (see §5): start a match when queue is short on players.
- [ ] **P2** Bot skins/names so viewers can tell them apart in roster and leaderboard.
- [ ] **P2** Bot-vs-bot exhibition mode for the landing page.

## 5. Matchmaking

Replace "create match + share code" with a queue that works for humans and bots.

- [x] **P0** Match queue API: `POST /api/queue` (join with robot), `DELETE /api/queue` (leave), queue status in UI.
- [ ] **P0** Simple pairing: the first two queued players form a duel. Configurable roster size remains.
- [x] **P0** Auto-start: match created, both robots registered, agents must connect within a grace window (30 s) or the match is cancelled and players are requeued automatically.
- [x] **P1** Engine support for N robots per team: roster arrays end-to-end (snapshot, events, HUD, results). Prerequisite for the team sizes below; today's engine is 1v1-shaped.
- [ ] **P1** Team support: queue for 1v1, 2v2, 3v3; queue fills teams fairly (alternate team assignment by queue order).
- [x] **P1** Skill rating: Glicko-2 (or simple ELO) stored per player per mode; updated on result. Rating shown on profile.
- [ ] **P1** Rating-based pairing: within ±150 rating window, widen the window the longer a player waits.
- [ ] **P1** Ranked vs casual queues: ranked updates rating; casual doesn't.
- [x] **P1** Bot fill: after configurable wait (e.g., 45 s), fill empty roster slots with bots and start.
- [x] **P1** Queue cancellation and timeout handling (agent never connects, duplicate queue attempts, disconnect during countdown).
- [ ] **P2** Parties: group with a friend, queue together, placed on the same team.
- [ ] **P2** Seasons: rating soft resets, season badges, archived season leaderboards.
- [ ] **P2** Placement matches: rating unprovisioned until 5 ranked games.
- [ ] **P2** Matchmaking telemetry: queue wait times, pairing deltas (admin page).
- [ ] **P2** Alternative win conditions: FFA (everyone vs everyone) and King of the Hill (hold the center zone for N ticks). Each is a mode flag with its own winner logic in the engine, not a special case in the queue.
- [ ] **P2** Private matches: optional join password or unlisted flag so shared-code matches can be gated.

Implementation note: queue state belongs in DynamoDB (atomic updates) or in-memory single-process to start; the SQS match-job path is already the right handoff to the worker.

## 6. Multi-page UI (information architecture)

Split `App.svelte` into real routes, one system per page. Svelte 5 + a lightweight router (hash routing is fine for the prototype; no SSR needed).

- [x] **P0** Router scaffold + shared layout (nav bar, auth state, connection status).
- [x] **P0** Public pages (no login, see access model): Home, Match, Match history/detail, Profile, Leaderboard, Spectate, SDK docs.
- [x] **P0** Auth pages: Play (queue), My Box, Settings, Create, Admin.
- [x] **P0** `/` **Home**: landing page with live matches, Play Now, Practice vs bots, platform summary, and a featured match. Logged-out users can view it. Play buttons open sign-in.
- [x] **P0** `/play` **Play**: mode and arena size pickers, queue join and leave, roster preview, matched-game navigation, and practice setup.
- [x] **P0** `/match/[id]` **Match**: arena canvas, HP and telemetry HUD, roster state, connection state, and owner start controls.
- [x] **P0** `/matches` **Match history**: past matches list (mode, map, result, robots, date), filterable, links to detail.
- [x] **P0** `/match/[id]/detail` (or the same route when finished): result summary, damage stats, replay link.
- [ ] **P0** `/box` **My Box**: current box console has status, SSH command, key management, quota, and `main.lua` preview. Agent logs remain.
- [x] **P0** `/docs/sdk` **SDK docs**: existing Lua SDK reference, plus items/bots/map API as they land.
- [ ] **P1** `/profile/[handle]` **Profile**: player card has rating per mode, W/L, and recent matches. Robot equipment presets remain.
- [ ] **P1** `/leaderboard` **Leaderboard**: ranked ladder works per mode. Seasons and the signed-in player's rank card remain.
- [ ] **P1** `/create` **Create / Workshop**: map editor and loadout/equipment editor (P2 items inside).
- [ ] **P1** `/settings` **Settings**: UI prefs and notification toggles work. SSH key management remains on My Box.
- [x] **P1** `/spectate` **Spectate**: browse live matches with viewer counts; click into any match as viewer. Public, no sign-in.
- [ ] **P2** `/tournaments` **Tournaments**: bracket view, signup, admin-run events.
- [ ] **P2** `/admin` **Admin**: service health, cloud status, queue telemetry, feature flags, kill switch for matches.
- [x] **P1** Per-page state discipline: each page owns its data loading. Shared auth/session and active match state stay in the layout.
- [x] **P0** Snapshot interpolation: decouple canvas rendering from the 10 Hz snapshot rate (rAF loop + position interpolation between ticks) so movement looks smooth. View-only; never affects simulation state.
- [ ] **P1** Guided onboarding: first-run wizard on Home (provision box → add key → deploy a variant → join a practice match) with per-step completion state, skippable and dismissible.
- [ ] **P2** Audio & visual juice: hit sounds, pickup chime, death explosion, screen shake; master mute in Settings.
- [x] **P2** Responsive layout for public watch pages (Home, Match, Spectate, Leaderboard) down to phone widths.
- [x] **P2** Accessibility: colorblind-safe team glyphs/shapes (never color alone), reduced-motion toggle honored by camera and effects.
- [x] **P2** `/docs/api` **API reference**: HTTP endpoints and WS message schemas for viewer and agent protocols, documented next to `/docs/sdk`.

Rule: a page may show summaries of another system (e.g., profile shows recent matches) but never hosts another system's controls.

## 7. Persistence & progression

- [x] **P0** Match records carry mode, map ID, item/event log summary, and per-robot damage stats.
- [x] **P0** Per-player aggregate stats (wins, losses, damage dealt/taken, matches) updated on result.
- [ ] **P1** Replays: S3 event recording and match-detail playback work. Input and seed re-simulation remains.
- [x] **P1** Rating persistence (with §5).
- [ ] **P2** Seasons and badges (with §5).
- [ ] **P2** Achievements such as first kill, survivor, and healing a set amount with packs.
- [ ] **P2** XP/account level independent of rating; cosmetic unlocks only.

## 8. Social

Later feature: deferred by default. Ask before working on anything in this
section.

- [ ] **P1** Presence: online/in-match/offline status for signed-in players.
- [ ] **P2** Friends list + invites.
- [ ] **P2** In-match chat (viewer chat first, robot-owning players can't chat while agents run).
- [ ] **P2** Clubs/teams with tags shown on leaderboard.
- [ ] **P2** Share buttons: match result link, replay link.

## 9. Spectating & streaming

- [x] **P1** Viewer count per live match.
- [ ] **P1** Follow cam: viewer selects a robot and the camera tracks it. Escape or clicking empty space restores the full-arena view. Large arenas need pan and zoom.
- [ ] **P1** Auto-director option: camera follows the robot with the most recent kill or lowest HP; switches on big events (kill, airdrop, streak).
- [ ] **P1** Zoom controls: fit-all / 100 % / follow presets; zoom is view-only and never affects the server snapshot.
- [ ] **P1** Delayed spectate option (anti-scouting for ranked, e.g., 30 s delay).
- [ ] **P2** Observer camera modes: follow robot, free camera, item-layer overlay, damage overlay.
- [x] **P0** Anonymous viewing is guaranteed by the access model (top of this doc); this section only adds viewer extras on top.

## 10. SDK & agent experience

- [x] **P0** Document the full observation schema on `/docs/sdk` (items, obstacles, hazards as they land).
- [x] **P1** SDK helpers: `nearest_item(type)`, `line_of_sight(x1,y1,x2,y2)`, `path_to(x,y)` (uses same data as bots).
- [ ] **P1** Better error feedback in agent messages (invalid intent reasons surfaced in box logs and UI).
- [x] **P1** Reconnect resume: agent can rejoin a running match if the WebSocket drops (removes the "disconnect = fail" limitation in README).
- [ ] **P2** Local simulator: run the engine headless against a robot script without the server (fast iteration; pairs with bot difficulty presets).
- [ ] **P2** Multi-file workspace support (`main.lua` + modules) in snapshot.
- [x] **P1** Script version history: every registration and deploy snapshots `/workspace/main.lua` to S3 with a timestamp; API list and restore work, and the box console lists and restores versions.
- [x] **P1** Lua syntax validation on deploy and registration: parse-check `/workspace/main.lua` and surface errors in the box console before the robot can join a match.
- [ ] **P1** SDK version handshake: the agent announces its SDK version and the server adds protocol warnings to agent logs. Box UI warning remains.

## 11. Ops, scaling & robustness

- [x] **P0** Reconnect resume for viewers. Joining mid-match returns current state.
- [ ] **P1** Multiple matches per process today, but one process total: extract match worker into horizontally scalable processes (SQS consumer group semantics).
- [ ] **P1** Match recovery on API restart: persist live match state each N ticks; resume or cleanly end on restart instead of dropping.
- [ ] **P1** Monitoring: queue depth, tick latency, agent deadline misses, WS fan-out lag on `/admin`.
- [x] **P2** Rate limits per player on queue/match creation.
- [x] **P2** Graceful maintenance mode (drain queue, refuse new matches, finish live ones).
- [x] **P1** Determinism CI test: two engine runs with the same seed and inputs must produce byte-identical event streams; guards every replay, zone, and RNG feature in this doc.
- [ ] **P2** Degraded read-only mode: when Floci is unreachable, spectate/history pages serve cached state and new matches are refused with a clear banner instead of hard errors.

### Ports from config, not hardcoded

- [x] **P1** Single source of truth for the SSH box port range: environment variables drive the provisioner, Compose, VPS docs, and the SSH command returned by the API.
- [x] **P1** Single-knob form: accept `SSH_PORT_MIN` + `SSH_PORT_COUNT` (default count 1000) and derive START/END, so one config value defines the whole range; keep explicit START/END as the override.
- [x] **P1** Validation at startup: fail fast if the range is inverted, overlaps host SSH (22), or is too small for expected box count.
- [x] **P1** `mise run vps:doctor` prints the exact open-port range and ready-to-paste firewall commands computed from the current config.
- [x] **P2** Dynamic port assignment audit: warn when free ports in the configured range run low relative to expected box growth.

## 12. Security & isolation (later phase, per AGENTS.md)

Later feature: deferred by default. Ask before working on anything in this
section.

- [ ] **P2** Firecracker microVMs replacing Docker boxes (Linux + KVM required).
- [ ] **P2** Network egress policy for boxes, credential rotation, read-only base images.
- [ ] **P2** Adversarial testing of untrusted robot code (CPU/Mem/PID/IO abuse, token exfiltration attempts).
- [ ] **P2** Hard workspace quota via Linux project quotas or `storage-opt` on VPS.

## 13. Dynamic match moments

Scripted and emergent moments give viewers more to follow than repeated shots. Every moment stays server-side and deterministic.

- [x] **P0** Kill feed: render kills, pickups, and disconnects as a scrollable feed on the match page.
- [x] **P0** Damage numbers / hit markers on the arena canvas.
- [x] **P0** Match end recap card: winner, kills, damage dealt, items picked up, match duration.
- [ ] **P1** Shrinking arena logic and deterministic damage exist. Match creation does not expose zone settings yet.
- [x] **P1** Overtime / sudden death: if the tick limit hits, double damage + no healing for a final 100-tick window, then lowest-HP-percentage wins instead of a draw.
- [x] **P1** Kill streak announcements: the engine emits named streak states and resets them on death; the match page shows a streak banner plus feed entries.
- [x] **P1** Bounty: a robot on a 3+ streak is marked and worth a shield pickup on kill.
- [ ] **P2** Low-HP comeback mechanics: brief overdrive after taking a killing-blow-survived hit, or rally aura near a dropped repair core.
- [ ] **P2** Mid-match world events (timer-driven, seeded RNG):
  - *Power surge*: weapons deal +25 % for 20 s, announced 10 s ahead.
  - *Blackout*: radar and observation range halved for 15 s. This rewards scripted dead reckoning.
  - *Meteor shards*: damaging debris lands at random spots for 30 s.
  - *Gold rush*: all item respawn timers halved for 60 s.
- [ ] **P2** Boss crate event: a neutral heavy drone spawns mid-map for 45 s; killing it drops an epic item. Gives passive robots something to contest.
- [ ] **P2** Respawn modes: casual and FFA variants where destroyed robots return after 10 s with reduced HP. Ranked mode keeps elimination.
- [ ] **P2** Highlight generation: from the event log, auto-clip "top moment" (most damage in a 5 s window) into the match recap.
- [x] **P2** Announcer/HUD states in snapshot: `OVERTIME` and `ZONE CLOSING` ship in snapshot `announcements` and render as HUD chips. `AIRDROP INBOUND` arrives with supply airdrops (§2).

Design rule: every moment must be derivable from the event stream so replays (§7) and spectate delay (§9) show the same thing without extra storage.

---

## Suggested build order

Dependency-ordered; each step is demoable on its own.

1. **P0 core loop expansion**: router scaffold, Home, Play, Match, Box, and Docs pages with snapshot interpolation.
2. **P0 gameplay**: maps data model + obstacles (§1), items with healing packs (§2), dummy/rookie bots (§4), basic queue (§5), kill feed + damage numbers + recap (§13). These together make "Practice vs bots on The Corridors with health packs and a kill feed" the new default demo.
3. **P1 systems**: matchmaking with ratings + bot fill (§5), fighter/sharpshooter bots (§4), weapons (§3), maps 3–5 (§1), drop-on-death + repair cores (§2), shrinking arena + overtime (§13), profile/leaderboard/history pages (§6, §7).
4. **P1 platform**: replays (§7), spectate page (§9), SDK helpers + reconnect resume (§10), worker scaling (§11).
5. **P2 polish**: tournaments, seasons, social, map editor, achievements, admin page, Firecracker.

## Assumptions

- Multi-page means client-side routes in the existing Svelte 5 app; no new backend framework.
- Bots run server-side as engine controllers; they never consume SSH boxes or robot tokens.
- Item pickup is automatic on contact. Robots configure `auto_pickup` and `pickup_types` in code to ignore or target drops. Revisit only if playtesting shows filtering is not enough.
- Glicko-2 chosen over plain ELO for rating uncertainty handling; revisit if simpler is enough for a college demo.
- Map format is JSON in S3 so Floci → AWS swap needs no engine changes.
- Match moments (zone, overtime, events) are per-match config flags, never hardcoded, so tests can exercise each one deterministically.
