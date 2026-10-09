//! Behaviour metrics for server bots. They catch "spins in place" and
//! "never engages" regressions that rule tests cannot see.
use arena_engine::{model::*, simulation::Arena, world::distance};
use serde_json::json;
use std::collections::BTreeMap;

pub struct Metrics {
    pub decisions: usize,
    pub spinning: usize,
    pub reversals: usize,
    pub travelled: f64,
    pub damage: f64,
    pub labels: BTreeMap<String, usize>,
    /// Robot-ticks spent with the centre inside a wall's radius band.
    pub embedded: usize,
}

pub fn run(mode: &str, capacity: usize, width: f64, height: f64, ticks: u32, seed: u64) -> Metrics {
    let c: Config = serde_json::from_value(json!({"matchId":"bots","mode":mode,"capacity":capacity,"width":width,"height":height,"durationSeconds":600,"seed":seed,"robots":[]})).unwrap();
    let mut a = Arena::new(c).unwrap();
    let mut m = Metrics {
        decisions: 0,
        spinning: 0,
        reversals: 0,
        travelled: 0.,
        damage: 0.,
        labels: BTreeMap::new(),
        embedded: 0,
    };
    let mut last: BTreeMap<String, (f64, f64, f64)> = BTreeMap::new();
    for _ in 0..ticks {
        a.step(BTreeMap::new(), &[]).unwrap();
        if a.finished {
            break;
        }
        if !a.tick.is_multiple_of(2) {
            continue;
        }
        for r in a.robots.iter().filter(|r| r.alive) {
            if !a.world.clear(r.x, r.y, 16.5) {
                m.embedded += 1;
            }
            let Some(action) = a.bot_intents.get(&r.robot_id) else {
                continue;
            };
            m.decisions += 1;
            *m.labels
                .entry(action.label.clone().unwrap_or_default())
                .or_default() += 1;
            // Turning hard without moving: the "rotating in a loop" symptom.
            if action.throttle.abs() < 0.05 && action.turn.abs() > 0.5 && action.consume.is_none() {
                m.spinning += 1;
            }
            if let Some((x, y, turn)) = last.get(&r.robot_id) {
                m.travelled += distance(*x, *y, r.x, r.y);
                if turn * action.turn < -0.25 {
                    m.reversals += 1;
                }
            }
            last.insert(r.robot_id.clone(), (r.x, r.y, action.turn));
        }
    }
    m.damage = a.robots.iter().map(|r| r.damage_dealt).sum();
    m
}

fn report(name: &str, m: &Metrics) {
    let pct = |n: usize| 100. * n as f64 / m.decisions.max(1) as f64;
    println!(
        "{name}: decisions {} spinning {:.1}% turn-reversals {:.1}% travelled/decision {:.1} damage {:.0} embedded {}",
        m.decisions,
        pct(m.spinning),
        pct(m.reversals),
        m.travelled / m.decisions.max(1) as f64,
        m.damage,
        m.embedded
    );
    let mut labels: Vec<_> = m.labels.iter().collect();
    labels.sort_by(|a, b| b.1.cmp(a.1));
    println!("  labels {:?}", &labels[..labels.len().min(8)]);
}

#[test]
fn bots_move_and_fight_instead_of_spinning() {
    // Damage floors sit at roughly half of what the tuned bots deal, so a
    // regression to passive or wall-grinding bots fails loudly.
    for (name, mode, cap, w, h, min_damage) in [
        ("sandbox 8", "sandbox", 8, 2400., 1500., 500.),
        ("duel", "quick-duel", 2, 2400., 1500., 100.),
        ("br 32", "br-solo", 32, 24000., 15000., 1000.),
    ] {
        let m = run(mode, cap, w, h, 1200, 7);
        report(name, &m);
        let spinning = m.spinning as f64 / m.decisions.max(1) as f64;
        assert!(
            spinning < 0.15,
            "{name}: bots spin in place {:.0}% of decisions",
            spinning * 100.
        );
        assert!(
            m.travelled / m.decisions.max(1) as f64 > 4.,
            "{name}: bots barely move"
        );
        let grinding = ["BLOCKED_SLIDE", "UNSTUCK"]
            .iter()
            .map(|l| m.labels.get(*l).copied().unwrap_or(0))
            .sum::<usize>() as f64
            / m.decisions.max(1) as f64;
        assert!(
            grinding < 0.3,
            "{name}: bots grind cover {:.0}% of decisions",
            grinding * 100.
        );
        assert!(
            m.damage >= min_damage,
            "{name}: bots barely fight ({:.0} damage)",
            m.damage
        );
    }
}
