use crate::catalog::Loadout;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
pub const DT: f64 = 0.05;
pub const MAX_ROBOTS: usize = 256;
pub const MAX_PROJECTILES: usize = 8192;
pub const MAX_MINES: usize = 768;
pub const MAX_FIELDS: usize = 512;
pub const MAX_CONTAINERS: usize = 4096;
pub const RADIUS: f64 = 14.;
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Config {
    pub match_id: String,
    #[serde(default = "default_mode")]
    pub mode: String,
    #[serde(default = "default_width")]
    pub width: f64,
    #[serde(default = "default_height")]
    pub height: f64,
    #[serde(default = "default_capacity")]
    pub capacity: usize,
    #[serde(default = "default_duration")]
    pub duration_seconds: u32,
    #[serde(default)]
    pub seed: u64,
    #[serde(default)]
    pub friendly_fire: bool,
    #[serde(default)]
    pub live_edit: bool,
    /// Explicit site count. 0 means auto: 4 on small arenas, 64 on large.
    /// Range 0..=256 keeps generation bounded for 256-robot matches.
    #[serde(default)]
    pub site_count: usize,
    /// Cover pieces per site. Default 4, range 0..=12.
    #[serde(default)]
    pub cover_per_site: usize,
    /// Loot containers per site. Default 16, range 0..=32.
    #[serde(default)]
    pub loot_per_site: usize,
    #[serde(default)]
    pub robots: Vec<Registration>,
}
fn default_mode() -> String {
    "br-solo".into()
}
fn default_width() -> f64 {
    42000.
}
fn default_height() -> f64 {
    26250.
}
fn default_capacity() -> usize {
    256
}
fn default_duration() -> u32 {
    1080
}
impl Config {
    pub fn validate(&self) -> Result<(), String> {
        if !["br-solo", "br-squad", "sandbox", "quick-duel"].contains(&self.mode.as_str()) {
            return Err("unknown v4 mode".into());
        }
        if self.match_id.is_empty() || self.match_id.len() > 128 {
            return Err("invalid match id".into());
        }
        if !self.width.is_finite()
            || !self.height.is_finite()
            || !(1200. ..=48000.).contains(&self.width)
            || !(750. ..=30000.).contains(&self.height)
        {
            return Err("dimensions must be 1200..48000 by 750..30000".into());
        }
        if self.capacity == 0 || self.capacity > MAX_ROBOTS || self.robots.len() > self.capacity {
            return Err("capacity must be 1..256".into());
        }
        if !(10..=2700).contains(&self.duration_seconds) {
            return Err("duration must be 10..2700 seconds".into());
        }
        if self.site_count > 256 {
            return Err("site count must be 0..256".into());
        }
        if self.cover_per_site > 12 {
            return Err("cover per site must be 0..12".into());
        }
        if self.loot_per_site > 32 {
            return Err("loot per site must be 0..32".into());
        }
        if self.mode == "br-squad" && !self.capacity.is_multiple_of(4) {
            return Err("squad capacity must be divisible by four".into());
        }
        let mut ids = std::collections::BTreeSet::new();
        let mut teams = BTreeMap::<String, usize>::new();
        for r in &self.robots {
            if r.robot_id.is_empty()
                || r.robot_id.len() > 128
                || !ids.insert(&r.robot_id)
                || r.name.len() > 80
                || r.team.len() > 128
            {
                return Err("invalid or duplicate robot".into());
            }
            crate::catalog::validate(&r.loadout)?;
            if self.mode == "br-squad" {
                if r.team.is_empty() {
                    return Err("squad team required".into());
                }
                let n = teams.entry(r.team.clone()).or_default();
                *n += 1;
                if *n > 4 {
                    return Err("squad is full".into());
                }
            }
        }
        Ok(())
    }
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Registration {
    pub robot_id: String,
    pub name: String,
    #[serde(default)]
    pub team: String,
    #[serde(default)]
    pub bot: bool,
    #[serde(default)]
    pub loadout: Loadout,
}
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Action {
    #[serde(default)]
    pub throttle: f64,
    #[serde(default)]
    pub brake: bool,
    #[serde(default)]
    pub turn: f64,
    #[serde(default)]
    pub aim: Option<f64>,
    #[serde(default)]
    pub fire: bool,
    #[serde(default)]
    pub cruise: bool,
    #[serde(default)]
    pub dash: bool,
    #[serde(default)]
    pub scan: bool,
    #[serde(default)]
    pub weapon: Option<usize>,
    #[serde(default)]
    pub utility: Option<String>,
    #[serde(default)]
    pub consume: Option<usize>,
    #[serde(default)]
    pub pickup: Option<String>,
    #[serde(default)]
    pub drop_slot: Option<usize>,
    #[serde(default)]
    pub equip: Option<Equip>,
    #[serde(default)]
    pub drop_equipment: Option<EquipmentSlot>,
    #[serde(default)]
    pub pickup_priorities: Option<Vec<String>>,
    #[serde(default)]
    pub transit: Option<String>,
    #[serde(default)]
    pub message: Option<String>,
    #[serde(default)]
    pub label: Option<String>,
}
impl Action {
    pub fn continuous(&self) -> Self {
        Self {
            throttle: self.throttle,
            brake: self.brake,
            turn: self.turn,
            aim: self.aim,
            fire: self.fire,
            cruise: self.cruise,
            label: self.label.clone(),
            ..Self::default()
        }
    }
    pub fn validate(&self) -> Result<(), String> {
        if !self.throttle.is_finite()
            || !(-1. ..=1.).contains(&self.throttle)
            || !self.turn.is_finite()
            || !(-1. ..=1.).contains(&self.turn)
            || self.aim.is_some_and(|a| !a.is_finite())
            || self.message.as_ref().is_some_and(|s| s.len() > 128)
            || self.label.as_ref().is_some_and(|s| s.len() > 64)
            || self
                .equip
                .as_ref()
                .is_some_and(|e| e.slot > 1 || e.kind.len() > 128 || e.container.len() > 128)
            || self.drop_equipment.as_ref().is_some_and(|e| {
                e.slot > 1 || !["weapon", "module", "utility"].contains(&e.group.as_str())
            })
            || self
                .pickup_priorities
                .as_ref()
                .is_some_and(|p| p.len() > 32 || p.iter().any(|s| s.len() > 128))
        {
            Err("invalid action".into())
        } else {
            Ok(())
        }
    }
}
#[derive(Clone, Serialize, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct WeaponSlot {
    pub kind: String,
    pub heat: f64,
    pub ready_at: u32,
    pub overheated: bool,
    pub charge_until: Option<u32>,
}
#[derive(Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Stack {
    pub kind: String,
    pub count: u32,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub weapon_state: Option<WeaponSlot>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub charges: Option<u32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ready_at: Option<u32>,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Channel {
    pub kind: String,
    pub until: u32,
    pub slot: usize,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Robot {
    pub robot_id: String,
    pub name: String,
    pub team: String,
    pub bot: bool,
    pub x: f64,
    pub y: f64,
    pub heading: f64,
    pub turret_heading: f64,
    pub vx: f64,
    pub vy: f64,
    pub speed: f64,
    pub hp: f64,
    pub max_hp: f64,
    pub shield: f64,
    pub max_shield: f64,
    pub energy: f64,
    pub max_energy: f64,
    pub vision_range: f64,
    pub alive: bool,
    pub loadout: Loadout,
    pub weapons: Vec<WeaponSlot>,
    pub active_weapon: usize,
    pub inventory: Vec<Stack>,
    pub pickup_priorities: Vec<String>,
    pub charges: BTreeMap<String, u32>,
    pub cooldowns: BTreeMap<String, u32>,
    pub burn_until: u32,
    pub slow_until: u32,
    pub emp_until: u32,
    pub cloak_until: u32,
    pub cruise: bool,
    pub channel: Option<Channel>,
    pub damage_dealt: f64,
    pub damage_taken: f64,
    pub kills: u32,
    pub placement: Option<usize>,
    pub messages: Vec<String>,
    pub action_results: Vec<String>,
    pub last_action: String,
    pub last_damage_tick: Option<u32>,
    pub transit_channel: Option<(String, u32)>,
}
impl Robot {
    pub fn has(&self, m: &str) -> bool {
        self.loadout.modules.iter().any(|s| s == m)
    }
    pub fn weapon(&self) -> &str {
        &self.weapons[self.active_weapon].kind
    }
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Obstacle {
    pub id: String,
    pub shape: String,
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
    /// Visual theme only: wall, hedge, glass, or rock. Collision and vision
    /// treat every material identically.
    #[serde(default = "default_material")]
    pub material: String,
}
fn default_material() -> String {
    "wall".into()
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Projectile {
    pub projectile_id: String,
    pub owner_id: String,
    pub team: String,
    pub kind: String,
    pub x: f64,
    pub y: f64,
    pub vx: f64,
    pub vy: f64,
    pub damage: f64,
    pub remaining: f64,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Container {
    pub item_id: String,
    pub x: f64,
    pub y: f64,
    pub contents: Vec<Stack>,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Site {
    pub id: String,
    pub kind: String,
    pub x: f64,
    pub y: f64,
    /// District theme around the site: urban, industrial, forest, or desert.
    /// Selects ground textures and structure styles; rules ignore it.
    #[serde(default)]
    pub biome: String,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Transit {
    pub id: String,
    pub x: f64,
    pub y: f64,
    pub target_x: f64,
    pub target_y: f64,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Mine {
    pub mine_id: String,
    pub owner_id: String,
    pub team: String,
    pub x: f64,
    pub y: f64,
    pub arm_tick: u32,
    pub end_tick: u32,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Field {
    pub id: String,
    pub kind: String,
    pub owner_id: String,
    pub team: String,
    pub x: f64,
    pub y: f64,
    pub end_tick: u32,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Event {
    pub tick: u32,
    pub r#type: String,
    pub robot_id: Option<String>,
    pub target_id: Option<String>,
    pub x: Option<f64>,
    pub y: Option<f64>,
    pub damage: Option<f64>,
    pub message: Option<String>,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Zone {
    pub active: bool,
    pub x: f64,
    pub y: f64,
    pub radius: f64,
    pub damage: f64,
    pub stage: usize,
}

#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Hazard {
    pub id: String,
    pub kind: String,
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
    pub damage_per_second: f64,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ZonePhase {
    pub start_tick: u32,
    pub hold_ticks: u32,
    pub end_tick: u32,
    pub x: f64,
    pub y: f64,
    pub radius: f64,
    pub damage: f64,
}

#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Equip {
    pub container: String,
    pub kind: String,
    pub slot: usize,
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct EquipmentSlot {
    pub group: String,
    pub slot: usize,
}
