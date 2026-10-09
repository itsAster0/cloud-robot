use crate::{model::*, simulation::Arena, world::*};
use serde::{Deserialize, Serialize};
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Edit {
    pub expected_revision: u32,
    #[serde(default)]
    pub hazards: Vec<Hazard>,
    #[serde(default)]
    pub remove_hazards: Vec<String>,
    #[serde(default)]
    pub future_zones: Option<Vec<ZonePhase>>,
    pub effective_tick: u32,
    #[serde(default)]
    pub obstacles: Vec<Obstacle>,
    #[serde(default)]
    pub remove_obstacles: Vec<String>,
    #[serde(default)]
    pub containers: Vec<Container>,
    #[serde(default)]
    pub transit: Vec<Transit>,
}
impl Arena {
    pub fn preview_edit(&self, edit: &Edit) -> Result<serde_json::Value, String> {
        if edit.effective_tick <= self.tick {
            return Err("preview requires a future effective tick".into());
        }
        let next = self.edited(edit)?;
        Ok(
            serde_json::json!({"revision":next.world.revision,"effectiveTick":edit.effective_tick,"obstacles":edit.obstacles,"removeObstacles":edit.remove_obstacles,"containers":edit.containers,"transit":edit.transit,"hazards":edit.hazards,"removeHazards":edit.remove_hazards,"futureZones":edit.future_zones}),
        )
    }
    pub fn apply_edit(&mut self, edit: &Edit) -> Result<(), String> {
        let next = self.edited(edit)?;
        self.world = next.world;
        Ok(())
    }
    fn edited(&self, e: &Edit) -> Result<Self, String> {
        if !self.config.live_edit {
            return Err("live editing is disabled".into());
        }
        if e.expected_revision != self.world.revision {
            return Err("revision conflict".into());
        }
        if e.effective_tick < self.tick {
            return Err("effective tick has passed".into());
        }
        if e.obstacles.len()
            + e.remove_obstacles.len()
            + e.containers.len()
            + e.transit.len()
            + e.hazards.len()
            + e.remove_hazards.len()
            + usize::from(e.future_zones.is_some())
            > 64
        {
            return Err("edit has more than 64 operations".into());
        }
        let mut next = self.clone();
        for id in &e.remove_obstacles {
            if !next.world.obstacles.iter().any(|o| &o.id == id) {
                return Err("obstacle does not exist".into());
            }
            next.world.obstacles.retain(|o| &o.id != id);
        }
        for o in &e.obstacles {
            if o.id.is_empty()
                || o.id.len() > 128
                || o.shape != "aabb"
                || ![o.x, o.y, o.width, o.height].iter().all(|v| v.is_finite())
                || o.width <= 0.
                || o.height <= 0.
                || o.width > 512.
                || o.height > 512.
                || o.x < 0.
                || o.y < 0.
                || o.x + o.width > self.config.width
                || o.y + o.height > self.config.height
            {
                return Err("invalid obstacle".into());
            }
            next.world.obstacles.retain(|v| v.id != o.id);
            next.world.obstacles.push(o.clone());
        }
        if next.world.obstacles.len() > 16384 {
            return Err("obstacle budget exceeded".into());
        }
        next.world.obstacles.sort_by(|a, b| a.id.cmp(&b.id));
        next.reindex();
        for r in &next.robots {
            if r.alive && !next.world.clear(r.x, r.y, RADIUS) {
                return Err("edit overlaps robot".into());
            }
        }
        let point = |x: f64, y: f64| {
            x.is_finite()
                && y.is_finite()
                && x >= RADIUS
                && y >= RADIUS
                && x <= self.config.width - RADIUS
                && y <= self.config.height - RADIUS
        };
        for c in &e.containers {
            if c.item_id.is_empty()
                || c.item_id.len() > 128
                || !point(c.x, c.y)
                || !next.world.clear(c.x, c.y, RADIUS)
                || c.contents.len() > 12
            {
                return Err("invalid container".into());
            }
            for s in &c.contents {
                if s.weapon_state.is_some()
                    || s.charges.is_some()
                    || s.ready_at.is_some()
                    || s.count == 0
                    || s.count > 3
                    || !(crate::catalog::CONSUMABLES.contains(&s.kind.as_str())
                        || crate::catalog::MODULES.contains(&s.kind.as_str())
                        || s.kind.strip_prefix("utility:").is_some_and(|u| {
                            crate::catalog::UTILITIES.iter().any(|(id, _)| *id == u)
                        })
                        || s.kind
                            .strip_prefix("weapon:")
                            .is_some_and(|w| crate::catalog::weapon(w).is_some()))
                {
                    return Err("invalid loot".into());
                }
            }
            next.world.containers.retain(|v| v.item_id != c.item_id);
            next.world.containers.push(c.clone());
        }
        if next.world.containers.len() > MAX_CONTAINERS {
            return Err("container budget exceeded".into());
        }
        let mut chunks = std::collections::BTreeMap::new();
        for c in &next.world.containers {
            let n = chunks
                .entry(((c.x / 1024.) as i32, (c.y / 1024.) as i32))
                .or_insert(0);
            *n += 1;
            if *n > 64 {
                return Err("chunk container budget exceeded".into());
            }
        }
        for t in &e.transit {
            if t.id.is_empty()
                || t.id.len() > 128
                || !point(t.x, t.y)
                || !point(t.target_x, t.target_y)
                || !next.world.clear(t.x, t.y, RADIUS)
                || !next.world.clear(t.target_x, t.target_y, RADIUS)
            {
                return Err("invalid transit endpoint".into());
            }
            next.world.transit.retain(|v| v.id != t.id);
            next.world.transit.push(t.clone());
        }
        if next.world.transit.len() > 256 {
            return Err("transit budget exceeded".into());
        }
        // Every site and robot must still reach the open region surrounding it.
        for (x, y) in next
            .world
            .sites
            .iter()
            .map(|s| (s.x, s.y))
            .chain(next.robots.iter().filter(|r| r.alive).map(|r| (r.x, r.y)))
        {
            if !escapes(&next.world, x, y) {
                return Err("edit strands a site or robot".into());
            }
        }
        for id in &e.remove_hazards {
            next.world.hazards.retain(|h| &h.id != id);
        }
        for h in &e.hazards {
            if h.id.is_empty()
                || h.id.len() > 128
                || !["damage", "slow"].contains(&h.kind.as_str())
                || !point(h.x, h.y)
                || !h.width.is_finite()
                || !h.height.is_finite()
                || h.width <= 0.
                || h.height <= 0.
                || h.width > 1024.
                || h.height > 1024.
                || h.x + h.width > self.config.width
                || h.y + h.height > self.config.height
                || !h.damage_per_second.is_finite()
                || !(0. ..=100.).contains(&h.damage_per_second)
            {
                return Err("invalid hazard".into());
            }
            next.world.hazards.retain(|v| v.id != h.id);
            next.world.hazards.push(h.clone());
        }
        if next.world.hazards.len() > 128 {
            return Err("hazard budget exceeded".into());
        }
        if let Some(phases) = &e.future_zones {
            next.world.zones.retain(|p| p.start_tick <= self.tick);
            for p in phases {
                if p.start_tick <= self.tick {
                    return Err("only future zone phases can change".into());
                }
                next.world.zones.push(p.clone());
            }
            if next.world.zones.is_empty() || next.world.zones.len() > 8 {
                return Err("zone schedule must contain 1..8 phases".into());
            }
            let mut x = self.config.width / 2.;
            let mut y = self.config.height / 2.;
            let mut radius = self.config.width.hypot(self.config.height) / 2.;
            let mut end = 0;
            for p in &next.world.zones {
                if p.start_tick < end
                    || p.end_tick <= p.start_tick
                    || p.hold_ticks >= p.end_tick - p.start_tick
                    || p.end_tick > self.config.duration_seconds * 20
                    || !point(p.x, p.y)
                    || !p.radius.is_finite()
                    || p.radius < 80.
                    || distance(x, y, p.x, p.y) + p.radius > radius
                    || !p.damage.is_finite()
                    || !(0. ..=100.).contains(&p.damage)
                {
                    return Err("invalid or non-contained future zone".into());
                }
                x = p.x;
                y = p.y;
                radius = p.radius;
                end = p.end_tick;
            }
        }
        next.world.revision += 1;
        Ok(next)
    }
}
fn escapes(world: &World, x: f64, y: f64) -> bool {
    let mut seen = std::collections::BTreeSet::new();
    let mut queue = std::collections::VecDeque::from([(0i32, 0i32)]);
    while let Some((cx, cy)) = queue.pop_front() {
        if !seen.insert((cx, cy)) {
            continue;
        }
        if cx.abs() >= 32 || cy.abs() >= 32 {
            return true;
        }
        for (dx, dy) in [(0, 1), (1, 0), (0, -1), (-1, 0)] {
            let (nx, ny) = (cx + dx, cy + dy);
            if !seen.contains(&(nx, ny))
                && world
                    .wall_hit(
                        x + cx as f64 * 32.,
                        y + cy as f64 * 32.,
                        x + nx as f64 * 32.,
                        y + ny as f64 * 32.,
                        RADIUS,
                    )
                    .is_none()
            {
                queue.push_back((nx, ny));
            }
        }
    }
    false
}
