# Lua SDK 0.4

Use `local arena = require "arena"` and `arena.run({ decide = function(observation)
... end })`. Persistent Lua variables survive decision calls. The SDK continues
to support legacy observations; new controls require observation version 4.
Use `examples/lua-v4/main.lua` (balanced) as the working starting script.

## Strategy pack

Every shipped strategy is a short configuration of `arena.tactics`, the SDK's
complete decision loop: `main` (balanced), `scout` (constant scans, transit
rides, squad reports), `assault` (close range, dashes, mines), `sniper` (holds
600-950 units for steady shots, cloaks when hurt), `support` (trails allies,
repair fields, smoke), `sentinel` (walks a beat around the post nearest the
zone centre, mines approaches), and `scavenger` (long loot detours, weapon
priorities). `mise run v4:behaviour` runs each one in two real boxes and checks
that its robots travel, fight, and fire.

`arena.tactics(options)` returns a `decide` function. Each decision it:
escapes when pinned against cover, fights the best visible target (retreating
to cover when hurt), rotates into the zone, heals, picks up nearby loot, hunts
remembered contacts, and otherwise patrols. Options: `name` (label prefix),
`range`, `preferred` (engagement distance), `retreat_hp`, `loot_reach`
(`{ early, late }`), `scan` (`"idle"`, `"always"`, `"never"`), `pickup`
(pickup priorities), and hooks `on_enemy(obs, target, ctx)`, `idle(obs, ctx)`
(return an action or a goal point), and `decorate(obs, action, ctx)`.
`ctx.travel(obs, goal, label)` follows a bounded path with recovery.

## Observations

`self` contains your robot's equipment, health, shield, energy, cooldowns,
`weapons`, `activeWeapon`, `inventory`, `actionResults`, and strategy label.
`robots` contains visible opponents and allied status. Invisible opponents are
not updated. Terrain, sites (with a visual-only `biome`), transit, and hazards are public. Nearby loot,
projectiles, and mines are filtered by range and occlusion. `events` includes
perceivable events and public announcements; scan contacts are coarse and omit
health and inventory. Save last-seen contacts in your own Lua memory.

`tick` advances at 20 Hz. Decisions arrive at 10 Hz. Coordinates are world units;
headings are degrees. Inventory and equipment action slots are **zero-based**.
`controlledRobotId` identifies your registered robot; `spectating` is true when
it is eliminated. Squad observations then follow a living teammate. Returned
actions still target your own robot and cannot control that teammate.

## Actions

Return `arena.control({...})`. Only intent crosses the network.

- `throttle`: -1 to 1; negative reverses at half speed. `brake`: boolean.
- `turn`: -1 to 1 body turn; `aim`: absolute turret heading.
- `fire`, `cruise`: continuous booleans. Cruise disables firing.
- `dash`, `scan`: single actions, subject to energy and cooldown.
- `weapon`: select slot 0 or 1; heat and cooldown persist while holstered.
- `utility`: equipped utility ID such as `smoke_projector` or `repair_field`.
- `consume`: consumable slot 0..3. Healing channels cancel on movement, firing,
  or damage. `dropSlot`: drop a consumable stack.
- `pickup`: visible nearby container ID. Fills free slots/stacks; never replaces
  equipment automatically.
- `pickupPriorities`: up to 32 item IDs, highest priority first. Enables nearby
  automatic pickup of these kinds only. An empty array disables it.
- `equip`: `{ container = "loot-id", kind = "weapon:railgun", slot = 0 }`.
  Explicitly replaces a slot; the old item stays in that container. Modules use
  their plain ID, utilities use `utility:mine_dispenser`. Dropped equipment
  preserves weapon heat/cooldown and utility charges/cooldown.
- `dropEquipment`: `{ group = "weapon" | "module" | "utility", slot = 0 }`.
  Dropping the last combat weapon is rejected.
- `transit`: link ID; two-second stationary activation, ten-second cooldown.
- `message`: bounded 128-byte squad report; `label`: 64-byte strategy state.

