use crate::{catalog, model::*, world::*};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use std::collections::BTreeMap;

#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Arena {
    pub config: Config,
    pub tick: u32,
    pub world: World,
    pub robots: Vec<Robot>,
    pub projectiles: Vec<Projectile>,
    pub mines: Vec<Mine>,
    pub fields: Vec<Field>,
    pub finished: bool,
    pub winner_team: String,
    pub events: Vec<Event>,
    pub recent_events: Vec<Event>,
    pub next_id: u64,
    pub bot_intents: BTreeMap<String, Action>,
    #[serde(default)]
    pub bot_memory: BTreeMap<String, BotMemory>,
    /// Arena site loot as generated, restocked every 30 seconds.
    #[serde(default)]
    pub restock: Vec<Container>,
    #[serde(skip)]
    pub robot_grid: Grid,
}
#[derive(Clone)]
struct Hit {
    source: String,
    target: usize,
    damage: f64,
    kind: String,
    bypass: bool,
}

/// Per-bot navigation memory. Bots decide every 2 ticks with no other state,
/// so without this a blocked route replays forever and the robot looks frozen.
/// All wander targets derive from tick + index, never wall-clock time.
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", default)]
pub struct BotMemory {
    pub last_x: f64,
    pub last_y: f64,
    pub stuck_ticks: u32,
    pub driving: bool,
    pub wander_x: f64,
    pub wander_y: f64,
    pub wander_ticks: u32,
    pub orbit: f64,
    /// Patrol goal used when no enemy is visible, so idle bots spread across
    /// sites instead of circling the zone centre together.
    pub roam_x: f64,
    pub roam_y: f64,
    pub roam_ticks: u32,
    pub roam_count: u32,
    /// Distance the last decision's throttle should cover in two ticks.
    pub expected: f64,
    /// Coarse enemy position from this bot's own last scan, valid until
    /// `hunt_until`. Scans are a player mechanic too, so hunting stays fair.
    pub hunt_x: f64,
    pub hunt_y: f64,
    pub hunt_until: u32,
}
/// The four server-bot builds, cycled by index.
pub(crate) fn bot_loadout(index: usize) -> catalog::Loadout {
    match index % 4 {
        0 => catalog::Loadout {
            chassis: "scout".into(),
            weapon: "machine_gun".into(),
            modules: vec!["optics".into(), "mobility_tuning".into()],
            utilities: vec!["cloak_emitter".into()],
        },
        1 => catalog::Loadout {
            chassis: "generalist".into(),
            weapon: "railgun".into(),
            modules: vec!["optics".into(), "cooling_system".into()],
            utilities: vec![],
        },
        2 => catalog::Loadout {
            chassis: "generalist".into(),
            weapon: "plasma".into(),
            modules: vec!["capacitor".into()],
            utilities: vec!["repair_field".into(), "smoke_projector".into()],
        },
        _ => catalog::Loadout {
            chassis: "generalist".into(),
            weapon: "machine_gun".into(),
            modules: vec!["reinforced_plating".into(), "cooling_system".into()],
            utilities: vec!["mine_dispenser".into()],
        },
    }
}

/// A robot at full health with its registered loadout, standing at (x, y).
pub(crate) fn fresh_robot(config: &Config, reg: &Registration, x: f64, y: f64) -> Robot {
    let c = catalog::chassis(&reg.loadout.chassis).unwrap();
    let mut charges = BTreeMap::new();
    for u in &reg.loadout.utilities {
        charges.insert(
            u.clone(),
            match u.as_str() {
                "mine_dispenser" => 3,
                "smoke_projector" => 2,
                _ => 0,
            },
        );
    }
    let has = |m: &str| reg.loadout.modules.iter().any(|s| s == m);
    let hp = c.hp + if has("reinforced_plating") { 20. } else { 0. };
    let energy = if has("capacitor") { 130. } else { 100. };
    Robot {
        robot_id: reg.robot_id.clone(),
        name: reg.name.clone(),
        team: if config.mode == "br-squad" {
            reg.team.clone()
        } else {
            reg.robot_id.clone()
        },
        bot: reg.bot,
        x,
        y,
        heading: 0.,
        turret_heading: 0.,
        vx: 0.,
        vy: 0.,
        speed: 0.,
        hp,
        max_hp: hp,
        shield: 0.,
        max_shield: if has("shield_reservoir") { 75. } else { 50. },
        energy,
        max_energy: energy,
        vision_range: if has("optics") { 900. } else { 600. },
        alive: true,
        loadout: reg.loadout.clone(),
        weapons: vec![WeaponSlot {
            kind: reg.loadout.weapon.clone(),
            ..Default::default()
        }],
        active_weapon: 0,
        inventory: vec![],
        pickup_priorities: vec![],
        charges,
        cooldowns: BTreeMap::new(),
        burn_until: 0,
        slow_until: 0,
        emp_until: 0,
        cloak_until: 0,
        cruise: false,
        channel: None,
        damage_dealt: 0.,
        damage_taken: 0.,
        kills: 0,
        placement: None,
        messages: vec![],
        action_results: vec![],
        last_action: String::new(),
        last_damage_tick: None,
        transit_channel: None,
        deaths: 0,
        respawn_at: None,
        left: false,
        score: 0,
        streak: 0,
        protected_until: 0,
    }
}

/// First clear spot on a golden-angle spiral around `site`, away from other
/// live robots. Deterministic for the same inputs.
pub(crate) fn find_spawn(
    config: &Config,
    world: &World,
    robots: &[Robot],
    site: (f64, f64),
    angle: f64,
) -> Option<(f64, f64)> {
    for attempt in 0..512 {
        let direction = angle + attempt as f64 * 2.399963229728653;
        let radius = 170. + (attempt / 8) as f64 * 30.;
        let x = (site.0 + direction.cos() * radius).clamp(RADIUS, config.width - RADIUS);
        let y = (site.1 + direction.sin() * radius).clamp(RADIUS, config.height - RADIUS);
        if world.clear(x, y, RADIUS)
            && !robots
                .iter()
                .any(|v| v.alive && distance(v.x, v.y, x, y) < RADIUS * 2. + 4.)
        {
            return Some((x, y));
        }
    }
    None
}

/// Arena respawn delay: five seconds at 20 Hz.
pub const ARENA_RESPAWN_TICKS: u32 = 100;
/// Arena scoring and pacing. The Uplink objective moves to another site
/// every minute; holding it alone scores every second.
pub const KILL_POINTS: u32 = 100;
pub const BOUNTY_STREAK: u32 = 3;
pub const BOUNTY_PER_KILL: u32 = 50;
pub const HILL_TICKS: u32 = 1200;
pub const HILL_RADIUS: f64 = 260.;
pub const HILL_POINTS: u32 = 5;
pub const SPAWN_PROTECT_TICKS: u32 = 40;
pub const SALVAGE_TICKS: u32 = 600;
pub const RESTOCK_TICKS: u32 = 600;

impl Arena {
    pub fn new(mut config: Config) -> Result<Self, String> {
        config.validate()?;
        let mut teams: BTreeMap<String, usize> = BTreeMap::new();
        for r in &config.robots {
            *teams.entry(r.team.clone()).or_default() += 1;
        }
        let mut bot_id = 0;
        let target = config.roster_target();
        let squad_size = config.squad_size();
        while config.robots.len() < target {
            let id = loop {
                let candidate = format!("bot-{bot_id:03}");
                bot_id += 1;
                if !config.robots.iter().any(|r| r.robot_id == candidate) {
                    break candidate;
                }
            };
            let team = if config.mode == "br-squad" {
                let available = teams
                    .iter()
                    .find(|(_, n)| **n < squad_size)
                    .map(|(name, _)| name.clone());
                let team = available.unwrap_or_else(|| {
                    let mut n = 0;
                    loop {
                        let key = format!("squad-{n:02}");
                        if !teams.contains_key(&key) {
                            break key;
                        }
                        n += 1;
                    }
                });
                *teams.entry(team.clone()).or_default() += 1;
                team
            } else {
                String::new()
            };
            config.robots.push(Registration {
                robot_id: id.clone(),
                name: id,
                team,
                bot: true,
                loadout: bot_loadout(bot_id),
            });
        }
        config.robots.sort_by(|a, b| a.robot_id.cmp(&b.robot_id));
        config.validate()?;
        let world = World::generate(&config);
        let mut robots: Vec<Robot> = vec![];
        let team_order: Vec<_> = teams.keys().cloned().collect();
        let mut team_slots: BTreeMap<String, usize> = BTreeMap::new();
        for (i, reg) in config.robots.iter().enumerate() {
            let site_index = if config.mode == "br-squad" {
                team_order.iter().position(|t| t == &reg.team).unwrap()
            } else {
                i % world.sites.len()
            };
            let site = &world.sites[site_index % world.sites.len()];
            let layer = if config.mode == "br-squad" {
                let n = team_slots.entry(reg.team.clone()).or_default();
                let slot = *n;
                *n += 1;
                slot
            } else {
                i / world.sites.len()
            };
            let angle = layer as f64 * 2.399963229728653 + 0.7;
            let (x, y) = find_spawn(&config, &world, &robots, (site.x, site.y), angle)
                .ok_or("no clear spawn available for capacity")?;
            let mut r = fresh_robot(&config, reg, x, y);
            r.heading = heading(r.x, r.y, site.x, site.y);
            r.turret_heading = r.heading;
            robots.push(r);
        }
        let mut a = Self {
            config,
            tick: 0,
            world,
            robots,
            projectiles: vec![],
            mines: vec![],
            fields: vec![],
            finished: false,
            winner_team: String::new(),
            events: vec![],
            recent_events: vec![],
            next_id: 0,
            bot_intents: BTreeMap::new(),
            bot_memory: BTreeMap::new(),
            restock: vec![],
            robot_grid: Grid::default(),
        };
        if a.config.mode == "arena" {
            a.restock = a.world.containers.clone();
        }
        a.reindex();
        Ok(a)
    }
    pub fn reindex(&mut self) {
        self.world.reindex();
        self.reindex_robots();
    }
    /// Static geometry changes only through edits and restores, which call
    /// `reindex`; each simulation step only moves robots.
    fn reindex_robots(&mut self) {
        self.robot_grid = Grid::default();
        for (i, r) in self.robots.iter().enumerate() {
            if r.alive {
                self.robot_grid
                    .insert(i, r.x - RADIUS, r.y - RADIUS, RADIUS * 2., RADIUS * 2.);
            }
        }
    }
    pub(crate) fn id(&mut self, prefix: &str) -> String {
        self.next_id += 1;
        format!("{prefix}-{}", self.next_id)
    }
    fn event(
        &mut self,
        kind: &str,
        id: Option<String>,
        target: Option<String>,
        damage: Option<f64>,
        message: Option<String>,
    ) {
        self.events.push(Event {
            tick: self.tick,
            r#type: kind.into(),
            robot_id: id,
            target_id: target,
            x: None,
            y: None,
            damage,
            message,
        });
    }
    /// Arena housekeeping at the start of a tick: drop robots whose players
    /// left, and respawn destroyed robots whose timer has run out.
    fn arena_upkeep(&mut self) {
        let before = self.robots.len();
        let gone: Vec<String> = self
            .robots
            .iter()
            .filter(|r| r.left && !r.alive)
            .map(|r| r.robot_id.clone())
            .collect();
        if !gone.is_empty() {
            self.robots.retain(|r| !(r.left && !r.alive));
            for id in &gone {
                self.bot_intents.remove(id);
                self.bot_memory.remove(id);
            }
        }
        for i in 0..self.robots.len() {
            let due = self.robots[i].respawn_at.is_some_and(|t| t <= self.tick);
            if !due || self.robots[i].alive {
                continue;
            }
            let reg = Registration {
                robot_id: self.robots[i].robot_id.clone(),
                name: self.robots[i].name.clone(),
                team: self.robots[i].team.clone(),
                bot: self.robots[i].bot,
                loadout: self.robots[i].loadout.clone(),
            };
            if let Some((x, y)) = self.arena_spawn(i as u64) {
                let old = &self.robots[i];
                let mut fresh = fresh_robot(&self.config, &reg, x, y);
                fresh.team = old.team.clone();
                fresh.kills = old.kills;
                fresh.deaths = old.deaths;
                fresh.damage_dealt = old.damage_dealt;
                fresh.damage_taken = old.damage_taken;
                fresh.score = old.score;
                fresh.messages = old.messages.clone();
                fresh.protected_until = self.tick + SPAWN_PROTECT_TICKS;
                self.robots[i] = fresh;
                let id = self.robots[i].robot_id.clone();
                self.event("robot_respawned", Some(id), None, None, None);
            }
        }
        // Keep a full arena: when players leave and bots fill empty slots,
        // new bots take their place.
        if self.config.bots.is_none() {
            let mut live = self.robots.iter().filter(|r| !r.left).count();
            let mut n = 0u64;
            while live < self.config.capacity {
                let id = format!("bot-r{}-{}", self.tick, n);
                let reg = Registration {
                    robot_id: id.clone(),
                    name: format!("Game Bot {}", self.robots.len() + 1),
                    team: String::new(),
                    bot: true,
                    loadout: bot_loadout(self.tick as usize + n as usize),
                };
                match self.arena_spawn(1000 + n) {
                    Some((x, y)) => self.robots.push(fresh_robot(&self.config, &reg, x, y)),
                    None => break,
                }
                live += 1;
                n += 1;
            }
        }
        if self.robots.len() != before || !self.robots.is_empty() {
            self.reindex_robots();
        }
    }

