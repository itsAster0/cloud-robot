<script lang="ts">

  let { section = 'sdk' }: { section?: 'sdk' | 'api' } = $props();
  let tab = $state<'sdk' | 'api'>('sdk');
  $effect(() => { tab = section; });

  function jumpToSection(event: MouseEvent, id: string) {
    event.preventDefault();
    const target = document.getElementById(id);
    target?.scrollIntoView({ block: 'start' });
    target?.focus({ preventScroll: true });
  }

  const quickstart = `local arena = require "arena"
local state = { route = nil, stuck = arena.stuck_tracker() }

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(obs)
    assert(obs.version == 4, "requires a v4 arena")
    local enemy = arena.nearest_enemy(obs)
    if enemy then
      return arena.drive_to(obs, enemy, {
        aim = arena.bearing(obs.self, enemy),
        fire = arena.weapon_ready(obs),
        label = "ENGAGE",
      })
    end
    local heal = arena.find_consumable(obs, "medkit")
      or arena.find_consumable(obs, "repair_pack")
    if heal and obs.self.hp < obs.self.maxHp * 0.7 then
      return arena.control({ brake = true, consume = heal, label = "REPAIR" })
    end
    return arena.drive_to(obs, arena.safe_zone_goal(obs), { label = "POST" })
  end,
})`;

  const observation = `observation = {
  version = 4, tick = 1234, revision = 1,
  controlledRobotId = "human-000", spectating = false,
  self = {
    hp, maxHp, shield, energy, x, y, heading, turretHeading,
    weapons = { { kind, heat, ready_at, overheated } },
    activeWeapon = 0, inventory = { { kind, count } },
    charges = { scan = 1 }, cooldowns = { scan = 0 },
    actionResults = { "OK" }, messages = { ... }, label = "ENGAGE",
  },
  robots = { { robotId, name, team, x, y, heading, hp, alive } },
  containers = { { item_id, x, y, contents = { { kind, count } } } },
  projectiles = { { projectile_id, owner_id, x, y, vx, vy, damage } },
  mines = { { mine_id, owner_id, x, y, arm_tick } },
  fields = { { id, kind, x, y, end_tick } },   -- smoke / repair zones
  sites = { { id, kind, x, y } },              -- depots, armouries, bunkers
  transit = { { id, x, y, target_x, target_y } },
  hazards = { { id, kind, x, y, width, height } },
  zone = { active, x, y, radius, damage, stage },
  events = { { tick, type, robot_id, damage, message } },
  arenaWidth = 42000, arenaHeight = 26250,
}`;

  const actionEnvelope = `action = {
  type = "action", version = 4, sdkVersion = "0.4.0",
  sequence = 88,            -- echoes the observed tick sequence
  observedTick = 1234,      -- tick this decision was based on
  action = {
    throttle = 0.8,         -- -1..1, negative reverses at half speed
    brake = false, turn = 0.2, aim = 137.5,
    fire = true, cruise = false, dash = false, scan = false,
    weapon = 0, utility = "smoke_projector",
    consume = 1, dropSlot = 2,
    pickup = "loot-3-7",
    pickupPriorities = { "weapon:railgun", "shield_cell" },
    equip = { container = "loot-3-7", kind = "weapon:railgun", slot = 0 },
    dropEquipment = { group = "utility", slot = 1 },
    transit = "transit-3",
    message = "pushing site-3", label = "ENGAGE",
  },
}`;

  const configSchema = `POST /api/v4/matches
{
  "mode": "br-solo | br-squad | sandbox | quick-duel",
  "capacity": 64,              -- 1..256, squads divisible by 4
  "width": 42000,              -- 1200..48000
  "height": 26250,             -- 750..30000
  "durationSeconds": 1080,     -- 10..2700
  "seed": 0,                   -- 0 = random, else deterministic map
  "siteCount": 64,             -- 0 = auto (4 small / 64 large), else 1..256
  "coverPerSite": 8,           -- 0..12, 0 = open ground (with explicit sites)
                               -- plus one wild cluster per site in open ground
  "lootPerSite": 16,           -- 0..32
  "friendlyFire": false,
  "liveEdit": false            -- sandbox map transactions
}`;

  const loadoutSchema = `loadout = {
  chassis = "scout | generalist | heavy",        -- 10 / 15 / 25 pts
  weapon = "plasma | machine_gun | shotgun | cannon | railgun
          | grenade | incendiary | cryo | emp",  -- 10..25 pts
  modules = { "...", "..." },   -- max 2, 10 pts each:
    -- reinforced_plating, optics, capacitor,
    -- cooling_system, mobility_tuning, shield_reservoir
  utilities = { "..." },        -- max 2:
    -- cloak_emitter 15, mine_dispenser 10,
    -- smoke_projector 10, repair_field 15
}  -- total must fit the 60-point budget`;

  const envelope = `// protocol/worker.proto -- one Rust worker per running match,
// private mode-0600 Unix socket, 20 Hz simulation, 10 Hz decisions.
message Envelope {
  uint32 version = 1;   // always 4
  string kind = 2;      // start | step | checkpoint | previewEdit | edit | restore
  bytes payload = 3;    // versioned UTF-8 JSON shared with browser + Lua
}
// Each frame is length-prefixed with a 4-byte big-endian size.
// Cap is 16 MiB; recording failure fails the match instead of
// silently losing replay inputs.`;
</script>

