use crate::model::*;
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, BTreeSet};

#[derive(Clone, Default)]
pub struct Grid {
    cells: BTreeMap<(i32, i32), Vec<usize>>,
}
impl Grid {
    pub fn insert(&mut self, id: usize, x: f64, y: f64, w: f64, h: f64) {
        for cx in (x / 256.).floor() as i32..=((x + w) / 256.).floor() as i32 {
            for cy in (y / 256.).floor() as i32..=((y + h) / 256.).floor() as i32 {
                self.cells.entry((cx, cy)).or_default().push(id);
            }
        }
    }
    pub fn query(&self, x: f64, y: f64, w: f64, h: f64) -> Vec<usize> {
        let mut ids = BTreeSet::new();
        for cx in (x / 256.).floor() as i32..=((x + w) / 256.).floor() as i32 {
            for cy in (y / 256.).floor() as i32..=((y + h) / 256.).floor() as i32 {
                if let Some(c) = self.cells.get(&(cx, cy)) {
                    ids.extend(c);
                }
            }
        }
        ids.into_iter().collect()
    }
}
#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct World {
    pub revision: u32,
    pub hazards: Vec<Hazard>,
    pub zones: Vec<ZonePhase>,
    pub sites: Vec<Site>,
    pub obstacles: Vec<Obstacle>,
    pub transit: Vec<Transit>,
    pub containers: Vec<Container>,
    #[serde(skip)]
    pub grid: Grid,
}
impl World {
    pub fn can_add_container(&self, x: f64, y: f64) -> bool {
        let chunk = ((x / 1024.).floor() as i32, (y / 1024.).floor() as i32);
        self.containers.len() < MAX_CONTAINERS
            && self
                .containers
                .iter()
                .filter(|c| ((c.x / 1024.).floor() as i32, (c.y / 1024.).floor() as i32) == chunk)
                .take(64)
                .count()
                < 64
    }
    pub fn generate(c: &Config) -> Self {
        let mut world = Self {
            revision: 1,
            hazards: vec![],
            zones: vec![],
            sites: vec![],
            obstacles: vec![],
            transit: vec![],
            containers: vec![],
            grid: Grid::default(),
        };
        let count = if c.site_count > 0 {
            c.site_count
        } else if c.width < 6000. {
            4
        } else {
            64
        };
        let cover_per_site = if c.cover_per_site == 0 && c.site_count == 0 {
            8
        } else {
            c.cover_per_site.min(12)
        };
        // cover_per_site == 0 is a legal explicit choice (open ground), so
        // only the auto path above substitutes the default.
        let loot_per_site = if c.loot_per_site == 0 && c.site_count == 0 {
            16
        } else {
            c.loot_per_site.min(32)
        };
        let cols = (count as f64).sqrt().ceil() as usize;
        let kinds = [
            "repair_depot",
            "armoury",
            "relay_station",
            "transit_station",
            "bunker",
            "exposed_supply",
        ];
        let mut rng = Rng(c.seed | 1);
        for i in 0..count {
            let x = (i % cols) as f64 * c.width / cols as f64 + c.width / cols as f64 / 2.;
            let y = (i / cols) as f64 * c.height / cols as f64 + c.height / cols as f64 / 2.;
            world.sites.push(Site {
                id: format!("site-{i}"),
                kind: kinds[i % 6].into(),
                x,
                y,
            });
            // Two slotted rings: 4 inner slots at 90 deg, 8 outer slots at
            // 45 deg offset by half a step. Fixed slots with capped segment
            // lengths guarantee 90+ unit gaps against the 64-unit navigation
            // grid, so cover reads dense without ever ringing a site shut.
            // Variant and material cycle together so walls, hedgerows, glass
            // panes, pillars, and rock read as distinct structures.
            // Counts stay exact and deterministic.
            let outer = cover_per_site.saturating_sub(4);
            for j in 0..cover_per_site {
                let (radius, angle, size, mat): (f64, f64, (f64, f64), &str) = if j < 4 {
                    let a = i as f64 * std::f64::consts::FRAC_PI_8
                        + j as f64 * std::f64::consts::FRAC_PI_2;
                    let (s, m) = match (i + j) % 4 {
                        0 => ((130., 34.), "wall"),
                        1 => ((90., 90.), "rock"),
                        2 => ((220., 24.), "hedge"),
                        _ => ((70., 70.), "wall"),
                    };
                    (170., a, s, m)
                } else {
                    // Spread outer pieces evenly across the 8 slots.
                    let k = (j - 4) * 8 / outer.max(1);
                    let a = i as f64 * std::f64::consts::FRAC_PI_8
                        + std::f64::consts::FRAC_PI_8
                        + k as f64 * std::f64::consts::FRAC_PI_4;
                    let (s, m) = match (i + j) % 5 {
                        0 => ((160., 32.), "wall"),
                        1 => ((70., 120.), "rock"),
                        2 => ((95., 95.), "wall"),
                        3 => ((200., 28.), "glass"),
                        _ => ((110., 60.), "hedge"),
                    };
                    (330., a, s, m)
                };
                let jitter = 0.85 + (rng.next_u64() % 30) as f64 / 100.;
                let wj = size.0 * jitter;
                let hj = size.1 * jitter;
                // Tangential orientation keeps lanes walkable past each piece.
                let (sin, cos) = angle.sin_cos();
                let (qw, qh) = (
                    wj * cos.abs() + hj * sin.abs(),
                    wj * sin.abs() + hj * cos.abs(),
                );
                let px = x + cos * radius;
                let py = y + sin * radius;
                let margin = 20.;
                world.obstacles.push(Obstacle {
                    id: format!("cover-{i}-{j}"),
                    shape: "aabb".into(),
                    x: (px - qw / 2.).clamp(margin, (c.width - margin - qw).max(margin)),
                    y: (py - qh / 2.).clamp(margin, (c.height - margin - qh).max(margin)),
                    width: qw,
                    height: qh,
                    material: mat.into(),
                });
            }
            for j in 0..loot_per_site {
                let spread = loot_per_site.max(1) as f64;
                let angle = j as f64 * std::f64::consts::TAU / spread;
                let radius = 40. + (rng.next_u64() % 35) as f64;
                let kind = match i % 6 {
                    0 => [
                        "repair_pack",
                        "medkit",
                        "shield_cell",
                        "utility:repair_field",
                    ][j % 4],
                    1 => ["weapon:machine_gun", "weapon:shotgun", "cooling_system"][j % 3],
                    2 => ["optics", "energy_cell", "capacitor"][j % 3],
                    3 => ["energy_cell", "mobility_tuning", "utility_refill"][j % 3],
                    4 => [
                        "reinforced_plating",
                        "weapon:shotgun",
                        "utility:mine_dispenser",
                        "utility:smoke_projector",
                    ][j % 4],
                    _ => ["weapon:railgun", "weapon:grenade", "cleanser"][j % 3],
                };
                world.containers.push(Container {
                    item_id: format!("loot-{i}-{j}"),
                    x: x + angle.cos() * radius,
                    y: y + angle.sin() * radius,
                    contents: vec![Stack {
                        kind: if j < 4 {
                            "repair_pack".into()
                        } else {
                            kind.into()
                        },
                        count: 1,
                        ..Default::default()
                    }],
                });
            }
        }
        // Wild scatter fills the open ground between sites so the map reads
        // dense everywhere, not just at anchors. Small open pieces only:
        // pillars, short walls, and blocks with gaps bots stroll through.
        // Skipped entirely when cover is explicitly zero (open ground).
        if cover_per_site > 0 {
            for i in 0..count {
                let (ax, ay, bx, by) = {
                    let a = &world.sites[i];
                    let b = &world.sites[(i + 1) % count];
                    (a.x, a.y, b.x, b.y)
                };
                let jx = (rng.next_u64() % 400) as f64 - 200.;
                let jy = (rng.next_u64() % 400) as f64 - 200.;
                let cx = (ax + bx) / 2. + jx;
                let cy = (ay + by) / 2. + jy;
                let rot = (rng.next_u64() % 360) as f64 * std::f64::consts::PI / 180.;
                let (rsin, rcos) = rot.sin_cos();
                // Five spread pieces per gap: rock pillar, glass pane,
                // hedgerow, block, and rubble — 200+ units apart.
                let pieces = [
                    (0., 0., 80., 80., "rock"),
                    (280., 90., 170., 30., "wall"),
                    (-90., 280., 60., 130., "wall"),
                    (320., 320., 200., 26., "hedge"),
                    (-260., 120., 150., 30., "glass"),
                ];
                for (j, (dx, dy, w, h, mat)) in pieces.iter().enumerate() {
                    let margin = 20.;
                    world.obstacles.push(Obstacle {
                        id: format!("wild-{i}-{j}"),
                        shape: "aabb".into(),
                        x: (cx + dx * rcos - dy * rsin - w / 2.)
                            .clamp(margin, (c.width - margin - w).max(margin)),
                        y: (cy + dx * rsin + dy * rcos - h / 2.)
                            .clamp(margin, (c.height - margin - h).max(margin)),
                        width: *w,
                        height: *h,
                        material: (*mat).into(),
                    });
                }
            }
        }
        for (i, s) in world.sites.iter().enumerate() {
            let target = &world.sites[(i + 1) % world.sites.len()];
            world.transit.push(Transit {
                id: format!("transit-{i}"),
                x: s.x,
                y: s.y,
                target_x: target.x,
                target_y: target.y,
            });
        }
        // Hazard fields sit between sites, never on them: slow patches bog
        // robots down, slag patches burn. Midpoints keep them clear of spawn
        // ground while shaping routes between structures.
        let hazard_count = (count / 6).clamp(2, 24);
        for k in 0..hazard_count {
            let a = &world.sites[k % count];
            let b = &world.sites[(k + 1) % count];
            let jx = (rng.next_u64() % 200) as f64 - 100.;
            let jy = (rng.next_u64() % 200) as f64 - 100.;
            let w = 260. + (rng.next_u64() % 160) as f64;
            let h = 160. + (rng.next_u64() % 120) as f64;
            let slow = k % 3 != 2;
            world.hazards.push(Hazard {
                id: format!("hazard-{k}"),
                kind: if slow { "slow".into() } else { "slag".into() },
                x: ((a.x + b.x) / 2. + jx - w / 2.).clamp(20., (c.width - 20. - w).max(20.)),
                y: ((a.y + b.y) / 2. + jy - h / 2.).clamp(20., (c.height - 20. - h).max(20.)),
                width: w,
                height: h,
                damage_per_second: if slow { 0. } else { 4. },
            });
        }
        let r0 = c.width.hypot(c.height) / 2.;
        for (i, (start, end, factor)) in [
            (180., 360., 0.62),
            (360., 600., 0.30),
            (600., 840., 0.10),
            (840., 1020., 0.018),
        ]
        .iter()
        .enumerate()
        {
            let scale = c.duration_seconds as f64 * 20. / 1080.;
            world.zones.push(ZonePhase {
                start_tick: (start * scale) as u32,
                hold_ticks: ((end - start) * 0.25 * scale) as u32,
                end_tick: (end * scale) as u32,
                x: c.width / 2.,
                y: c.height / 2.,
                radius: (r0 * factor).max(180.),
                damage: [2., 4., 8., 16.][i],
            });
        }
        world.reindex();
        world
    }
    pub fn reindex(&mut self) {
        self.grid = Grid::default();
        for (i, o) in self.obstacles.iter().enumerate() {
            self.grid.insert(i, o.x, o.y, o.width, o.height);
        }
    }
    pub fn clear(&self, x: f64, y: f64, r: f64) -> bool {
        self.grid
            .query(x - r, y - r, r * 2., r * 2.)
            .into_iter()
            .all(|i| {
                let o = &self.obstacles[i];
                let nx = x.clamp(o.x, o.x + o.width);
                let ny = y.clamp(o.y, o.y + o.height);
                distance(x, y, nx, ny) >= r
            })
    }
    pub fn wall_hit(&self, x: f64, y: f64, nx: f64, ny: f64, r: f64) -> Option<f64> {
        self.grid
            .query(
                x.min(nx) - r,
                y.min(ny) - r,
                (nx - x).abs() + r * 2.,
                (ny - y).abs() + r * 2.,
            )
            .into_iter()
            .filter_map(|i| segment_box(x, y, nx, ny, &self.obstacles[i], r))
            .min_by(f64::total_cmp)
    }
    pub fn los(&self, x: f64, y: f64, nx: f64, ny: f64) -> bool {
        self.wall_hit(x, y, nx, ny, 0.).is_none()
    }
}
#[derive(Clone, Serialize, Deserialize)]
pub struct Rng(pub u64);
impl Rng {
    pub fn next_u64(&mut self) -> u64 {
        self.0 ^= self.0 << 13;
        self.0 ^= self.0 >> 7;
        self.0 ^= self.0 << 17;
        self.0
    }
}
pub fn distance(x: f64, y: f64, nx: f64, ny: f64) -> f64 {
    (nx - x).hypot(ny - y)
}
pub fn heading(x: f64, y: f64, nx: f64, ny: f64) -> f64 {
    (ny - y).atan2(nx - x).to_degrees().rem_euclid(360.)
}
pub fn rotate(from: f64, to: f64, limit: f64) -> f64 {
    (from
        + (to - from + 180.)
            .rem_euclid(360.)
            .sub(180.)
            .clamp(-limit, limit))
    .rem_euclid(360.)
}
use std::ops::Sub;
pub fn segment_circle(x: f64, y: f64, nx: f64, ny: f64, cx: f64, cy: f64, r: f64) -> Option<f64> {
    let dx = nx - x;
    let dy = ny - y;
    let ox = x - cx;
    let oy = y - cy;
    let a = dx * dx + dy * dy;
    let c = ox * ox + oy * oy - r * r;
    if c <= 0. {
        return Some(0.);
    }
    if a == 0. {
        return None;
    }
    let b = 2. * (ox * dx + oy * dy);
    let disc = b * b - 4. * a * c;
    if disc < 0. {
        return None;
    }
    let t = (-b - disc.sqrt()) / (2. * a);
    if (0. ..=1.).contains(&t) {
        Some(t)
    } else {
        None
    }
}
pub fn segment_box(x: f64, y: f64, nx: f64, ny: f64, o: &Obstacle, r: f64) -> Option<f64> {
    let mut lo: f64 = 0.;
    let mut hi: f64 = 1.;
    for (p, d, min, max) in [
        (x, nx - x, o.x - r, o.x + o.width + r),
        (y, ny - y, o.y - r, o.y + o.height + r),
    ] {
        if d.abs() < 1e-12 {
            if p < min || p > max {
                return None;
            }
        } else {
            let a = (min - p) / d;
            let b = (max - p) / d;
            lo = lo.max(a.min(b));
            hi = hi.min(a.max(b));
            if lo > hi {
                return None;
            }
        }
    }
    Some(lo)
}

