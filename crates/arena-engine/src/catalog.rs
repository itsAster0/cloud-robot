use serde::{Deserialize, Serialize};

pub const VERSION: &str = "0.4.0";
pub const BUDGET: u32 = 60;
pub const CANNON_KNOCKBACK: f64 = 32.;
#[derive(Clone, Debug, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Loadout {
    pub chassis: String,
    pub weapon: String,
    #[serde(default)]
    pub modules: Vec<String>,
    #[serde(default)]
    pub utilities: Vec<String>,
}
impl Default for Loadout {
    fn default() -> Self {
        Self {
            chassis: "generalist".into(),
            weapon: "plasma".into(),
            modules: vec![],
            utilities: vec![],
        }
    }
}
#[derive(Clone, Copy, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Chassis {
    pub id: &'static str,
    pub cost: u32,
    pub hp: f64,
    pub speed: f64,
}
pub const CHASSIS: [Chassis; 3] = [
    Chassis {
        id: "scout",
        cost: 10,
        hp: 80.,
        speed: 100.,
    },
    Chassis {
        id: "generalist",
        cost: 15,
        hp: 100.,
        speed: 80.,
    },
    Chassis {
        id: "heavy",
        cost: 25,
        hp: 140.,
        speed: 60.,
    },
];
#[derive(Clone, Copy, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Weapon {
    pub id: &'static str,
    pub cost: u32,
    pub damage: f64,
    pub interval: f64,
    pub range: f64,
    pub heat: f64,
    pub speed: f64,
    pub pellets: usize,
}
pub const WEAPONS: [Weapon; 9] = [
    Weapon {
        id: "plasma",
        cost: 10,
        damage: 20.,
        interval: 0.5,
        range: 650.,
        heat: 12.,
        speed: 480.,
        pellets: 1,
    },
    Weapon {
        id: "machine_gun",
        cost: 15,
        damage: 6.,
        interval: 0.12,
        range: 450.,
        heat: 5.,
        speed: 640.,
        pellets: 1,
    },
    Weapon {
        id: "shotgun",
        cost: 15,
        damage: 6.,
        interval: 0.9,
        range: 250.,
        heat: 24.,
        speed: 600.,
        pellets: 6,
    },
    Weapon {
        id: "cannon",
        cost: 20,
        damage: 45.,
        interval: 1.4,
        range: 700.,
        heat: 30.,
        speed: 280.,
        pellets: 1,
    },
    Weapon {
        id: "railgun",
        cost: 25,
        damage: 55.,
        interval: 2.2,
        range: 1000.,
        heat: 45.,
        speed: 0.,
        pellets: 1,
    },
    Weapon {
        id: "grenade",
        cost: 20,
        damage: 40.,
        interval: 1.6,
        range: 500.,
        heat: 30.,
        speed: 200.,
        pellets: 1,
    },
    Weapon {
        id: "incendiary",
        cost: 15,
        damage: 10.,
        interval: 0.9,
        range: 450.,
        heat: 18.,
        speed: 400.,
        pellets: 1,
    },
    Weapon {
        id: "cryo",
        cost: 15,
        damage: 12.,
        interval: 0.9,
        range: 450.,
        heat: 18.,
        speed: 400.,
        pellets: 1,
    },
    Weapon {
        id: "emp",
        cost: 20,
        damage: 5.,
        interval: 1.5,
        range: 500.,
        heat: 25.,
        speed: 360.,
        pellets: 1,
    },
];
pub const MODULES: [&str; 6] = [
    "reinforced_plating",
    "optics",
    "capacitor",
    "cooling_system",
    "mobility_tuning",
    "shield_reservoir",
];
pub const UTILITIES: [(&str, u32); 4] = [
    ("cloak_emitter", 15),
    ("mine_dispenser", 10),
    ("smoke_projector", 10),
    ("repair_field", 15),
];
pub const CONSUMABLES: [&str; 6] = [
    "repair_pack",
    "medkit",
    "shield_cell",
    "energy_cell",
    "cleanser",
    "utility_refill",
];
pub fn weapon(id: &str) -> Option<Weapon> {
    WEAPONS.iter().find(|w| w.id == id).copied()
}
pub fn chassis(id: &str) -> Option<Chassis> {
    CHASSIS.iter().find(|w| w.id == id).copied()
}
pub fn validate(l: &Loadout) -> Result<u32, String> {
    let mut cost = chassis(&l.chassis).ok_or("unknown chassis")?.cost
        + weapon(&l.weapon).ok_or("unknown weapon")?.cost;
    if l.modules.len() > 2 || l.utilities.len() > 2 {
        return Err("at most two modules and two utilities".into());
    }
    for (i, m) in l.modules.iter().enumerate() {
        if !MODULES.contains(&m.as_str()) || l.modules[..i].contains(m) {
            return Err("unknown or duplicate module".into());
        }
        cost += 10;
    }
    for (i, u) in l.utilities.iter().enumerate() {
        if l.utilities[..i].contains(u) {
            return Err("duplicate utility".into());
        }
        cost += UTILITIES
            .iter()
            .find(|(id, _)| id == u)
            .ok_or("unknown utility")?
            .1;
    }
    if cost > BUDGET {
        return Err(format!("build costs {cost}; maximum is {BUDGET}"));
    }
    Ok(cost)
}
pub fn catalogue() -> serde_json::Value {
    serde_json::json!({"version":VERSION,"budget":BUDGET,"chassis":CHASSIS,"weapons":WEAPONS,"modules":MODULES,"utilities":UTILITIES,"consumables":CONSUMABLES})
}