    /// A clear spawn at a site chosen from the tick and a salt, so arrivals
    /// spread across the map deterministically.
    fn arena_spawn(&self, salt: u64) -> Option<(f64, f64)> {
        if self.world.sites.is_empty() {
            return None;
        }
        let pick = (self.tick as u64)
            .wrapping_mul(0x9E37_79B9)
            .wrapping_add(salt.wrapping_mul(7919));
        let site = &self.world.sites[(pick % self.world.sites.len() as u64) as usize];
        find_spawn(
            &self.config,
            &self.world,
            &self.robots,
            (site.x, site.y),
            (pick % 360) as f64,
        )
    }

    /// Expiry for dropped containers: arena salvage fades, others persist.
    pub(crate) fn salvage_expiry(&self) -> Option<u32> {
        (self.config.mode == "arena").then_some(self.tick + SALVAGE_TICKS)
    }

    /// The site holding the Uplink this minute, chosen from the seed.
    pub fn hill_site(&self) -> Option<usize> {
        if self.config.mode != "arena" || self.world.sites.is_empty() {
            return None;
        }
        let round = u64::from(self.tick / HILL_TICKS);
        let pick = (self.config.seed ^ round.wrapping_mul(0x9E37_79B9_7F4A_7C15))
            .wrapping_mul(0xBF58_476D_1CE4_E5B9)
            >> 33;
        Some((pick % self.world.sites.len() as u64) as usize)
    }

    /// Uplink state for snapshots and observations: where it is, when it
    /// moves, and which team holds it (empty when nobody or contested).
    pub fn hill(&self) -> Value {
        let Some(i) = self.hill_site() else {
            return Value::Null;
        };
        let site = &self.world.sites[i];
        let inside = self
            .robots
            .iter()
            .any(|r| r.alive && distance(r.x, r.y, site.x, site.y) <= HILL_RADIUS);
        let teams: std::collections::BTreeSet<&str> = self
            .robots
            .iter()
            .filter(|r| r.alive && distance(r.x, r.y, site.x, site.y) <= HILL_RADIUS)
            .map(|r| r.team.as_str())
            .collect();
        let holder = if teams.len() == 1 {
            teams.into_iter().next().unwrap_or("")
        } else {
            ""
        };
        json!({"siteId": site.id, "x": site.x, "y": site.y, "radius": HILL_RADIUS,
            "movesAt": (self.tick / HILL_TICKS + 1) * HILL_TICKS, "holder": holder,
            "contested": inside && holder.is_empty(),
            "pointsPerSecond": HILL_POINTS})
    }

    /// Scores the Uplink once a second and restocks site loot.
    fn arena_objectives(&mut self) {
        if self.tick > 0
            && self.tick.is_multiple_of(HILL_TICKS)
            && let Some(i) = self.hill_site()
        {
            let site = self.world.sites[i].id.clone();
            self.event("hill_moved", None, None, None, Some(site));
        }
        if self.tick.is_multiple_of(20)
            && let Some(i) = self.hill_site()
        {
            let (sx, sy) = (self.world.sites[i].x, self.world.sites[i].y);
            let inside: Vec<usize> = (0..self.robots.len())
                .filter(|&j| {
                    let r = &self.robots[j];
                    r.alive && distance(r.x, r.y, sx, sy) <= HILL_RADIUS
                })
                .collect();
            let first = inside.first().map(|&j| self.robots[j].team.clone());
            if let Some(team) = first
                && inside.iter().all(|&j| self.robots[j].team == team)
            {
                for j in inside {
                    self.robots[j].score += HILL_POINTS;
                }
            }
        }
        self.world
            .containers
            .retain(|c| c.expires_at.is_none_or(|t| t > self.tick));
        if self.tick > 0 && self.tick.is_multiple_of(RESTOCK_TICKS) {
            for c in &self.restock {
                if self.world.containers.len() < MAX_CONTAINERS
                    && !self.world.containers.iter().any(|w| w.item_id == c.item_id)
                {
                    self.world.containers.push(c.clone());
                }
            }
        }
    }

    /// Records a join the arena could not honour, for the API to report.
    pub fn reject_join(&mut self, robot_id: String, reason: String) {
        self.event("join_rejected", Some(robot_id), None, None, Some(reason));
    }

    /// Arena mode: add a player's robot mid-match. When the arena is full a
    /// bot makes room; a full arena of players rejects the join.
    pub fn join(&mut self, reg: Registration) -> Result<(), String> {
        if self.config.mode != "arena" {
            return Err("only arena matches accept players mid-match".into());
        }
        if self.robots.iter().any(|r| r.robot_id == reg.robot_id) {
            return Err("robot already in the arena".into());
        }
        crate::catalog::validate(&reg.loadout)?;
        let live = self.robots.iter().filter(|r| !r.left).count();
        if live >= self.config.capacity {
            let bot = self
                .robots
                .iter()
                .enumerate()
                .filter(|(_, r)| r.bot)
                .min_by_key(|(_, r)| (r.alive, r.kills))
                .map(|(i, _)| i)
                .ok_or("the arena is full")?;
            let id = self.robots.remove(bot).robot_id;
            self.bot_intents.remove(&id);
            self.bot_memory.remove(&id);
        }
        let (x, y) = self
            .arena_spawn(self.robots.len() as u64 + 1)
            .ok_or("no clear spawn in the arena")?;
        let robot = fresh_robot(&self.config, &reg, x, y);
        self.robots.push(robot);
        self.config.robots.push(reg);
        self.reindex_robots();
        let id = self.robots.last().unwrap().robot_id.clone();
        self.event("robot_joined", Some(id), None, None, None);
        Ok(())
    }