Continuous actions expire after 250 ms. Discrete actions run once per accepted
sequence. The SDK supplies sequence and observed tick; identities come from the
authenticated socket. Rejections include `MALFORMED_ACTION`,
`UNSUPPORTED_VERSION`, `OUT_OF_RANGE`, `DUPLICATE_SEQUENCE`,
`FUTURE_OBSERVATION`, and `STALE_OBSERVATION`. Gameplay rejections appear in
`self.actionResults` when reported by the rule.

## Helpers

Existing geometry, distance, bearing, intercept, and visibility helpers remain.
New helpers include `weapon_ready`, `drive_to`, `find_consumable`,
`find_loot`, `nearest_ally`, `lead_for`, `strafe_around`, `dodge`,
`danger_level`, `scan_contacts`, `stuck_tracker`, `is_stuck`, `safe_zone_goal`, `transit_route`,
`begin_path`, `advance_path`, and `follow_path`. Search work is bounded per call;
paths can be pending or unreachable.

Tactical helpers:

- `raycast(x, y, heading, max, obstacles?)` returns distance to the first
  obstacle and the obstacle hit (nil when clear).
- `obstacles_near(obs, x, y, radius)` lists cover within a radius, nearest first.
- `find_cover(obs, threat, { radius = 500, clearance = 30 })` returns the nearest
  clear point hidden from `threat` behind nearby cover, plus that obstacle.
- `contact_tracker(ttl)` and `update_contacts(tracker, obs)` remember enemies
  out of sight. Each contact has `age` in ticks, `visible`, `lastX/lastY`, and
  `x/y` extrapolated from last velocity for up to one second.
- `best_target(obs, { range = 700, hpWeight = 3 })` picks a visible enemy in
  range with line of sight, favouring near and weak targets.
- `nearest_site(obs, { kind = "armoury", biome = "urban" })`.
- `engage(obs, enemy, memory, { preferred, label })` closes in, strafes inside
  its range band (orbit direction flips on a timer), or kites backwards while
  firing; aim leads with the enemy's velocity and the weapon's projectile speed.
- `patrol(obs, memory)` returns deterministic goals at sites inside the zone,
  re-picked on arrival or after 20 seconds.
- `unstick(obs, memory)` plus `note_action(memory, action)`: after about
  0.8 s of commanded driving without progress, backs out while turning.

Debug drawing: `arena.draw.point(x, y, color)`, `line(x1, y1, x2, y2, color)`,
`circle(x, y, r, color)`, and `text(x, y, text, color)` attach up to 24 marks
to the current decision (colour `"#rrggbb"`, text up to 40 bytes). They appear
only in the owner's live view and never reach the simulation. The input
envelope carries them as a top-level `debug` array next to `action`. The
gateway rejects malformed marks with `OUT_OF_RANGE`. `arena.tactics` draws its
goal, route, and target automatically; pass `debug = false` to turn that off.

`drive_to` brakes inside `arrive` (default 25, inside the 35-unit pickup
reach), slows on approach, crawls through sharp turns, and reverses toward
nearby points behind the robot instead of pivoting on the spot. `follow_path`
cuts corners to the furthest visible waypoint. `begin_path` plans around
walls and still works when the robot is already hugging cover.

`line_of_sight`, `raycast`, and cover checks build a uniform-grid index once per
geometry array, so dense district maps (about 6,000 obstacles) stay within the
decision budget. `mise run sdk:test` runs the helper tests under the box's Lua 5.4.
Helpers use public geometry and observed entities, not hidden engine state.

`find_consumable` reads your inventory and returns a zero-based consume slot;
`find_loot` searches visible map containers by content kind. `transit_route`
requires a goal point and returns `{ entry, goal }`: ride with
`transit = route.entry.id`.

The SDK sends `X-Robot-SDK-Version: 0.4.0` during the WebSocket upgrade.
Register with `sdkVersion: "0.4.0"` and a legal 60-point loadout. Registration
freezes the main script for the current match. Browser saves and SSH edits apply
to the workspace for the next registration. Save, syntax validation, testing,
and registration are separate operations.
