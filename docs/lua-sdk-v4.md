# Lua SDK 0.4

Use `local arena = require "arena"` and `arena.run({ decide = function(observation)
... end })`. Persistent Lua variables survive decision calls. The SDK continues
to support legacy observations; new controls require observation version 4.
Use `examples/lua-v4/main.lua` (balanced) as the working starting script.

## Strategy pack

`examples/lua-v4/` ships six more Rust-only strategies that match the box
console templates: `scout` (transit recon), `assault` (mid-range brawler),
`sniper` (600-900 railgun control), `support` (repair fields + smoke),
`sentinel` (safe-zone denial + mines), and `scavenger` (loot priorities +
explicit weapon equips + transit rides). All assert `obs.version == 4`.

## Observations

`self` contains your robot's equipment, health, shield, energy, cooldowns,
`weapons`, `activeWeapon`, `inventory`, `actionResults`, and strategy label.
`robots` contains visible opponents and allied status. Invisible opponents are
not updated. Terrain, sites, transit, and hazards are public. Nearby loot,
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
paths can be pending or unreachable. Helpers use public geometry and observed
entities, not hidden engine state.

`find_consumable` reads your inventory and returns a zero-based consume slot;
`find_loot` searches visible map containers by content kind. `transit_route`
requires a goal point and returns `{ entry, goal }`: ride with
`transit = route.entry.id`.

The SDK sends `X-Robot-SDK-Version: 0.4.0` during the WebSocket upgrade.
Register with `sdkVersion: "0.4.0"` and a legal 60-point loadout. Registration
freezes the main script for the current match. Browser saves and SSH edits apply
to the workspace for the next registration. Save, syntax validation, testing,
and registration are separate operations.