    pub fn zone(&self) -> Zone {
        let mut zone = Zone {
            active: false,
            x: self.config.width / 2.,
            y: self.config.height / 2.,
            radius: self.config.width.hypot(self.config.height) / 2.,
            damage: 0.,
            stage: 0,
        };
        for (i, p) in self.world.zones.iter().enumerate() {
            if self.tick < p.start_tick {
                break;
            }
            let shrink_start = p.start_tick + p.hold_ticks;
            let amount = (self.tick.saturating_sub(shrink_start) as f64
                / (p.end_tick - shrink_start).max(1) as f64)
                .clamp(0., 1.);
            zone.x += (p.x - zone.x) * amount;
            zone.y += (p.y - zone.y) * amount;
            zone.radius += (p.radius - zone.radius) * amount;
            zone.damage = p.damage;
            zone.stage = i + 1;
            zone.active = self.config.mode != "sandbox" && self.config.mode != "arena";
            if self.tick < p.end_tick {
                break;
            }
        }
        zone
    }
    pub fn visible(&self, observer: usize, target: usize) -> bool {
        let a = &self.robots[observer];
        let b = &self.robots[target];
        observer == target
            || a.team == b.team
            || (a.alive
                && b.alive
                && b.cloak_until <= self.tick
                && distance(a.x, a.y, b.x, b.y) <= a.vision_range
                && self.world.los(a.x, a.y, b.x, b.y)
                && !self.fields.iter().any(|f| {
                    f.kind == "smoke"
                        && segment_circle(a.x, a.y, b.x, b.y, f.x, f.y, 100.).is_some()
                }))
    }
    /// Mirrors the pickup rule: whether a manual pickup would take `kind`.
    fn can_take(r: &Robot, kind: &str) -> bool {
        if let Some(w) = kind.strip_prefix("weapon:") {
            r.weapons.len() < 2 && !r.weapons.iter().any(|v| v.kind == w)
        } else if let Some(u) = kind.strip_prefix("utility:") {
            r.loadout.utilities.len() < 2 && !r.loadout.utilities.iter().any(|v| v == u)
        } else if catalog::MODULES.contains(&kind) {
            r.loadout.modules.len() < 2 && !r.has(kind)
        } else if catalog::CONSUMABLES.contains(&kind) {
            r.inventory.len() < 4 || r.inventory.iter().any(|s| s.kind == kind && s.count < 3)
        } else {
            false
        }
    }
    /// Deterministic patrol target: a site inside the safe zone chosen from
    /// the bot index and patrol count, offset so bots never share one point.
    /// Re-picked on arrival, after a time limit, or when the zone excludes it.
    fn roam_goal(&mut self, i: usize) -> (f64, f64) {
        let z = self.zone();
        let (rx, ry) = (self.robots[i].x, self.robots[i].y);
        let id = self.robots[i].robot_id.clone();
        let inside: Vec<(f64, f64)> = self
            .world
            .sites
            .iter()
            .filter(|s| distance(s.x, s.y, z.x, z.y) < z.radius * 0.85)
            .map(|s| (s.x, s.y))
            .collect();
        let hill = self
            .hill_site()
            .map(|h| (self.world.sites[h].x, self.world.sites[h].y));
        let mem = self.bot_memory.entry(id).or_default();
        let stale = mem.roam_ticks == 0
            || distance(rx, ry, mem.roam_x, mem.roam_y) < 150.
            || distance(mem.roam_x, mem.roam_y, z.x, z.y) > z.radius * 0.9;
        if stale {
            mem.roam_count += 1;
            // Hash so bots do not walk the same site order one step apart.
            let pick = (((i as u64) << 32 | mem.roam_count as u64)
                .wrapping_mul(0x9E37_79B9_7F4A_7C15)
                >> 33) as usize;
            let (sx, sy) = match hill {
                // Arena bots split between the Uplink and roaming.
                Some(h) if pick.is_multiple_of(2) => h,
                _ if inside.is_empty() => (z.x, z.y),
                _ => inside[pick % inside.len()],
            };
            let angle = pick as f64 * 2.399963229728653;
            let spread = 120. + (pick % 5) as f64 * 40.;
            mem.roam_x = (sx + angle.cos() * spread).clamp(RADIUS, self.config.width - RADIUS);
            mem.roam_y = (sy + angle.sin() * spread).clamp(RADIUS, self.config.height - RADIUS);
            mem.roam_ticks = 400;
        } else {
            mem.roam_ticks = mem.roam_ticks.saturating_sub(2);
        }
        (mem.roam_x, mem.roam_y)
    }
    fn bot_action(&mut self, i: usize) -> Action {
        // Stuck detection runs on the 2-tick decision cadence: a bot that was
        // told to drive but barely moved is grinding cover, not fighting.
        // Wander legs derive from tick + index (golden-angle spiral), never
        // wall-clock time, so wedged bots visibly break out deterministically.
        let (orbit, wander_goal) = {
            let (rid, rx, ry, alive) = {
                let r = &self.robots[i];
                (r.robot_id.clone(), r.x, r.y, r.alive)
            };
            let mem = self.bot_memory.entry(rid).or_insert_with(|| BotMemory {
                last_x: rx,
                last_y: ry,
                orbit: if i.is_multiple_of(2) { 1. } else { -1. },
                ..Default::default()
            });
            // Stuck means covering under a third of what the throttle asked
            // for; slow crawls through turns are not stuck.
            if mem.driving && alive && distance(rx, ry, mem.last_x, mem.last_y) < mem.expected / 3.
            {
                mem.stuck_ticks += 2;
            } else {
                mem.stuck_ticks = 0;
            }
            mem.last_x = rx;
            mem.last_y = ry;
            mem.driving = false;
            if mem.stuck_ticks >= 8 {
                let angle = self.tick as f64 * 0.05 + i as f64 * 2.399963229728653;
                mem.wander_x = (rx + angle.cos() * 500.).clamp(RADIUS, self.config.width - RADIUS);
                mem.wander_y = (ry + angle.sin() * 500.).clamp(RADIUS, self.config.height - RADIUS);
                mem.wander_ticks = 20;
                mem.stuck_ticks = 0;
                mem.orbit = -mem.orbit;
            }
            let goal = if mem.wander_ticks > 0 {
                mem.wander_ticks -= 1;
                if distance(rx, ry, mem.wander_x, mem.wander_y) < 120. {
                    mem.wander_ticks = 0;
                    None
                } else {
                    Some((mem.wander_x, mem.wander_y))
                }
            } else {
                None
            };
            (mem.orbit, goal)
        };
        // Patrol state advances every decision so the goal is ready when idle.
        let roam = self.roam_goal(i);
        let hunt = self.bot_memory.get(&self.robots[i].robot_id).and_then(|m| {
            let (rx, ry) = (self.robots[i].x, self.robots[i].y);
            (m.hunt_until > self.tick && distance(rx, ry, m.hunt_x, m.hunt_y) > 80.)
                .then_some((m.hunt_x, m.hunt_y))
        });
        let r = &self.robots[i];
        let mut a = Action::default();
        let nearest = self
            .robot_grid
            .query(
                r.x - r.vision_range,
                r.y - r.vision_range,
                r.vision_range * 2.,
                r.vision_range * 2.,
            )
            .into_iter()
            .filter(|j| self.robots[*j].team != r.team && self.visible(i, *j))
            .min_by(|j, k| {
                distance(r.x, r.y, self.robots[*j].x, self.robots[*j].y).total_cmp(&distance(
                    r.x,
                    r.y,
                    self.robots[*k].x,
                    self.robots[*k].y,
                ))
            });
        let (tx, ty) = if let Some(j) = nearest {
            let e = &self.robots[j];
            a.aim = Some(heading(r.x, r.y, e.x, e.y));
            a.fire = true;
            a.label = Some("ENGAGE".into());
            if r.hp < r.max_hp * 0.3 {
                a.dash = true;
                a.label = Some("RETREAT_LOW_HP".into());
                (r.x + (r.x - e.x), r.y + (r.y - e.y))
            } else {
                let d = distance(r.x, r.y, e.x, e.y);
                let preferred =
                    (catalog::weapon(r.weapon()).unwrap().range * 0.65).clamp(140., 650.);
                if d < preferred * 0.8 {
                    (r.x + (r.x - e.x), r.y + (r.y - e.y))
                } else {
                    if d < preferred * 1.15 {
                        (r.x + (e.y - r.y) * orbit, r.y - (e.x - r.x) * orbit)
                    } else {
                        // Dash down wounded or distant prey instead of jogging
                        // behind it forever.
                        if r.energy > 30. && (e.hp < e.max_hp * 0.6 || d > 400.) {
                            a.dash = true;
                        }
                        (e.x, e.y)
                    }
                }
            }
        } else {
            let z = self.zone();
            // Only chase containers the pickup rule would actually empty into
            // this robot; others keep bots hovering beside them forever.
            let loot = self
                .world
                .containers
                .iter()
                .filter(|c| {
                    c.contents
                        .iter()
                        .any(|stack| Self::can_take(r, &stack.kind))
                        && distance(c.x, c.y, r.x, r.y) <= r.vision_range
                        && self.world.los(r.x, r.y, c.x, c.y)
                        && distance(c.x, c.y, z.x, z.y) < z.radius
                })
                .min_by(|a, b| {
                    distance(r.x, r.y, a.x, a.y).total_cmp(&distance(r.x, r.y, b.x, b.y))
                });
            // Enemy fire is public: head toward the nearest hostile shot in
            // earshot instead of wandering away from the fight.
            let heard = self
                .projectiles
                .iter()
                .filter(|p| p.team != r.team && distance(p.x, p.y, r.x, r.y) < 1100.)
                .min_by(|a, b| {
                    distance(r.x, r.y, a.x, a.y).total_cmp(&distance(r.x, r.y, b.x, b.y))
                })
                .map(|p| {
                    let speed = p.vx.hypot(p.vy).max(1.);
                    (p.x - p.vx / speed * 300., p.y - p.vy / speed * 300.)
                });
            if let Some(c) = loot {
                a.label = Some("LOOT".into());
                if distance(r.x, r.y, c.x, c.y) < 35. {
                    a.pickup = Some(c.item_id.clone());
                }
                (c.x, c.y)
            } else if let Some(goal) = heard {
                a.label = Some("INVESTIGATE".into());
                goal
            } else if let Some(goal) = hunt {
                a.label = Some("HUNT".into());
                goal
            } else {
                a.label = Some("PATROL".into());
                roam
            }
        };
        let (mut tx, mut ty) = (tx, ty);
        // An escape leg outranks loot and zone strolls, but combat aim and
        // fire computed above are kept so the bot shoots while breaking out.
        if let Some((gx, gy)) = wander_goal {
            tx = gx;
            ty = gy;
            a.label = Some("UNSTUCK".into());
        }
        // Sidestep incoming fire: any projectile closing within 160 units
        // pushes the drive target perpendicular instead of absorbing the hit.
        {
            let mut threat: Option<(f64, f64)> = None;
            for p in &self.projectiles {
                let dx = r.x - p.x;
                let dy = r.y - p.y;
                let d = dx.hypot(dy);
                if d < 160.
                    && d > 1.
                    && (p.vx * dx + p.vy * dy) > 0.
                    && (threat.is_none_or(|(td, _): (f64, f64)| d < td))
                {
                    threat = Some((d, dx * p.vy - dy * p.vx));
                }
            }
            if let Some((_, cross)) = threat {
                let s = if cross * orbit >= 0. { orbit } else { -orbit };
                let d = distance(r.x, r.y, tx, ty).max(1.);
                let px = -(ty - r.y) / d * s;
                let py = (tx - r.x) / d * s;
                tx += px * 220.;
                ty += py * 220.;
                if a.label.as_deref() == Some("ENGAGE") {
                    a.label = Some("DODGE".into());
                }
            }
        }
        if nearest.is_none() && distance(r.x, r.y, tx, ty) > 2000. {
            let z = self.zone();
            if let Some(link) = self
                .world
                .transit
                .iter()
                .filter(|t| {
                    distance(t.x, t.y, z.x, z.y) <= z.radius
                        && distance(t.target_x, t.target_y, z.x, z.y) <= z.radius
                        && distance(r.x, r.y, t.x, t.y)
                            + distance(t.target_x, t.target_y, tx, ty)
                            + 160.
                            < distance(r.x, r.y, tx, ty)
                })
                .min_by(|a, b| {
                    (distance(r.x, r.y, a.x, a.y) + distance(a.target_x, a.target_y, tx, ty))
                        .total_cmp(
                            &(distance(r.x, r.y, b.x, b.y)
                                + distance(b.target_x, b.target_y, tx, ty)),
                        )
                })
            {
                tx = link.x;
                ty = link.y;
                if distance(r.x, r.y, tx, ty) < 30. {
                    a.transit = Some(link.id.clone());
                    a.brake = true;
                    a.label = Some("TRANSIT".into());
                }
            }
        }
        if self.config.mode == "br-squad"
            && let Some(j) = nearest
            && self.tick.is_multiple_of(20)
        {
            let e = &self.robots[j];
            a.message = Some(format!(
                "TARGET {} {:.0} {:.0} {}",
                e.robot_id, e.x, e.y, self.tick
            ));
        }
        if r.loadout.utilities.iter().any(|u| u == "repair_field")
            && self.robots.iter().any(|v| {
                v.alive
                    && v.team == r.team
                    && v.hp < v.max_hp * 0.7
                    && distance(r.x, r.y, v.x, v.y) < 120.
            })
        {
            a.utility = Some("repair_field".into());
        }
        if r.hp < r.max_hp * 0.4 && r.loadout.utilities.iter().any(|u| u == "cloak_emitter") {
            a.utility = Some("cloak_emitter".into());
            a.fire = false;
        }
        // Periodic sweep: contacts arrive coarse but keep bots aware beyond
        // their vision cone. Energy/cooldown gating lives server-side.
        // Idle bots scan whenever cooldown and energy allow; fighting bots
        // keep a slow periodic sweep. Gating lives server-side.
        if (nearest.is_none() && r.energy > 50.) || self.tick % 200 == (i as u32 * 37) % 200 {
            a.scan = true;
        }
        let (tx, ty) = (
            tx.clamp(RADIUS, self.config.width - RADIUS),
            ty.clamp(RADIUS, self.config.height - RADIUS),
        );
        // The local search only succeeds when some expanded node sees the
        // goal, which far goals on dense maps almost never allow. Plan toward
        // a horizon point instead, fanning out when the direct one is blocked;
        // the bot re-plans every decision, so horizons chain into a route.
        let bounds = (self.config.width, self.config.height);
        let goal_distance = distance(r.x, r.y, tx, ty);
        let direct = if goal_distance <= 600. {
            local_waypoint(&self.world, (r.x, r.y), (tx, ty), bounds, RADIUS + 3.)
        } else {
            None
        };
        let horizon = goal_distance.clamp(120., 450.);
        let waypoint = direct.or_else(|| {
            let base = (ty - r.y).atan2(tx - r.x);
            [0., 25., -25., 50., -50., 80., -80.]
                .iter()
                .filter_map(|offset: &f64| {
                    let angle = base + offset.to_radians();
                    let sub = (
                        (r.x + angle.cos() * horizon).clamp(RADIUS, self.config.width - RADIUS),
                        (r.y + angle.sin() * horizon).clamp(RADIUS, self.config.height - RADIUS),
                    );
                    if !self.world.clear(sub.0, sub.1, RADIUS + 3.) {
                        return None;
                    }
                    local_waypoint(&self.world, (r.x, r.y), sub, bounds, RADIUS + 3.)
                })
                .next()
        });
        let retreating = a.label.as_deref() == Some("RETREAT_LOW_HP");
        if let Some((wx, wy)) = waypoint {
            let to_goal = distance(r.x, r.y, tx, ty);
            let wanted = heading(r.x, r.y, wx, wy);
            let turn = (wanted - r.heading + 180.).rem_euclid(360.) - 180.;
            let roaming = matches!(a.label.as_deref(), Some("PATROL" | "INVESTIGATE"));
            if roaming && to_goal < 45. {
                // Arrived: hold position instead of orbiting an unreachable point.
                a.turn = 0.;
                a.throttle = 0.;
                a.brake = true;
            } else if turn.abs() > 120. && (retreating || to_goal < 350.) {
                // Goal behind: reverse toward it and keep facing forward rather
                // than spending a second turning on the spot.
                let back = (turn + 360.).rem_euclid(360.) - 180.;
                a.turn = back.clamp(-18., 18.) / 18.;
                a.throttle = -1.;
            } else {
                a.turn = turn.clamp(-18., 18.) / 18.;
                // Crawl through sharp turns so the bot arcs instead of pivoting.
                a.throttle = if a.brake {
                    0.
                } else if turn.abs() > 70. {
                    0.35
                } else if turn.abs() > 35. {
                    0.7
                } else {
                    1.
                };
            }
            a.cruise = nearest.is_none()
                && distance(r.x, r.y, wx, wy) > 800.
                && turn.abs() < 15.
                && r.energy > 40.;
        } else {
            // Never freeze against a wall: slide along it while turning toward
            // the goal so cover produces visible detours, not parked robots.
            let wanted = heading(r.x, r.y, tx, ty);
            let turn = (wanted - r.heading + 180.).rem_euclid(360.) - 180.;
            a.turn = turn.clamp(-30., 30.) / 30.;
            a.throttle = 0.5;
            a.brake = false;
            a.cruise = false;
            a.label = Some("BLOCKED_SLIDE".into());
        }
        if r.hp < r.max_hp * 0.6
            && let Some(s) = r
                .inventory
                .iter()
                .position(|s| s.kind == "repair_pack" || s.kind == "medkit")
            && nearest.is_none()
        {
            a.consume = Some(s);
            a.throttle = 0.;
            a.fire = false;
            a.brake = true;
            a.cruise = false;
        }
        if a.throttle.abs() >= 0.3 && !a.brake {
            let id = r.robot_id.clone();
            let speed = catalog::chassis(&r.loadout.chassis).map_or(80., |c| c.speed);
            let expected =
                a.throttle.abs() * speed * if a.throttle < 0. { 0.5 } else { 1. } * 2. * DT;
            if let Some(mem) = self.bot_memory.get_mut(&id) {
                mem.driving = true;
                mem.expected = expected;
            }
        }
        a
    }
    pub fn step(
        &mut self,
        inputs: BTreeMap<String, Action>,
        withdrawals: &[String],
    ) -> Result<(), String> {
        self.step_joining(inputs, withdrawals, Vec::new())
    }
    /// A step that first admits arena joins (see `join`); rejected joins
    /// become `join_rejected` events in this tick.
    pub fn step_joining(
        &mut self,
        inputs: BTreeMap<String, Action>,
        withdrawals: &[String],
        joins: Vec<Registration>,
    ) -> Result<(), String> {
        if self.finished {
            return Ok(());
        }
        for a in inputs.values() {
            a.validate()?;
        }
        self.events.clear();
        if self.config.mode == "arena" {
            self.arena_upkeep();
            self.arena_objectives();
        }
        for reg in joins {
            let id = reg.robot_id.clone();
            if let Err(reason) = self.join(reg) {
                self.reject_join(id, reason);
            }
        }
        for r in &mut self.robots {
            r.action_results.clear();
        }
        let mut actions = Vec::with_capacity(self.robots.len());
        for i in 0..self.robots.len() {
            let (is_bot, robot_id) = {
                let r = &self.robots[i];
                (r.bot, r.robot_id.clone())
            };
            let action = if is_bot {
                if self.tick.is_multiple_of(2) {
                    let action = self.bot_action(i);
                    self.bot_intents.insert(robot_id, action.clone());
                    action
                } else {
                    self.bot_intents
                        .get(&robot_id)
                        .cloned()
                        .unwrap_or_default()
                        .continuous()
                }
            } else {
                inputs.get(&robot_id).cloned().unwrap_or_default()
            };
            actions.push(action);
        }
        let mut hits = vec![];
        for (i, action) in actions.iter().enumerate() {
            if !self.robots[i].alive {
                // A player leaving while waiting to respawn still leaves.
                if self.config.mode == "arena" && withdrawals.contains(&self.robots[i].robot_id) {
                    self.robots[i].left = true;
                    self.robots[i].respawn_at = None;
                }
                continue;
            }
            if withdrawals.contains(&self.robots[i].robot_id) {
                hits.push(Hit {
                    source: String::new(),
                    target: i,
                    damage: 1e9,
                    kind: "withdraw".into(),
                    bypass: true,
                });
                continue;
            }
            self.move_robot(i, action);
            self.inventory(i, action);
            self.utility(i, action);
            if let Some(message) = &action.message {
                let team = self.robots[i].team.clone();
                let id = self.robots[i].robot_id.clone();
                for r in &mut self.robots {
                    if r.team == team && r.robot_id != id {
                        if r.messages.len() >= 5 {
                            r.messages.remove(0);
                        }
                        r.messages.push(format!("{} {id}: {message}", self.tick));
                    }
                }
            }
            if self.robots[i].burn_until > self.tick {
                hits.push(Hit {
                    source: String::new(),
                    target: i,
                    damage: 3. * DT,
                    kind: "burn".into(),
                    bypass: false,
                });
            }
            let zone = self.zone();
            let r = &self.robots[i];
            if zone.active && distance(r.x, r.y, zone.x, zone.y) > zone.radius {
                hits.push(Hit {
                    source: String::new(),
                    target: i,
                    damage: zone.damage * DT,
                    kind: "zone".into(),
                    bypass: true,
                });
            }
        }
        self.reindex_robots();
        for i in 0..self.robots.len() {
            if !self.robots[i].alive {
                continue;
            }
            for h in &self.world.hazards {
                let r = &mut self.robots[i];
                if r.x >= h.x && r.x <= h.x + h.width && r.y >= h.y && r.y <= h.y + h.height {
                    if h.kind == "slow" {
                        r.slow_until = self.tick + 2;
                    } else {
                        hits.push(Hit {
                            source: String::new(),
                            target: i,
                            damage: h.damage_per_second * DT,
                            kind: "hazard".into(),
                            bypass: false,
                        });
                    }
                }
            }
        }
        self.resolve_overlaps();
        self.reindex_robots();
        for (i, action) in actions.iter().enumerate() {
            if self.robots[i].alive && !withdrawals.contains(&self.robots[i].robot_id) {
                if action.fire {
                    self.robots[i].protected_until = 0;
                }
                self.shoot(i, action, &mut hits);
            }
        }
        self.projectiles_step(&mut hits);
        self.fields_step(&mut hits);
        for hit in &hits {
            self.damage(hit);
        }
        for i in 0..self.robots.len() {
            if self.robots[i].alive {
                self.complete_channel(i);
            }
        }
        let alive_before = self.robots.iter().filter(|r| r.alive).count();
        let deaths: Vec<usize> = self
            .robots
            .iter()
            .enumerate()
            .filter(|(_, r)| r.alive && r.hp <= 0.)
            .map(|(i, _)| i)
            .collect();
        let place = alive_before - deaths.len() + 1;
        let arena_mode = self.config.mode == "arena";
        for i in deaths {
            self.robots[i].alive = false;
            if arena_mode {
                // Arena robots come back after five seconds unless the player
                // left; there are no placements in a never-ending arena.
                self.robots[i].deaths += 1;
                if withdrawals.contains(&self.robots[i].robot_id) {
                    self.robots[i].left = true;
                } else {
                    self.robots[i].respawn_at = Some(self.tick + ARENA_RESPAWN_TICKS);
                }
            } else {
                self.robots[i].placement = Some(place);
            }
            self.robots[i].speed = 0.;
            let id = self.robots[i].robot_id.clone();
            self.event("robot_destroyed", Some(id.clone()), None, None, None);
            let killer = self
                .events
                .iter()
                .rev()
                .find(|e| {
                    e.r#type == "hit"
                        && e.target_id.as_ref() == Some(&id)
                        && e.damage.is_some_and(|d| d > 0.)
                })
                .and_then(|e| e.robot_id.clone());
            let victim_streak = std::mem::take(&mut self.robots[i].streak);
            if let Some(killer) = killer
                && let Some(k) = self.robots.iter().position(|r| r.robot_id == killer)
            {
                self.robots[k].kills += 1;
                self.event("kill", Some(killer.clone()), Some(id.clone()), None, None);
                if arena_mode {
                    self.robots[k].streak += 1;
                    self.robots[k].score += KILL_POINTS;
                    if victim_streak >= BOUNTY_STREAK {
                        let bounty = BOUNTY_PER_KILL * victim_streak;
                        self.robots[k].score += bounty;
                        self.event(
                            "bounty_claimed",
                            Some(killer),
                            Some(id.clone()),
                            Some(f64::from(bounty)),
                            None,
                        );
                    }
                }
            }
            if self.world.containers.len() < MAX_CONTAINERS {
                let r = &self.robots[i];
                let mut contents = r.inventory.clone();
                for w in &r.weapons {
                    contents.push(Stack {
                        kind: format!("weapon:{}", w.kind),
                        weapon_state: Some(w.clone()),
                        count: 1,
                        ..Default::default()
                    });
                }
                for m in &r.loadout.modules {
                    contents.push(Stack {
                        kind: m.clone(),
                        count: 1,
                        ..Default::default()
                    });
                }
                for u in &r.loadout.utilities {
                    contents.push(Stack {
                        kind: format!("utility:{u}"),
                        count: 1,
                        charges: r.charges.get(u).copied(),
                        ready_at: r.cooldowns.get(u).copied(),
                        ..Default::default()
                    });
                }
                let (x, y) = (r.x, r.y);
                let item_id = self.id("salvage");
                let expires_at = self.salvage_expiry();
                self.world.containers.push(Container {
                    item_id,
                    x,
                    y,
                    contents,
                    expires_at,
                });
            }
        }
        self.supply();
        self.recent_events.extend(self.events.iter().cloned());
        self.recent_events.retain(|e| e.tick + 10 >= self.tick);
        if self.recent_events.len() > 16384 {
            self.recent_events.drain(..self.recent_events.len() - 16384);
        }
        self.tick += 1;
        self.reindex_robots();
        self.finish();
        Ok(())
    }
    fn move_robot(&mut self, i: usize, a: &Action) {
        let tick = self.tick;
        let r = &mut self.robots[i];
        let cooling = 25. * if r.has("cooling_system") { 1.25 } else { 1. };
        for w in &mut r.weapons {
            w.heat = (w.heat - cooling * DT).max(0.);
            if w.heat <= 40. {
                w.overheated = false;
            }
        }
        if let Some(w) = a.weapon {
            if w < r.weapons.len() {
                r.active_weapon = w;
            } else {
                r.action_results.push("INVALID_WEAPON_SLOT".into());
            }
        }
        r.heading = (r.heading + a.turn * 180. * DT).rem_euclid(360.);
        if let Some(aim) = a.aim {
            r.turret_heading = rotate(r.turret_heading, aim, 270. * DT);
        }
        r.last_action = a.label.clone().unwrap_or_default();
        r.cruise = a.cruise
            && r.emp_until <= tick
            && r.energy >= 10. * DT
            && r.last_damage_tick
                .is_none_or(|t| tick.saturating_sub(t) > 20);
        if r.cruise {
            r.energy -= 10. * DT;
        } else {
            r.energy = (r.energy + 10. * DT).min(r.max_energy);
        }
        let speed = catalog::chassis(&r.loadout.chassis).unwrap().speed
            * if r.has("mobility_tuning") { 1.1 } else { 1. }
            * if r.slow_until > tick { 0.75 } else { 1. }
            * if r.cruise { 4. } else { 1. };
        let target = if a.brake {
            0.
        } else {
            a.throttle * speed * if a.throttle < 0. { 0.5 } else { 1. }
        };
        r.speed += (target - r.speed).clamp(-240. * DT, 240. * DT);
        let mut travel = r.speed * DT;
        if a.dash
            && r.energy >= 30.
            && r.emp_until <= tick
            && *r.cooldowns.get("dash").unwrap_or(&0) <= tick
        {
            travel += 80.;
            r.energy -= 30.;
            r.cooldowns.insert("dash".into(), tick + 80);
            r.channel = None;
        }
        // A robot whose centre ended up inside a wall's radius band would see
        // every sweep hit at t = 0 and freeze for the rest of the match. Push
        // it back out along the contact normal first.
        if let Some((px, py)) = self.world.depenetrate(r.x, r.y, RADIUS) {
            r.x = px.clamp(RADIUS, self.config.width - RADIUS);
            r.y = py.clamp(RADIUS, self.config.height - RADIUS);
        }
        let nx =
            (r.x + r.heading.to_radians().cos() * travel).clamp(RADIUS, self.config.width - RADIUS);
        let ny = (r.y + r.heading.to_radians().sin() * travel)
            .clamp(RADIUS, self.config.height - RADIUS);
        let fraction = self
            .world
            .wall_hit(r.x, r.y, nx, ny, RADIUS)
            .map_or(1., |t| (t - 0.001).max(0.));
        r.vx = (nx - r.x) * fraction / DT;
        r.vy = (ny - r.y) * fraction / DT;
        r.x += r.vx * DT;
        r.y += r.vy * DT;
        if fraction < 1. {
            r.speed = 0.;
            r.action_results.push("MOVEMENT_BLOCKED".into());
        }
        if a.throttle != 0. || a.fire || r.vx.abs() + r.vy.abs() > 0.01 {
            r.channel = None;
            r.transit_channel = None;
        }
    }
    fn resolve_overlaps(&mut self) {
        for i in 0..self.robots.len() {
            if !self.robots[i].alive {
                continue;
            }
            let p = &self.robots[i];
            for j in self
                .robot_grid
                .query(
                    p.x - RADIUS * 3.,
                    p.y - RADIUS * 3.,
                    RADIUS * 6.,
                    RADIUS * 6.,
                )
                .into_iter()
                .filter(|j| *j > i)
            {
                if !self.robots[j].alive {
                    continue;
                }
                let (a, b) = (&self.robots[i], &self.robots[j]);
                let d = distance(a.x, a.y, b.x, b.y);
                if d < RADIUS * 2. {
                    let dx = if d < 0.001 { 1. } else { (b.x - a.x) / d };
                    let dy = if d < 0.001 { 0. } else { (b.y - a.y) / d };
                    let push = (RADIUS * 2. - d) / 2. + 0.001;
                    for (k, sign) in [(i, -1.), (j, 1.)] {
                        let r = &mut self.robots[k];
                        let nx = (r.x + sign * dx * push).clamp(RADIUS, self.config.width - RADIUS);
                        let ny =
                            (r.y + sign * dy * push).clamp(RADIUS, self.config.height - RADIUS);
                        if self.world.clear(nx, ny, RADIUS) {
                            r.x = nx;
                            r.y = ny;
                        }
                    }
                }
            }
        }
    }
    fn damage(&mut self, h: &Hit) {
        if h.damage <= 0. {
            return;
        }
        if !h.bypass && self.robots[h.target].protected_until > self.tick {
            return;
        }
        let r = &mut self.robots[h.target];
        let original = r.hp + r.shield;
        let mut d = h.damage;
        if !h.bypass {
            let absorbed = d.min(r.shield);
            r.shield -= absorbed;
            d -= absorbed;
        }
        r.hp = (r.hp - d).max(0.);
        let actual = original - r.hp - r.shield;
        r.damage_taken += actual;
        r.cloak_until = 0;
        r.cruise = false;
        r.channel = None;
        r.transit_channel = None;
        r.last_damage_tick = Some(self.tick);
        match h.kind.as_str() {
            "incendiary" => r.burn_until = self.tick + 80,
            "cryo" => r.slow_until = self.tick + 40,
            "emp" => r.emp_until = self.tick + 30,
            _ => {}
        }
        let target = r.robot_id.clone();
        if h.kind == "cannon" {
            self.cannon_push(h.target, &h.source);
        }
        if !h.source.is_empty()
            && let Some(s) = self.robots.iter_mut().find(|r| r.robot_id == h.source)
        {
            s.damage_dealt += actual;
        }
        self.event(
            "hit",
            if h.source.is_empty() {
                None
            } else {
                Some(h.source.clone())
            },
            Some(target),
            Some(actual),
            Some(h.kind.clone()),
        );
    }
    fn cannon_push(&mut self, target: usize, source: &str) {
        let Some(shooter) = self.robots.iter().find(|r| r.robot_id == source) else {
            return;
        };
        let r = &self.robots[target];
        let (x, y) = (r.x, r.y);
        let length = distance(shooter.x, shooter.y, x, y);
        if length < 0.001 {
            return;
        }
        let nx = (x + (x - shooter.x) / length * catalog::CANNON_KNOCKBACK)
            .clamp(RADIUS, self.config.width - RADIUS);
        let ny = (y + (y - shooter.y) / length * catalog::CANNON_KNOCKBACK)
            .clamp(RADIUS, self.config.height - RADIUS);
        let mut fraction = self.world.wall_hit(x, y, nx, ny, RADIUS).unwrap_or(1.);
        for (i, other) in self.robots.iter().enumerate() {
            if i != target
                && other.alive
                && let Some(hit) = segment_circle(x, y, nx, ny, other.x, other.y, RADIUS * 2.)
            {
                fraction = fraction.min(hit);
            }
        }
        let fraction = if fraction < 1. {
            (fraction - 0.001).max(0.)
        } else {
            fraction
        };
        self.robots[target].x = x + (nx - x) * fraction;
        self.robots[target].y = y + (ny - y) * fraction;
    }
    fn finish(&mut self) {
        if self.config.mode == "arena" {
            // Arena sessions only end on time; the top scorer wins.
            if self.tick >= self.config.duration_seconds * 20 {
                self.finished = true;
                self.winner_team = self
                    .robots
                    .iter()
                    .max_by(|a, b| {
                        a.score
                            .cmp(&b.score)
                            .then(a.kills.cmp(&b.kills))
                            .then(b.deaths.cmp(&a.deaths))
                            .then(b.robot_id.cmp(&a.robot_id))
                    })
                    .map(|r| r.team.clone())
                    .unwrap_or_else(|| "draw".into());
            }
            return;
        }
        if self.config.mode == "sandbox" && self.tick < self.config.duration_seconds * 20 {
            return;
        }
        let mut scores: BTreeMap<String, (usize, f64, f64)> = BTreeMap::new();
        for r in self.robots.iter().filter(|r| r.alive) {
            let s = scores.entry(r.team.clone()).or_default();
            s.0 += 1;
            s.1 += r.hp + r.shield;
            s.2 += r.damage_dealt;
        }
        if scores.len() <= 1 || self.tick >= self.config.duration_seconds * 20 {
            self.finished = true;
            let mut ranked: Vec<_> = scores.into_iter().collect();
            ranked.sort_by(|a, b| {
                b.1.0
                    .cmp(&a.1.0)
                    .then(b.1.1.total_cmp(&a.1.1))
                    .then(b.1.2.total_cmp(&a.1.2))
            });
            self.winner_team =
                if ranked.is_empty() || (ranked.len() > 1 && ranked[0].1 == ranked[1].1) {
                    "draw".into()
                } else {
                    ranked[0].0.clone()
                };
            for r in &mut self.robots {
                if r.alive {
                    r.placement = Some(1);
                }
            }
        }
    }
    fn supply(&mut self) {
        let seconds = self.tick as f64 * DT;
        for minute in [4, 7, 10, 13] {
            let due = minute * 60;
            if seconds == f64::from(due - 30) {
                self.event(
                    "supply_announced",
                    None,
                    None,
                    None,
                    Some("Supply at arena centre in 30 seconds".to_string()),
                );
            }
            if seconds == f64::from(due) && self.world.containers.len() < MAX_CONTAINERS {
                let item_id = self.id("supply");
                self.world.containers.push(Container {
                    item_id,
                    x: self.config.width / 2.,
                    y: self.config.height / 2.,
                    contents: vec![
                        Stack {
                            kind: "weapon:railgun".into(),
                            count: 1,
                            ..Default::default()
                        },
                        Stack {
                            kind: "medkit".into(),
                            count: 3,
                            ..Default::default()
                        },
                    ],
                    expires_at: None,
                });
            }
        }
    }
    pub fn snapshot(&self) -> Value {
        let robots:Vec<_>=self.robots.iter().map(|r|json!({"robotId":r.robot_id,"name":r.name,"team":r.team,"bot":r.bot,"x":r.x,"y":r.y,"vx":r.vx,"vy":r.vy,"heading":r.heading,"turretHeading":r.turret_heading,"hp":r.hp,"maxHp":r.max_hp,"shield":r.shield,"maxShield":r.max_shield,"energy":r.energy,"maxEnergy":r.max_energy,"alive":r.alive,"weapon":r.weapon(),"visionRange":r.vision_range,"placement":r.placement,"damageDealt":r.damage_dealt,"damageTaken":r.damage_taken,"kills":r.kills,"deaths":r.deaths,"respawnIn":r.respawn_at.map(|t|t.saturating_sub(self.tick)),"score":r.score,"streak":r.streak,"bounty":if r.streak>=BOUNTY_STREAK{BOUNTY_PER_KILL*r.streak}else{0},"protected":r.protected_until>self.tick,"effects":self.visible_effects(r)})).collect();
        let items: Vec<_> = self
            .world
            .containers
            .iter()
            .filter(|c| !c.contents.is_empty())
            .map(|c| json!({"itemId":c.item_id,"x":c.x,"y":c.y,"type":"container","active":true,"contents":c.contents}))
            .collect();
        json!({"type":"snapshot","version":4,"matchId":self.config.match_id,"sequence":self.tick,"tick":self.tick,"tickRate":20,"status":if self.finished{"finished"}else{"running"},"winnerTeam":self.winner_team,"width":self.config.width,"height":self.config.height,"mapId":"world-v4","mode":self.config.mode,"endTick":self.config.duration_seconds*20,"revision":self.world.revision,"robots":robots,"projectiles":self.projectiles,"items":items,"obstacles":self.world.obstacles,"mines":self.mines,"fields":self.fields,"transit":self.world.transit,"hazards":self.world.hazards,"sites":self.world.sites,"zone":self.zone(),"hill":self.hill(),"events":self.events,"feed":self.recent_events.iter().filter(|e|matches!(e.r#type.as_str(),"kill"|"bounty_claimed"|"hill_moved"|"robot_joined")).collect::<Vec<_>>()})
    }
    /// Status effects a spectator could see on the robot's body.
    fn visible_effects(&self, r: &Robot) -> Vec<&'static str> {
        [
            (r.burn_until > self.tick, "burning"),
            (r.slow_until > self.tick, "slowed"),
            (r.emp_until > self.tick, "emp"),
            (r.cloak_until > self.tick, "cloaked"),
            (r.channel.is_some(), "healing"),
            (r.cruise, "cruising"),
        ]
        .into_iter()
        .filter_map(|(on, name)| on.then_some(name))
        .collect()
    }
    pub fn observation(&self, i: usize) -> Value {
        self.observation_with_geometry(i, true)
    }
    pub fn geometry(&self) -> Value {
        json!({"obstacles":self.world.obstacles,"transit":self.world.transit,"sites":self.world.sites,"hazards":self.world.hazards})
    }
    pub fn observation_with_geometry(&self, i: usize, include_geometry: bool) -> Value {
        let r = &self.robots[i];
        let nearby = |x: f64, y: f64| {
            r.alive
                && distance(r.x, r.y, x, y) <= r.vision_range
                && self.world.los(r.x, r.y, x, y)
                && !self.fields.iter().any(|f| {
                    f.kind == "smoke" && segment_circle(r.x, r.y, x, y, f.x, f.y, 100.).is_some()
                })
        };
        let robots:Vec<_>=self.robots.iter().enumerate().filter(|(j,_)|*j!=i&&self.visible(i,*j)).map(|(_,v)|json!({"robotId":v.robot_id,"team":v.team,"x":v.x,"y":v.y,"vx":v.vx,"vy":v.vy,"heading":v.heading,"turretHeading":v.turret_heading,"hp":v.hp,"shield":v.shield,"alive":v.alive,"weapon":v.weapon()})).collect();
        let mut observation = json!({"type":"observation","version":4,"matchId":self.config.match_id,"tick":self.tick,"tickRate":20,"self":r,"robots":robots,"arenaWidth":self.config.width,"arenaHeight":self.config.height,"visionRange":r.vision_range,"revision":self.world.revision,"items":self.world.containers.iter().filter(|c|!c.contents.is_empty()&&nearby(c.x,c.y)).collect::<Vec<_>>(),"projectiles":self.projectiles.iter().filter(|p|nearby(p.x,p.y)).collect::<Vec<_>>(),"mines":self.mines.iter().filter(|m|nearby(m.x,m.y)).collect::<Vec<_>>(),"zone":self.zone(),"hill":self.hill(),"messages":r.messages,"events":self.recent_events.iter().filter(|e|e.robot_id.as_ref()==Some(&r.robot_id)||e.target_id.as_ref()==Some(&r.robot_id)||e.r#type=="supply_announced"||(e.r#type=="scan_emitted"&&e.x.zip(e.y).is_some_and(|(x,y)|distance(r.x,r.y,x,y)<=900.))).collect::<Vec<_>>()});
        if include_geometry {
            observation
                .as_object_mut()
                .unwrap()
                .extend(self.geometry().as_object().unwrap().clone());
        }
        observation
    }
    pub fn state_hash(&self) -> String {
        let bytes = serde_json::to_vec(self).expect("finite authoritative state");
        let hash = bytes.iter().fold(0xcbf29ce484222325u64, |h, b| {
            (h ^ u64::from(*b)).wrapping_mul(0x100000001b3)
        });
        format!("{hash:016x}")
    }
}
impl Arena {
    fn shoot(&mut self, i: usize, a: &Action, hits: &mut Vec<Hit>) {
        let r = &mut self.robots[i];
        let slot = r.active_weapon;
        let w = catalog::weapon(&r.weapons[slot].kind).unwrap();
        if r.cruise {
            return;
        }
        let releasing = r.weapons[slot].charge_until.is_some_and(|t| self.tick >= t);
        if !releasing {
            if !a.fire || r.weapons[slot].ready_at > self.tick || r.weapons[slot].overheated {
                return;
            }
            if w.id == "railgun" {
                if r.weapons[slot].charge_until.is_none() {
                    r.weapons[slot].charge_until = Some(self.tick + 8);
                    r.cloak_until = 0;
                }
                return;
            }
        }
        if w.pellets + self.projectiles.len() > MAX_PROJECTILES
            || self
                .projectiles
                .iter()
                .filter(|p| p.owner_id == r.robot_id)
                .count()
                + w.pellets
                > 64
        {
            r.action_results.push("PROJECTILE_LIMIT".into());
            return;
        }
        r.weapons[slot].charge_until = None;
        r.weapons[slot].ready_at = self.tick + (w.interval / DT).ceil() as u32;
        r.weapons[slot].heat = (r.weapons[slot].heat + w.heat).min(100.);
        r.weapons[slot].overheated = r.weapons[slot].heat >= 100.;
        r.cloak_until = 0;
        r.channel = None;
        let (x, y, angle, owner, team) = (
            r.x,
            r.y,
            r.turret_heading,
            r.robot_id.clone(),
            r.team.clone(),
        );
        self.event(
            "shot_fired",
            Some(owner.clone()),
            None,
            None,
            Some(w.id.into()),
        );
        if w.id == "railgun" {
            let nx = x + angle.to_radians().cos() * w.range;
            let ny = y + angle.to_radians().sin() * w.range;
            let wall = self.world.shot_hit(x, y, nx, ny, 0.).unwrap_or(1.);
            let target = self
                .robot_grid
                .query(x.min(nx), y.min(ny), (nx - x).abs(), (ny - y).abs())
                .into_iter()
                .filter(|j| {
                    *j != i
                        && self.robots[*j].alive
                        && (self.config.friendly_fire || self.robots[*j].team != team)
                })
                .filter_map(|j| {
                    let r = &self.robots[j];
                    segment_circle(x, y, nx, ny, r.x, r.y, RADIUS)
                        .filter(|t| *t < wall)
                        .map(|t| (j, t))
                })
                .min_by(|a, b| a.1.total_cmp(&b.1).then(a.0.cmp(&b.0)));
            if let Some((target, _)) = target {
                hits.push(Hit {
                    source: owner,
                    target,
                    damage: w.damage,
                    kind: w.id.into(),
                    bypass: false,
                });
            }
            return;
        }
        for pellet in 0..w.pellets {
            let spread = if w.pellets > 1 {
                (pellet as f64 - (w.pellets - 1) as f64 / 2.) * 4.
            } else {
                0.
            };
            let radians = (angle + spread).to_radians();
            let projectile_id = self.id("projectile");
            self.projectiles.push(Projectile {
                projectile_id,
                owner_id: owner.clone(),
                team: team.clone(),
                kind: w.id.into(),
                x: x + radians.cos() * (RADIUS + 3.),
                y: y + radians.sin() * (RADIUS + 3.),
                vx: radians.cos() * w.speed,
                vy: radians.sin() * w.speed,
                damage: w.damage,
                remaining: w.range,
            });
        }
    }
    fn projectiles_step(&mut self, hits: &mut Vec<Hit>) {
        let mut keep = vec![];
        for mut p in std::mem::take(&mut self.projectiles) {
            let nx = p.x + p.vx * DT;
            let ny = p.y + p.vy * DT;
            let wall = self.world.shot_hit(p.x, p.y, nx, ny, 2.).unwrap_or(2.);
            let target = self
                .robot_grid
                .query(
                    p.x.min(nx) - RADIUS,
                    p.y.min(ny) - RADIUS,
                    (nx - p.x).abs() + RADIUS * 2.,
                    (ny - p.y).abs() + RADIUS * 2.,
                )
                .into_iter()
                .filter(|j| {
                    let r = &self.robots[*j];
                    r.alive
                        && r.robot_id != p.owner_id
                        && (self.config.friendly_fire || r.team != p.team)
                })
                .filter_map(|j| {
                    let r = &self.robots[j];
                    segment_circle(p.x, p.y, nx, ny, r.x, r.y, RADIUS + 2.)
                        .filter(|t| *t < wall)
                        .map(|t| (j, t))
                })
                .min_by(|a, b| a.1.total_cmp(&b.1).then(a.0.cmp(&b.0)));
            p.remaining -= p.vx.hypot(p.vy) * DT;
            let impact = target.map(|(_, t)| t).unwrap_or(wall.min(1.));
            p.x += (nx - p.x) * impact;
            p.y += (ny - p.y) * impact;
            if target.is_some() || wall <= 1. || p.remaining <= 0. {
                if p.kind == "grenade" {
                    self.blast(&p.owner_id, &p.team, (p.x, p.y), 80., p.damage, hits);
                } else if let Some((target, _)) = target {
                    hits.push(Hit {
                        source: p.owner_id.clone(),
                        target,
                        damage: p.damage,
                        kind: p.kind.clone(),
                        bypass: false,
                    });
                }
                continue;
            }
            if p.x >= 0. && p.x <= self.config.width && p.y >= 0. && p.y <= self.config.height {
                keep.push(p);
            }
        }
        self.projectiles = keep;
    }
    fn blast(
        &self,
        owner: &str,
        team: &str,
        origin: (f64, f64),
        radius: f64,
        damage: f64,
        hits: &mut Vec<Hit>,
    ) {
        let (x, y) = origin;
        for i in self
            .robot_grid
            .query(x - radius, y - radius, radius * 2., radius * 2.)
        {
            let r = &self.robots[i];
            let d = distance(x, y, r.x, r.y);
            if r.alive
                && r.robot_id != owner
                && (self.config.friendly_fire || r.team != team)
                && d <= radius
                && self.world.los(x, y, r.x, r.y)
            {
                hits.push(Hit {
                    source: owner.into(),
                    target: i,
                    damage: damage * (1. - d / radius),
                    kind: "explosion".into(),
                    bypass: false,
                });
            }
        }
    }
    fn utility(&mut self, i: usize, a: &Action) {
        let tick = self.tick;
        let r = &self.robots[i];
        if r.emp_until > tick {
            return;
        }
        if a.scan && r.energy >= 25. && *r.cooldowns.get("scan").unwrap_or(&0) <= tick {
            let (x, y, id) = (r.x, r.y, r.robot_id.clone());
            let contacts:Vec<_>=self.robots.iter().filter(|v|v.alive&&v.team!=self.robots[i].team&&distance(x,y,v.x,v.y)<=900.).map(|v|json!({"x":(v.x/100.).round()*100.,"y":(v.y/100.).round()*100.,"tick":tick})).collect();
            if self.robots[i].bot {
                let nearest = self
                    .robots
                    .iter()
                    .filter(|v| {
                        v.alive && v.team != self.robots[i].team && distance(x, y, v.x, v.y) <= 900.
                    })
                    .map(|v| ((v.x / 100.).round() * 100., (v.y / 100.).round() * 100.))
                    .min_by(|a, b| distance(x, y, a.0, a.1).total_cmp(&distance(x, y, b.0, b.1)));
                if let (Some((hx, hy)), Some(mem)) = (nearest, self.bot_memory.get_mut(&id)) {
                    mem.hunt_x = hx;
                    mem.hunt_y = hy;
                    mem.hunt_until = tick + 200;
                }
            }
            self.robots[i].energy -= 25.;
            self.robots[i].cooldowns.insert("scan".into(), tick + 160);
            self.event(
                "scan_result",
                Some(id.clone()),
                None,
                None,
                Some(serde_json::to_string(&contacts).unwrap()),
            );
            self.event("scan_emitted", Some(id), None, None, None);
            if let Some(event) = self.events.last_mut() {
                event.x = Some((x / 100.).round() * 100.);
                event.y = Some((y / 100.).round() * 100.);
            }
        }
        let Some(u) = &a.utility else {
            return;
        };
        let r = &self.robots[i];
        if !r.loadout.utilities.contains(u) {
            self.robots[i]
                .action_results
                .push("UTILITY_NOT_EQUIPPED".into());
            return;
        }
        if *r.cooldowns.get(u).unwrap_or(&0) > tick {
            return;
        }
        let energy = match u.as_str() {
            "cloak_emitter" => 35.,
            "repair_field" => 40.,
            _ => 0.,
        };
        if r.energy < energy {
            return;
        }
        if (u == "mine_dispenser" || u == "smoke_projector") && *r.charges.get(u).unwrap_or(&0) == 0
        {
            return;
        }
        if u == "mine_dispenser"
            && (self.mines.len() >= MAX_MINES
                || self
                    .mines
                    .iter()
                    .filter(|m| m.owner_id == r.robot_id)
                    .count()
                    >= 3)
        {
            self.robots[i].action_results.push("MINE_LIMIT".into());
            return;
        }
        if (u == "smoke_projector" || u == "repair_field") && self.fields.len() >= MAX_FIELDS {
            self.robots[i].action_results.push("FIELD_LIMIT".into());
            return;
        }
        let (x, y, owner_id, team) = (r.x, r.y, r.robot_id.clone(), r.team.clone());
        let id = self.id("utility");
        let r = &mut self.robots[i];
        r.energy -= energy;
        match u.as_str() {
            "cloak_emitter" => {
                r.cloak_until = tick + 100;
                r.cooldowns.insert(u.clone(), tick + 400);
            }
            "mine_dispenser" => {
                *r.charges.get_mut(u).unwrap() -= 1;
                self.mines.push(Mine {
                    mine_id: id,
                    owner_id,
                    team,
                    x,
                    y,
                    arm_tick: tick + 60,
                    end_tick: tick + 1200,
                });
            }
            "smoke_projector" => {
                *r.charges.get_mut(u).unwrap() -= 1;
                self.fields.push(Field {
                    id,
                    kind: "smoke".into(),
                    owner_id,
                    team,
                    x,
                    y,
                    end_tick: tick + 160,
                });
            }
            "repair_field" => {
                r.cooldowns.insert(u.clone(), tick + 400);
                self.fields.push(Field {
                    id,
                    kind: "repair".into(),
                    owner_id,
                    team,
                    x,
                    y,
                    end_tick: tick + 100,
                });
            }
            _ => {}
        }
    }
    fn fields_step(&mut self, hits: &mut Vec<Hit>) {
        let mut keep = vec![];
        for m in std::mem::take(&mut self.mines) {
            if m.end_tick <= self.tick {
                continue;
            }
            let triggered = m.arm_tick <= self.tick
                && self
                    .robot_grid
                    .query(m.x - 28., m.y - 28., 56., 56.)
                    .iter()
                    .any(|i| {
                        let r = &self.robots[*i];
                        r.team != m.team && distance(r.x, r.y, m.x, m.y) <= 28.
                    });
            if triggered {
                self.blast(&m.owner_id, &m.team, (m.x, m.y), 70., 50., hits);
            } else {
                keep.push(m);
            }
        }
        self.mines = keep;
        self.fields.retain(|f| f.end_tick > self.tick);
        for f in &self.fields {
            if f.kind == "repair" {
                for r in &mut self.robots {
                    if r.alive && r.team == f.team && distance(r.x, r.y, f.x, f.y) < 120. {
                        r.hp = (r.hp + 4. * DT).min(r.max_hp);
                    }
                }
            }
        }
    }
    fn inventory(&mut self, i: usize, a: &Action) {
        self.equipment_actions(i, a);
        if let Some(priorities) = &a.pickup_priorities {
            self.robots[i].pickup_priorities = priorities.clone();
        }
        let automatic = if a.pickup.is_none() && !self.robots[i].pickup_priorities.is_empty() {
            let r = &self.robots[i];
            self.world
                .containers
                .iter()
                .filter(|c| {
                    distance(r.x, r.y, c.x, c.y) <= 35. && self.world.los(r.x, r.y, c.x, c.y)
                })
                .filter_map(|c| {
                    c.contents
                        .iter()
                        .filter_map(|s| r.pickup_priorities.iter().position(|p| p == &s.kind))
                        .min()
                        .map(|priority| (priority, &c.item_id))
                })
                .min()
                .map(|(_, id)| id.clone())
        } else {
            None
        };
        if let Some(slot) = a.drop_slot
            && slot < self.robots[i].inventory.len()
        {
            if !self
                .world
                .can_add_container(self.robots[i].x, self.robots[i].y)
            {
                self.robots[i]
                    .action_results
                    .push("CONTAINER_BUDGET_EXCEEDED".into());
            } else {
                let stack = self.robots[i].inventory.remove(slot);
                let item_id = self.id("drop");
                let expires_at = self.salvage_expiry();
                self.world.containers.push(Container {
                    item_id,
                    x: self.robots[i].x,
                    y: self.robots[i].y,
                    contents: vec![stack],
                    expires_at,
                });
                self.robots[i].channel = None;
            }
        }
        if let Some(id) = a.pickup.as_ref().or(automatic.as_ref())
            && let Some(j) = self.world.containers.iter().position(|c| &c.item_id == id)
        {
            let c = &self.world.containers[j];
            let r = &self.robots[i];
            if distance(r.x, r.y, c.x, c.y) <= 35. && self.world.los(r.x, r.y, c.x, c.y) {
                let mut remaining = vec![];
                for mut item in std::mem::take(&mut self.world.containers[j].contents) {
                    let r = &mut self.robots[i];
                    if automatic.is_some() && !r.pickup_priorities.contains(&item.kind) {
                        remaining.push(item);
                        continue;
                    }
                    if let Some(w) = item.kind.strip_prefix("weapon:") {
                        if r.weapons.len() < 2
                            && !r.weapons.iter().any(|v| v.kind == w)
                            && catalog::weapon(w).is_some()
                        {
                            r.weapons.push(item.weapon_state.clone().unwrap_or_else(|| {
                                WeaponSlot {
                                    kind: w.into(),
                                    ..Default::default()
                                }
                            }));
                            item.count -= 1;
                        }
                    } else if let Some(u) = item.kind.strip_prefix("utility:") {
                        if r.loadout.utilities.len() < 2
                            && !r.loadout.utilities.iter().any(|v| v == u)
                            && catalog::UTILITIES.iter().any(|(id, _)| *id == u)
                        {
                            r.loadout.utilities.push(u.into());
                            r.charges.insert(
                                u.into(),
                                item.charges.unwrap_or(match u {
                                    "mine_dispenser" => 3,
                                    "smoke_projector" => 2,
                                    _ => 0,
                                }),
                            );
                            r.cooldowns.insert(u.into(), item.ready_at.unwrap_or(0));
                            item.count -= 1;
                        }
                    } else if catalog::MODULES.contains(&item.kind.as_str()) {
                        if r.loadout.modules.len() < 2 && !r.has(&item.kind) {
                            r.loadout.modules.push(item.kind.clone());
                            item.count -= 1;
                            Self::module_stats(r);
                        }
                    } else if catalog::CONSUMABLES.contains(&item.kind.as_str()) {
                        if let Some(slot) = r
                            .inventory
                            .iter_mut()
                            .find(|s| s.kind == item.kind && s.count < 3)
                        {
                            let n = item.count.min(3 - slot.count);
                            slot.count += n;
                            item.count -= n;
                        }
                        if item.count > 0 && r.inventory.len() < 4 {
                            let n = item.count.min(3);
                            r.inventory.push(Stack {
                                kind: item.kind.clone(),
                                count: n,
                                ..Default::default()
                            });
                            item.count -= n;
                        }
                    }
                    if item.count > 0 {
                        remaining.push(item);
                    }
                }
                self.world.containers[j].contents = remaining;
            }
        }
        let tick = self.tick;
        let zone = self.zone();
        let r = &mut self.robots[i];
        if let Some(slot) = a.consume
            && r.channel.is_none()
            && slot < r.inventory.len()
            && a.throttle == 0.
            && !a.fire
            && r.speed.abs() < 0.01
        {
            let kind = &r.inventory[slot].kind;
            let seconds = match kind.as_str() {
                "repair_pack" | "shield_cell" => 2,
                "medkit" => 5,
                _ => 1,
            };
            r.channel = Some(Channel {
                kind: kind.clone(),
                until: tick + seconds * 20,
                slot,
            });
        }
        if let Some(id) = &a.transit
            && r.transit_channel.is_none()
            && *r.cooldowns.get("transit").unwrap_or(&0) <= tick
            && r.speed.abs() < 0.01
            && let Some(t) = self
                .world
                .transit
                .iter()
                .find(|t| &t.id == id && distance(r.x, r.y, t.x, t.y) < 35.)
            && distance(t.x, t.y, zone.x, zone.y) <= zone.radius
            && distance(t.target_x, t.target_y, zone.x, zone.y) <= zone.radius
        {
            r.transit_channel = Some((id.clone(), tick + 40));
        }
        if let Some((id, until)) = r.transit_channel.clone()
            && tick >= until
        {
            r.transit_channel = None;
            if let Some(t) = self.world.transit.iter().find(|t| t.id == id) {
                let clear = self.world.clear(t.target_x, t.target_y, RADIUS)
                    && distance(t.x, t.y, zone.x, zone.y) <= zone.radius
                    && distance(t.target_x, t.target_y, zone.x, zone.y) <= zone.radius;
                let (x, y) = (t.target_x, t.target_y);
                if clear
                    && !self
                        .robots
                        .iter()
                        .enumerate()
                        .any(|(j, v)| j != i && v.alive && distance(v.x, v.y, x, y) < RADIUS * 2.)
                {
                    self.robots[i].x = x;
                    self.robots[i].y = y;
                    self.robots[i]
                        .cooldowns
                        .insert("transit".into(), tick + 200);
                    self.event(
                        "teleport",
                        Some(self.robots[i].robot_id.clone()),
                        None,
                        None,
                        None,
                    );
                }
            }
        }
    }
    fn complete_channel(&mut self, i: usize) {
        let tick = self.tick;
        let zone = self.zone();
        let r = &mut self.robots[i];
        if r.hp <= 0. || r.last_damage_tick == Some(tick) {
            return;
        }
        if let Some(c) = r.channel.clone()
            && tick >= c.until
        {
            r.channel = None;
            if c.slot >= r.inventory.len() || r.inventory[c.slot].kind != c.kind {
                return;
            }
            if zone.stage >= self.world.zones.len()
                && distance(r.x, r.y, zone.x, zone.y) > zone.radius
                && (c.kind == "repair_pack" || c.kind == "medkit")
            {
                r.action_results.push("HEALING_BLOCKED_BY_ZONE".into());
                return;
            }
            match c.kind.as_str() {
                "repair_pack" => r.hp = (r.hp + 25.).min(r.max_hp),
                "medkit" => r.hp = (r.hp + 60.).min(r.max_hp),
                "shield_cell" => r.shield = (r.shield + 25.).min(r.max_shield),
                "energy_cell" => r.energy = (r.energy + 40.).min(r.max_energy),
                "cleanser" => {
                    r.burn_until = 0;
                    r.slow_until = 0;
                    r.emp_until = 0;
                }
                "utility_refill" => {
                    if let Some((_, n)) = r.charges.iter_mut().find(|(k, n)| {
                        (k.as_str() == "mine_dispenser" || k.as_str() == "smoke_projector")
                            && **n < 3
                    }) {
                        *n += 1;
                    } else {
                        return;
                    }
                }
                _ => return,
            }
            r.inventory[c.slot].count -= 1;
            if r.inventory[c.slot].count == 0 {
                r.inventory.remove(c.slot);
            }
        }
    }
    pub(crate) fn module_stats(r: &mut Robot) {
        r.max_hp = catalog::chassis(&r.loadout.chassis).unwrap().hp
            + if r.has("reinforced_plating") { 20. } else { 0. };
        r.vision_range = if r.has("optics") { 900. } else { 600. };
        r.max_energy = if r.has("capacitor") { 130. } else { 100. };
        r.max_shield = if r.has("shield_reservoir") { 75. } else { 50. };
        r.hp = r.hp.min(r.max_hp);
        r.shield = r.shield.min(r.max_shield);
        r.energy = r.energy.min(r.max_energy);
    }
}