/// Bounded local detour search using only public geometry. None means the
/// caller must stop or select another route, rather than drive into cover.
pub fn local_waypoint(
    world: &World,
    start: (f64, f64),
    goal: (f64, f64),
    bounds: (f64, f64),
    radius: f64,
) -> Option<(f64, f64)> {
    if world
        .wall_hit(start.0, start.1, goal.0, goal.1, radius)
        .is_none()
    {
        return Some(goal);
    }
    let mut open = vec![(0_i32, 0_i32, 0_u32, start)];
    let mut seen = std::collections::BTreeSet::from([(0_i32, 0_i32)]);
    for _ in 0..128 {
        let best = open
            .iter()
            .enumerate()
            .min_by(|(_, a), (_, b)| {
                let score = |n: &(i32, i32, u32, (f64, f64))| {
                    n.2 as f64 * 64.
                        + distance(
                            start.0 + n.0 as f64 * 64.,
                            start.1 + n.1 as f64 * 64.,
                            goal.0,
                            goal.1,
                        )
                };
                score(a).total_cmp(&score(b))
            })
            .map(|(i, _)| i)?;
        let (x, y, cost, first) = open.remove(best);
        let p = (start.0 + x as f64 * 64., start.1 + y as f64 * 64.);
        if world.wall_hit(p.0, p.1, goal.0, goal.1, radius).is_none() {
            return Some(first);
        }
        for (dx, dy) in [(1, 0), (0, 1), (-1, 0), (0, -1)] {
            let cell = (x + dx, y + dy);
            let q = (start.0 + cell.0 as f64 * 64., start.1 + cell.1 as f64 * 64.);
            if seen.contains(&cell)
                || q.0 < radius
                || q.1 < radius
                || q.0 > bounds.0 - radius
                || q.1 > bounds.1 - radius
                || world.wall_hit(p.0, p.1, q.0, q.1, radius).is_some()
            {
                continue;
            }
            seen.insert(cell);
            open.push((cell.0, cell.1, cost + 1, if cost == 0 { q } else { first }));
        }
    }
    None
}