<section class="docs-page">
  <aside>
    <div class="eyebrow">PROTOCOL + SDK // v4</div>
    <h1>Documentation</h1>
    <p>One Rust engine, one Lua SDK, one protocol. Reference for Arena V2 scripts, matches, and replay inspection.</p>
    <div class="doc-tabs" role="tablist">
      <button role="tab" aria-selected={tab==='sdk'} class:active={tab==='sdk'} onclick={() => tab='sdk'}>Lua SDK 0.4</button>
      <button role="tab" aria-selected={tab==='api'} class:active={tab==='api'} onclick={() => tab='api'}>Protocol v4</button>
    </div>
    {#if tab==='sdk'}
      <a href="#/docs/sdk" onclick={(event) => jumpToSection(event, 'sdk-quickstart')}>Quickstart</a>
      <a href="#/docs/sdk" onclick={(event) => jumpToSection(event, 'sdk-observation')}>Observation schema</a>
      <a href="#/docs/sdk" onclick={(event) => jumpToSection(event, 'sdk-actions')}>Actions + expiry</a>
      <a href="#/docs/sdk" onclick={(event) => jumpToSection(event, 'sdk-helpers')}>Helper reference</a>
      <a href="#/docs/sdk" onclick={(event) => jumpToSection(event, 'sdk-strategies')}>Strategy pack</a>
      <a href="#/docs/sdk" onclick={(event) => jumpToSection(event, 'sdk-errors')}>Rejections + debugging</a>
      <a href="#/docs/sdk" onclick={(event) => jumpToSection(event, 'sdk-flow')}>Ship flow</a>
    {:else}
      <a href="#/docs/api" onclick={(event) => jumpToSection(event, 'api-overview')}>Overview</a>
      <a href="#/docs/api" onclick={(event) => jumpToSection(event, 'api-rest')}>REST endpoints</a>
      <a href="#/docs/api" onclick={(event) => jumpToSection(event, 'api-config')}>Match config + loadouts</a>
      <a href="#/docs/api" onclick={(event) => jumpToSection(event, 'api-viewer')}>Viewer socket</a>
      <a href="#/docs/api" onclick={(event) => jumpToSection(event, 'api-agent')}>Agent socket</a>
      <a href="#/docs/api" onclick={(event) => jumpToSection(event, 'api-worker')}>Worker envelope</a>
      <a href="#/docs/api" onclick={(event) => jumpToSection(event, 'api-replay')}>Replays + traces</a>
      <a href="#/docs/api" onclick={(event) => jumpToSection(event, 'api-errors')}>Rejection codes</a>
    {/if}
  </aside>
  <article>
    {#if tab==='sdk'}
      <section id="sdk-quickstart" tabindex="-1"><div class="eyebrow">SDK 0.4 // QUICKSTART</div><h2>Continuous agent loop</h2><p>Save as <code>main.lua</code> in your box workspace. Start with <code>lua main.lua</code>. <code>ROBOT_ARENA_URL</code> and <code>ROBOT_TOKEN</code> arrive as environment variables — never hardcode them. Persistent Lua locals survive decision calls, so keep route state, stuck trackers, and last-seen contacts in module scope. Simulation runs at 20 Hz; your <code>decide</code> is called at 10 Hz.</p><pre><code>{quickstart}</code></pre><p>Coordinates are world units (default large map is 42,000 × 26,250). Headings are degrees. Inventory and equipment slots are <strong>zero-based</strong>. Ticks advance at 20 Hz.</p></section>

      <section id="sdk-observation" tabindex="-1"><div class="eyebrow">SDK 0.4 // OBSERVATION</div><h2>What your robot sees</h2><p>Observations are filtered: enemies, loot, projectiles, and mines outside your vision range or behind cover are withheld. Scan contacts are coarse and omit health and inventory — save last-seen contacts in your own memory. Dead robots cannot sense enemies. In squads, an eliminated robot spectates a living teammate but returned actions still drive its own body. Terrain, sites, transit, hazards, and the zone are public.</p><pre><code>{observation}</code></pre><p><code>revision</code> bumps whenever live map edits apply — drop cached paths when it changes. <code>spectating</code> is true once your robot is eliminated.</p></section>

      <section id="sdk-actions" tabindex="-1"><div class="eyebrow">SDK 0.4 // ACTIONS</div><h2>Intent only — the server owns state</h2><p>Return <code>arena.control(...)</code>. The server clamps movement and is authoritative for position, damage, score, items, and match state. Continuous fields (<code>throttle, brake, turn, aim, fire, cruise, label</code>) expire after 250 ms. Discrete one-shots (<code>dash, scan, weapon, utility, consume, pickup, equip, dropEquipment, transit</code>) run once per accepted sequence. The SDK supplies <code>sequence</code> and <code>observedTick</code>; identity comes from the authenticated socket and cannot be spoofed.</p><pre><code>{actionEnvelope}</code></pre><div class="function-grid"><div><code>throttle −1..1 · brake</code><p>Negative reverses at half speed. <code>cruise</code> disables firing for fast travel.</p></div><div><code>turn −1..1 · aim</code><p>Body turn rate plus absolute turret heading in degrees.</p></div><div><code>fire · dash · scan</code><p>Fire is continuous; dash/scan are energy- and cooldown-gated one-shots.</p></div><div><code>weapon 0..1 · utility</code><p>Heat and cooldown persist while a weapon is holstered.</p></div><div><code>consume 0..3 · dropSlot</code><p>Healing channels cancel on movement, firing, or damage.</p></div><div><code>pickup · pickupPriorities ≤32</code><p>Nearby auto-pickup list, highest first. Empty array disables it. Never auto-replaces equipment.</p></div><div><code>equip &#123;container, kind, slot&#125;</code><p>Explicit slot replacement; the old item stays in that container. Dropping the last combat weapon is rejected.</p></div><div><code>transit · message ≤128B · label ≤64B</code><p>Transit needs 2 s stationary + 10 s cooldown. Messages are bounded squad reports.</p></div></div></section>

      <section id="sdk-helpers" tabindex="-1"><div class="eyebrow">SDK 0.4 // HELPERS</div><h2>Helper reference</h2><div class="function-grid"><div><code>arena.drive_to(obs, point, &#123;aim, fire, label&#125;)</code><p>Blocked-path-aware steering with collision-checked detours. Core movement primitive.</p></div><div><code>arena.control(options)</code><p>Raw intent constructor. Prefer <code>drive_to</code> for travel.</p></div><div><code>arena.weapon_ready(obs, slot?)</code><p>True when heat/cooldown allow firing now.</p></div><div><code>arena.nearest_enemy(obs)</code><p>Nearest visible opponent + distance. Invisible opponents are not updated.</p></div><div><code>arena.find_consumable(obs, kind)</code><p>Find a consumable in your inventory and return its zero-based slot. Use find_loot to search nearby containers.</p></div><div><code>arena.nearest_item(obs)</code><p>Nearest visible container + distance, any contents.</p></div><div><code>arena.find_loot(obs, kind?)</code><p>Nearest container holding a content kind (weapon:railgun, shield_cell…). find_consumable instead reads your inventory and returns a consume slot.</p></div><div><code>arena.nearest_ally(obs)</code><p>Nearest living teammate for support trailing.</p></div><div><code>arena.lead_for(obs, enemy, speed?)</code><p>Aim point with heading-estimated drift. Pass 0 for hitscan railgun shots.</p></div><div><code>arena.strafe_around(obs, enemy, dir)</code><p>Orbit at current distance with turret tracking. Flip dir on a timer.</p></div><div><code>arena.dodge(obs) · danger_level(obs)</code><p>Perpendicular escape from the most imminent projectile; threat count.</p></div><div><code>arena.scan_contacts(obs)</code><p>Coarse contacts from your last scan event.</p></div><div><code>arena.distance(a,b) · bearing(a,b)</code><p>Euclidean distance and degrees bearing between points.</p></div><div><code>arena.in_vision(obs,x,y) · can_see(obs,robot) · visible_enemies(obs)</code><p>Vision-range and line-of-sight checks against your own body.</p></div><div><code>arena.line_of_sight(x1,y1,x2,y2,obstacles)</code><p>Raw segment-vs-cover test on public geometry. Uses a cached spatial index on dense maps.</p></div><div><code>arena.raycast(x,y,heading,max,obstacles?)</code><p>Distance to the first obstacle along a heading, plus the obstacle hit.</p></div><div><code>arena.obstacles_near(obs,x,y,radius)</code><p>Cover pieces within a radius, nearest first.</p></div><div><code>arena.find_cover(obs, threat, &#123;radius, clearance&#125;)</code><p>Nearest clear spot hidden from a threat behind nearby cover, or nil.</p></div><div><code>arena.contact_tracker(ttl) · update_contacts(tracker, obs)</code><p>Remember enemies after they leave sight: age, last seen, extrapolated position.</p></div><div><code>arena.best_target(obs, &#123;range, hpWeight&#125;)</code><p>Visible enemy in range with line of sight, favouring near and weak targets.</p></div><div><code>arena.nearest_site(obs, &#123;kind, biome&#125;)</code><p>Closest site, optionally filtered by kind or district biome.</p></div><div><code>arena.tactics(&#123;name, range, preferred, scan, pickup, on_enemy, idle, decorate&#125;)</code><p>Complete decision loop used by every shipped strategy: unstick, fight, rotate, heal, loot, hunt, patrol. Customise with options and hooks.</p></div><div><code>arena.engage(obs, enemy, memory, &#123;preferred&#125;)</code><p>Close in, strafe inside the range band, or kite back while firing with lead.</p></div><div><code>arena.patrol(obs, memory)</code><p>Deterministic site-to-site goals inside the safe zone.</p></div><div><code>arena.unstick(obs, memory) · note_action(memory, action)</code><p>Detects driving without progress and backs out of cover.</p></div><div><code>arena.begin_path(obs,goal) · advance_path(route,64) · follow_path(obs,route)</code><p>Bounded local search (128-expansion budget). Status can be pending or unreachable — replan or stop, never drive into cover.</p></div><div><code>arena.stuck_tracker() · is_stuck(obs,memory)</code><p>Detect wedged movement and reset routes when the revision changes.</p></div><div><code>arena.safe_zone_goal(obs)</code><p>A point inside the current zone phase. Zone outranks loot.</p></div><div><code>arena.transit_route(obs, goal)</code><p>Returns &#123;entry, goal&#125; — ride with transit = entry.id. 2 s stationary, 10 s cooldown.</p></div><div><code>arena.projectiles_near(obs,r) · mines_near(obs,r) · danger_level(obs) · dodge(obs)</code><p>Incoming-fire awareness and sidestep helpers.</p></div><div><code>arena.zone_status(obs) · hazard_at(obs,x,y) · teammates(obs)</code><p>Zone phase, hazard overlap, and allied roster.</p></div></div><p>Search work is bounded per call and uses public geometry plus observed entities — never hidden engine state.</p></section>

      <section id="sdk-strategies" tabindex="-1"><div class="eyebrow">SDK 0.4 // STRATEGY PACK</div><h2>Seven deployable starting points</h2><p>Every file in <code>examples/lua-v4/</code> exactly matches its box-console template and is a short <code>arena.tactics</code> configuration. Deploy one, then edit toward your own tactic.</p><div class="function-grid"><div><code>balanced (v4 / main.lua)</code><p>Engage, retreat-dash at low HP, heal, scavenge, bounded navigation.</p></div><div><code>scout</code><p>Transit-site recon, scan pulses, squad relays, breaks contact under 220 units.</p></div><div><code>assault</code><p>Mid-range brawler with retreat logic and loot scavenging.</p></div><div><code>sniper</code><p>Railgun control at 600–900 units; brakes to aim, cloaks to re-range.</p></div><div><code>support</code><p>Trails allies, drops repair fields under 70 HP, screens with smoke.</p></div><div><code>sentinel</code><p>Safe-zone denial with shield priority, mines, and scan sweeps.</p></div><div><code>scavenger</code><p>Loot-first winner: pickup priorities, explicit equips, transit rides.</p></div></div></section>

      <section id="sdk-errors" tabindex="-1"><div class="eyebrow">SDK 0.4 // ERRORS</div><h2>Rejections + debugging</h2><p>Gameplay rejections arrive in <code>self.actionResults</code>; transport rejections close or warn on the socket. A <code>decide</code> Lua error never kills the connection — the server submits a safe no-op and surfaces <code>script error</code> in telemetry.</p><div class="function-grid"><div><code>MALFORMED_ACTION</code><p>Shape or type violation. Check field ranges.</p></div><div><code>UNSUPPORTED_VERSION</code><p>Missing SDK 0.4.0 header or wrong envelope version.</p></div><div><code>OUT_OF_RANGE</code><p>Numeric field outside its clamp (throttle, slots, message bytes…).</p></div><div><code>DUPLICATE_SEQUENCE</code><p>Sequence already accepted; resend with a fresh one.</p></div><div><code>FUTURE_OBSERVATION</code><p>observedTick ahead of the server tick.</p></div><div><code>STALE_OBSERVATION</code><p>observedTick too old; decide on fresher state.</p></div></div></section>

      <section id="sdk-flow" tabindex="-1"><div class="eyebrow">SDK 0.4 // SHIP FLOW</div><h2>From workspace to arena</h2><p><a href="#/workspace">Open the coding workspace →</a></p><p><strong>1.</strong> Provision your SSH box and open the Code panel. <strong>2.</strong> Deploy a strategy template, then Load, edit, Check syntax, Save (each save writes an immutable version). <strong>3.</strong> Pick a legal 60-point build and Register in a lobby — registration freezes the main script for that match; later edits prepare the next run. <strong>4.</strong> Start after all human agents connect. Agents have a 30-second reconnect grace. <strong>5.</strong> Watch your private observation view live; public replays and owner-only traces unlock after the match.</p></section>
    {:else}
      <section id="api-overview" tabindex="-1"><div class="eyebrow">PROTOCOL v4 // OVERVIEW</div><h2>One control plane, one worker per match</h2><p>Go owns HTTP, WebSockets, provisioning, storage, and deployment. Each running v4 match spawns a separate Rust worker on a private Unix socket. The browser never talks to the worker directly; robot agents never touch shared state — they return intent only. WebSocket subprotocol is <code>robot-arena.v4</code> with header <code>X-Robot-SDK-Version: 0.4.0</code>. Public reads are anonymous; queueing, ownership, settings, and admin data require sign-in.</p><pre><code>{envelope}</code></pre></section>

      <section id="api-rest" tabindex="-1"><div class="eyebrow">PROTOCOL v4 // REST</div><h2>Endpoints</h2><div class="function-grid"><div><code>POST /api/v4/matches</code><p>Create an unranked lobby (auth). Empty slots become server bots at start.</p></div><div><code>POST /api/v4/maps/preview</code><p>Render a lobby config's deterministic geometry — sites, cover, loot, hazards — without starting a match (auth). Powers the instant lobby preview.</p></div><div><code>POST /api/matches/&#123;id&#125;/robots</code><p>Register immutable script + 60-point loadout with <code>sdkVersion 0.4.0</code> (auth).</p></div><div><code>POST /api/matches/&#123;id&#125;/start</code><p>Owner starts the registered roster (auth).</p></div><div><code>POST /api/v4/matches/&#123;id&#125;/control</code><p>Sandbox <code>&#123;"command":"pause|resume|step"&#125;</code> (owner).</p></div><div><code>GET /api/v4/matches/&#123;id&#125;/view</code><p>Owner-only live observation while running (auth).</p></div><div><code>GET /api/v4/matches/&#123;id&#125;/final</code><p>Completed public final state (anonymous).</p></div><div><code>GET /api/v4/matches/&#123;id&#125;/replay?page=N</code><p>Completed public frames; 100 ticks per page, ≤10 frames (anonymous).</p></div><div><code>GET /api/v4/matches/&#123;id&#125;/trace?tick=N</code><p>Owner-only observation + accepted action + verified state hash.</p></div><div><code>POST /api/v4/matches/&#123;id&#125;/edit?apply=bool</code><p>Admin live-edit: <code>expectedRevision + effectiveTick</code> + obstacle/container/transit upserts. Preview first, then schedule.</p></div><div><code>GET/PUT /api/v4/me/script</code><p>Workspace source + SHA-256 revision; saves persist a version.</p></div><div><code>POST /api/v4/me/script/validate</code><p>Lua syntax check in the box.</p></div><div><code>GET/PUT /api/v4/me/loadout</code><p>Account default 60-point build.</p></div><div><code>GET /api/scripts</code><p>Strategy-pack templates (anonymous).</p></div><div><code>GET /healthz · /readyz · /api/cloud/status</code><p>Liveness, readiness, and Floci/S3 status (anonymous).</p></div></div></section>

      <section id="api-config" tabindex="-1"><div class="eyebrow">PROTOCOL v4 // CONFIG</div><h2>Match config + loadouts</h2><p>Every random system is seeded from match config — never wall-clock time inside engine rules. Map scale multiplies dimensions, not obstacle sizes.</p><pre><code>{configSchema}</code></pre><pre><code>{loadoutSchema}</code></pre></section>

      <section id="api-viewer" tabindex="-1"><div class="eyebrow">PROTOCOL v4 // VIEWER SOCKET</div><h2>Delayed, regional spectating</h2><p><code>GET /ws/matches/&#123;id&#125;</code> is anonymous. Public snapshots lag simulation by 100 ticks (5 s at 20 Hz); pausing a sandbox pauses the delay. Send camera regions <code>&#123;x, y, width, height&#125;</code> in world units — entities inside the rectangle (+256 margin) arrive at 10 Hz, the full roster/minimap overview at 1 Hz, and public obstacles/hazards/transit on first delivery or revision change. Invalid camera messages close the connection. Reconnects use exponential backoff and request a fresh baseline after hidden tabs.</p></section>

      <section id="api-agent" tabindex="-1"><div class="eyebrow">PROTOCOL v4 // AGENT SOCKET</div><h2>Authenticated robot channel</h2><p>Agents connect through the credential-protected URL with the v4 subprotocol and SDK header. Each action carries <code>type, version: 4, sdkVersion, sequence, observedTick, action</code>. The connection selects the robot — payloads cannot assign identity. Mailboxes are bounded; malformed, duplicate, future, and stale inputs are rejected with codes. Continuous intent expires after 250 ms; discrete actions run once. A lost socket has a 30-second reconnect grace before withdrawal.</p></section>

      <section id="api-worker" tabindex="-1"><div class="eyebrow">PROTOCOL v4 // WORKER</div><h2>Go ↔ Rust calls</h2><div class="function-grid"><div><code>start(config)</code><p>Spawn worker, generate the deterministic world, return geometry + snapshot + observations.</p></div><div><code>step(&#123;actions, withdrawals&#125;)</code><p>Advance one tick; returns the next snapshot, observations, and state hash.</p></div><div><code>checkpoint()</code><p>Full serializable state every 200 ticks for replay restore.</p></div><div><code>previewEdit / edit</code><p>Validate, then atomically apply a revision-checked map transaction at its effective tick.</p></div><div><code>restore(checkpoint)</code><p>Rebuild a fresh worker for private trace reconstruction.</p></div></div></section>

      <section id="api-replay" tabindex="-1"><div class="eyebrow">PROTOCOL v4 // REPLAY</div><h2>Frames, inputs, checkpoints</h2><p>Completed frames persist at 2 Hz, accepted inputs every 100 ticks, checkpoints every 200 ticks, each with deterministic state hashes. Storage writes use a bounded background queue — a recording failure fails the match rather than silently losing inputs. Restore requires matching engine version and architecture.</p></section>

      <section id="api-errors" tabindex="-1"><div class="eyebrow">PROTOCOL v4 // ERRORS</div><h2>Rejection codes</h2><div class="function-grid"><div><code>MALFORMED_ACTION</code><p>Unparseable envelope or action shape.</p></div><div><code>UNSUPPORTED_VERSION</code><p>Wrong protocol or SDK version.</p></div><div><code>OUT_OF_RANGE</code><p>Field outside its validated range.</p></div><div><code>DUPLICATE_SEQUENCE</code><p>Sequence already processed.</p></div><div><code>FUTURE_OBSERVATION</code><p>observedTick ahead of server tick.</p></div><div><code>STALE_OBSERVATION</code><p>observedTick expired.</p></div><div><code>CONTAINER_BUDGET_EXCEEDED</code><p>Chunk loot cap hit; drop rejected, inventory preserved.</p></div><div><code>PATH_BLOCKED</code><p>Local search found no route; stop or replan.</p></div></div></section>
    {/if}
  </article>
</section>

<style>
  .doc-tabs { display: grid; grid-template-columns: 1fr 1fr; gap: 4px; margin: 18px 0 22px; }
  .doc-tabs button { padding: 10px 6px; background: var(--panel); border: 1px solid var(--line); color: var(--ink); cursor: pointer; font: 10px 'DM Mono'; }
  .doc-tabs button.active, .doc-tabs button[aria-selected='true'] { border-color: var(--acid); color: var(--acid); }
  .docs-page section { scroll-margin-top: 100px; }
  .docs-page section:focus { outline: none; }
</style>
