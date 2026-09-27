use crate::model::*;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// Uniform 256-unit spatial hash. Queries return sorted ids so callers stay
/// deterministic even though the cell map itself is unordered.
#[derive(Clone, Default)]
pub struct Grid {
    cells: HashMap<(i32, i32), Vec<usize>>,
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
        let mut ids = vec![];
        for cx in (x / 256.).floor() as i32..=((x + w) / 256.).floor() as i32 {
            for cy in (y / 256.).floor() as i32..=((y + h) / 256.).floor() as i32 {
                if let Some(c) = self.cells.get(&(cx, cy)) {
                    ids.extend_from_slice(c);
                }
            }
        }
        ids.sort_unstable();
        ids.dedup();
        ids
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
                biome: biome_at(c.seed, x, y, c.width.max(c.height) / cols as f64).into(),
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
        ensure_variety(&mut world.sites, c.seed);
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
                // Landforms follow the climate: high ground grows a mountain
                // ridge, wet low ground a lake. Both stay well clear of sites
                // so spawns, loot, and transit pads remain open.
                let spacing = c.width.max(c.height) / cols as f64;
                let clear_of_sites = world
                    .sites
                    .iter()
                    .all(|s| distance(s.x, s.y, cx, cy) > 650.);
                let high = climate(c.seed, 4, cx, cy, spacing) > 0.6;
                let wet = climate(c.seed, 2, cx, cy, spacing) > 0.56;
                if clear_of_sites && (high || wet) {
                    let pieces = if high {
                        ridge(cx, cy, rot, rng.next_u64())
                    } else {
                        lake(cx, cy, &mut rng)
                    };
                    for (j, (x, y, w, h, mat)) in pieces.into_iter().enumerate() {
                        if x < 40. || y < 40. || x + w > c.width - 40. || y + h > c.height - 40. {
                            continue;
                        }
                        world.obstacles.push(Obstacle {
                            id: format!("land-{i}-{j}"),
                            shape: "aabb".into(),
                            x,
                            y,
                            width: w,
                            height: h,
                            material: mat.into(),
                        });
                    }
                    continue;
                }
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
        // Biome ground effects: swamps bog down, snowdrifts slow, and desert
        // heat vents burn. One patch per site, off its core and streets.
        for i in 0..count {
            let (sx, sy, biome) = {
                let s = &world.sites[i];
                (s.x, s.y, s.biome.clone())
            };
            let kind = match biome.as_str() {
                "swamp" | "snow" => "slow",
                "desert" => "slag",
                _ => continue,
            };
            let (cw, ch) = (c.width / cols as f64, c.height / cols as f64);
            // Small cells have no room beside their districts; the shared
            // hazards already cover them.
            if cw.min(ch) < 1400. {
                continue;
            }
            let q = rng.next_u64() % 4;
            let px = sx + if q.is_multiple_of(2) { -1. } else { 1. } * cw * 0.3;
            let py = sy + if q < 2 { -1. } else { 1. } * ch * 0.3;
            let (w, h) = (
                240. + (rng.next_u64() % 120) as f64,
                180. + (rng.next_u64() % 80) as f64,
            );
            if distance(px, py, sx, sy) < 420. {
                continue;
            }
            world.hazards.push(Hazard {
                id: format!("biome-{i}"),
                kind: kind.into(),
                x: (px - w / 2.).clamp(20., (c.width - 20. - w).max(20.)),
                y: (py - h / 2.).clamp(20., (c.height - 20. - h).max(20.)),
                width: w,
                height: h,
                damage_per_second: if kind == "slag" { 3. } else { 0. },
            });
        }
        if cover_per_site > 0 {
            world.reindex();
            build_districts(&mut world, c, cols, cover_per_site);
        }
        // Arena mode never closes: no zone phases at all.
        if c.mode == "arena" {
            world.reindex();
            return world;
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
    /// Position moved out of any obstacle closer than `r`, or None when the
    /// point is already clear. Resolves up to four contacts, each along the
    /// normal from the nearest box point; a centre inside a box leaves
    /// through its nearest face.
    pub fn depenetrate(&self, x: f64, y: f64, r: f64) -> Option<(f64, f64)> {
        if self.clear(x, y, r) {
            return None;
        }
        let (mut x, mut y) = (x, y);
        for _ in 0..4 {
            let Some(o) = self
                .grid
                .query(x - r, y - r, r * 2., r * 2.)
                .into_iter()
                .map(|i| &self.obstacles[i])
                .find(|o| {
                    let nx = x.clamp(o.x, o.x + o.width);
                    let ny = y.clamp(o.y, o.y + o.height);
                    distance(x, y, nx, ny) < r
                })
            else {
                break;
            };
            let (nx, ny) = (x.clamp(o.x, o.x + o.width), y.clamp(o.y, o.y + o.height));
            let d = distance(x, y, nx, ny);
            if d > 1e-9 {
                let push = r - d + 0.01;
                x += (x - nx) / d * push;
                y += (y - ny) / d * push;
            } else {
                let exits = [
                    (x - o.x, -1., 0.),
                    (o.x + o.width - x, 1., 0.),
                    (y - o.y, 0., -1.),
                    (o.y + o.height - y, 0., 1.),
                ];
                let (depth, dx, dy) = exits
                    .into_iter()
                    .min_by(|a, b| a.0.total_cmp(&b.0))
                    .unwrap();
                x += dx * (depth + r + 0.01);
                y += dy * (depth + r + 0.01);
            }
        }
        Some((x, y))
    }
    /// Like `wall_hit`, for shots and sight: water blocks movement only.
    pub fn shot_hit(&self, x: f64, y: f64, nx: f64, ny: f64, r: f64) -> Option<f64> {
        self.grid
            .query(
                x.min(nx) - r,
                y.min(ny) - r,
                (nx - x).abs() + r * 2.,
                (ny - y).abs() + r * 2.,
            )
            .into_iter()
            .filter(|&i| self.obstacles[i].material != "water")
            .filter_map(|i| segment_box(x, y, nx, ny, &self.obstacles[i], r))
            .min_by(f64::total_cmp)
    }
    pub fn los(&self, x: f64, y: f64, nx: f64, ny: f64) -> bool {
        self.shot_hit(x, y, nx, ny, 0.).is_none()
    }
}
pub const BIOMES: [&str; 6] = ["urban", "industrial", "forest", "desert", "snow", "swamp"];
/// Seeded value noise in [0, 1]: hashed lattice corners blended with
/// smoothstep, so nearby points get similar values.
fn value_noise(seed: u64, x: f64, y: f64) -> f64 {
    let (x0, y0) = (x.floor(), y.floor());
    let (fx, fy) = (x - x0, y - y0);
    let corner = |dx: f64, dy: f64| {
        let (ix, iy) = ((x0 + dx) as i64 as u64, (y0 + dy) as i64 as u64);
        (mix(seed ^ mix(ix.wrapping_mul(0x1F1F_1F1F) ^ iy.rotate_left(32))) >> 11) as f64
            / (1u64 << 53) as f64
    };
    let smooth = |t: f64| t * t * (3. - 2. * t);
    let (sx, sy) = (smooth(fx), smooth(fy));
    let top = corner(0., 0.) + (corner(1., 0.) - corner(0., 0.)) * sx;
    let bottom = corner(0., 1.) + (corner(1., 1.) - corner(0., 1.)) * sx;
    top + (bottom - top) * sy
}
/// One climate channel (1 temperature, 2 moisture, 3 development,
/// 4 elevation) at a world point: two octaves over a lattice about two site
/// spacings wide, so biomes form regions spanning several sites.
pub fn climate(seed: u64, channel: u64, x: f64, y: f64, spacing: f64) -> f64 {
    let s = mix(seed ^ channel.wrapping_mul(0xA24B_AED4_963E_E407));
    let scale = spacing.max(600.) * 2.2;
    let (u, v) = (x / scale, y / scale);
    0.68 * value_noise(s, u, v) + 0.32 * value_noise(s ^ 1, u * 2.3 + 17., v * 2.3 + 5.)
}
/// Minecraft-style biome choice from temperature and moisture; development
/// splits the temperate dry band into city and industry.
pub fn biome_at(seed: u64, x: f64, y: f64, spacing: f64) -> &'static str {
    let t = climate(seed, 1, x, y, spacing);
    let m = climate(seed, 2, x, y, spacing);
    if t < 0.36 {
        "snow"
    } else if t > 0.6 && m < 0.5 {
        "desert"
    } else if m > 0.62 {
        "swamp"
    } else if m > 0.52 {
        "forest"
    } else if climate(seed, 3, x, y, spacing) > 0.5 {
        "urban"
    } else {
        "industrial"
    }
}
/// Small maps can land inside one climate region; recolour the latest sites
/// of the commonest biome so at least min(sites, 4) themes appear.
fn ensure_variety(sites: &mut [Site], seed: u64) {
    let want = sites.len().min(4);
    let mut order = BIOMES;
    let mut rng = Rng(mix(seed ^ 0xB10E) | 1);
    for n in (1..order.len()).rev() {
        order.swap(n, (rng.next_u64() % (n as u64 + 1)) as usize);
    }
    loop {
        let mut counts: Vec<(&str, usize)> = vec![];
        for s in sites.iter() {
            match counts.iter_mut().find(|(b, _)| *b == s.biome) {
                Some(entry) => entry.1 += 1,
                None => counts.push((BIOMES.iter().find(|b| **b == s.biome).unwrap(), 1)),
            }
        }
        if counts.len() >= want {
            return;
        }
        let common = counts.iter().max_by_key(|(_, n)| *n).unwrap().0;
        let missing = order
            .iter()
            .find(|b| !counts.iter().any(|(c, _)| c == *b))
            .unwrap();
        let last = sites.iter().rposition(|s| s.biome == common).unwrap();
        sites[last].biome = (*missing).into();
    }
}
/// Mountain ridge: a rotated line of cliff blocks with one seeded pass.
fn ridge(cx: f64, cy: f64, rot: f64, roll: u64) -> Vec<Piece> {
    let (sin, cos) = rot.sin_cos();
    let pass = (roll % 5) as i32 - 2;
    (-3..=3)
        .filter(|n| *n != pass)
        .map(|n| {
            let along = n as f64 * 270.;
            let size = 170. + ((roll >> (n + 8)) % 60) as f64;
            let (x, y) = (cx + cos * along, cy + sin * along);
            (
                (x - size / 2.).round(),
                (y - size * 0.4).round(),
                size.round(),
                (size * 0.8).round(),
                "cliff",
            )
        })
        .collect()
}
/// Lake: overlapping water rectangles that read as one rounded body.
fn lake(cx: f64, cy: f64, rng: &mut Rng) -> Vec<Piece> {
    let r = 170. + (rng.next_u64() % 130) as f64;
    let (a, b) = (
        0.55 + (rng.next_u64() % 30) as f64 / 100.,
        0.55 + (rng.next_u64() % 30) as f64 / 100.,
    );
    [
        (0., 0., 2. * r, 2. * r * a),
        (0., 0., 2. * r * b, 2. * r),
        (r * 0.7, r * 0.5, r * 1.1, r * 1.1),
    ]
    .iter()
    .map(|(dx, dy, w, h)| {
        (
            (cx + dx - w / 2.).round(),
            (cy + dy - h / 2.).round(),
            w.round(),
            h.round(),
            "water",
        )
    })
    .collect()
}
/// SplitMix64 finalizer: decorrelates nearby inputs.
fn mix(mut z: u64) -> u64 {
    z = z.wrapping_add(0x9E37_79B9_7F4A_7C15);
    z = (z ^ (z >> 30)).wrapping_mul(0xBF58_476D_1CE4_E5B9);
    z = (z ^ (z >> 27)).wrapping_mul(0x94D0_49BB_1331_11EB);
    z ^ (z >> 31)
}
type Piece = (f64, f64, f64, f64, &'static str);
/// Minimum gap between a new structure and earlier geometry. Wider than a
/// heavy chassis plus the 64-unit navigation step, so every gap is walkable.
const DISTRICT_CLEARANCE: f64 = 90.;
/// World-wide district piece budget, split evenly across sites.
const DISTRICT_PIECES: usize = 8000;
/// Themed structures fill each site's cell outside its core rings and its
/// street cross, so the world reads as places instead of empty ground between
/// anchors. Each structure is placed whole or not at all, clear of earlier
/// geometry and hazards. A per-site budget bounds total geometry because
/// obstacles travel in every snapshot. A separate RNG stream keeps the core
/// sites, scatter, and hazards identical to earlier layouts for a seed.
fn build_districts(world: &mut World, c: &Config, cols: usize, cover_per_site: usize) {
    let cw = c.width / cols as f64;
    let ch = c.height / cols as f64;
    let lot = (cw.min(ch) / 6.).clamp(240., 560.);
    // Structures keep at least 80% scale on small maps; clearance checks then
    // thin them out instead of shrinking walls and crates into slivers.
    let k = (lot / 560.).max(0.8);
    let core = 330. + lot * 0.35;
    let street = 110. + lot * 0.45;
    let budget = (DISTRICT_PIECES / world.sites.len()).clamp(24, 200) * cover_per_site / 8;
    let mut rng = Rng((c.seed ^ 0x9E37_79B9_7F4A_7C15) | 1);
    for i in 0..world.sites.len() {
        let (sx, sy) = (world.sites[i].x, world.sites[i].y);
        let biome = world.sites[i].biome.clone();
        let nx = (cw / lot).floor().max(1.) as usize;
        let ny = (ch / lot).floor().max(1.) as usize;
        let (x0, y0) = (
            sx - nx as f64 * lot / 2. + lot / 2.,
            sy - ny as f64 * lot / 2. + lot / 2.,
        );
        let mut lots: Vec<(f64, f64)> = (0..nx * ny)
            .map(|n| (x0 + (n % nx) as f64 * lot, y0 + (n / nx) as f64 * lot))
            .collect();
        // Seeded visiting order spreads the budget across the whole cell.
        for n in (1..lots.len()).rev() {
            let j = (rng.next_u64() % (n as u64 + 1)) as usize;
            lots.swap(n, j);
        }
        let mut used = 0;
        for (n, (lx, ly)) in lots.into_iter().enumerate() {
            let open_field = rng.next_u64() % 10 < 3;
            let jx = ((rng.next_u64() % 81) as f64 - 40.) * k;
            let jy = ((rng.next_u64() % 81) as f64 - 40.) * k;
            if open_field
                || distance(lx, ly, sx, sy) < core
                || (lx - sx).abs() < street
                || (ly - sy).abs() < street
            {
                continue;
            }
            let pieces = structure(&biome, lx + jx, ly + jy, k, &mut rng);
            if used + pieces.len() > budget {
                continue;
            }
            let pieces: Vec<Piece> = pieces
                .into_iter()
                .map(|(x, y, w, h, m)| (x.round(), y.round(), w.round(), h.round(), m))
                .filter(|p| p.2 >= 20. && p.3 >= 20.)
                .collect();
            if pieces.is_empty() {
                continue;
            }
            let (bx0, by0, bx1, by1) = pieces.iter().fold(
                (f64::MAX, f64::MAX, f64::MIN, f64::MIN),
                |(a, b, cc, d), p| (a.min(p.0), b.min(p.1), cc.max(p.0 + p.2), d.max(p.1 + p.3)),
            );
            if bx0 < 40. || by0 < 40. || bx1 > c.width - 40. || by1 > c.height - 40. {
                continue;
            }
            let g = DISTRICT_CLEARANCE;
            let blocked = world
                .grid
                .query(bx0 - g, by0 - g, bx1 - bx0 + 2. * g, by1 - by0 + 2. * g)
                .into_iter()
                .any(|id| overlaps(&world.obstacles[id], bx0 - g, by0 - g, bx1 + g, by1 + g))
                || world.hazards.iter().any(|h| {
                    h.x < bx1 + g
                        && h.x + h.width > bx0 - g
                        && h.y < by1 + g
                        && h.y + h.height > by0 - g
                });
            if blocked {
                continue;
            }
            for (j, (x, y, w, h, m)) in pieces.into_iter().enumerate() {
                let id = world.obstacles.len();
                world.obstacles.push(Obstacle {
                    id: format!("district-{i}-{n}-{j}"),
                    shape: "aabb".into(),
                    x,
                    y,
                    width: w,
                    height: h,
                    material: m.into(),
                });
                world.grid.insert(id, x, y, w, h);
                used += 1;
            }
        }
    }
}
fn overlaps(o: &Obstacle, x0: f64, y0: f64, x1: f64, y1: f64) -> bool {
    o.x < x1 && o.x + o.width > x0 && o.y < y1 && o.y + o.height > y0
}
/// Walled room centered on (cx, cy) with 120-unit doorways on the chosen
/// sides (top, right, bottom, left).
fn room(
    cx: f64,
    cy: f64,
    w: f64,
    h: f64,
    doors: [bool; 4],
    mat: &'static str,
    k: f64,
) -> Vec<Piece> {
    let t = 24. * k.max(0.6);
    let door = 120.;
    let (x0, y0) = (cx - w / 2., cy - h / 2.);
    let mut out = vec![];
    for (side, open) in doors.iter().enumerate() {
        let horizontal = side % 2 == 0;
        let (sx, sy, len) = match side {
            0 => (x0, y0, w),
            1 => (x0 + w - t, y0 + t, h - 2. * t),
            2 => (x0, y0 + h - t, w),
            _ => (x0, y0 + t, h - 2. * t),
        };
        let spans = if *open {
            let half = (len - door) / 2.;
            vec![(0., half), (half + door, half)]
        } else {
            vec![(0., len)]
        };
        for (offset, span) in spans {
            if span < t {
                continue;
            }
            out.push(if horizontal {
                (sx + offset, sy, span, t, mat)
            } else {
                (sx, sy + offset, t, span, mat)
            });
        }
    }
    out
}
fn structure(biome: &str, cx: f64, cy: f64, k: f64, rng: &mut Rng) -> Vec<Piece> {
    let roll = rng.next_u64() % 100;
    let r = |rng: &mut Rng, lo: f64, span: u64| (lo + (rng.next_u64() % span) as f64) * k;
    let flip = rng.next_u64().is_multiple_of(2);
    match biome {
        "urban" if roll < 45 => {
            let (w, h) = (r(rng, 300., 120), r(rng, 240., 100));
            let doors = if flip {
                [true, false, true, false]
            } else {
                [false, true, false, true]
            };
            room(cx, cy, w, h, doors, "brick", k)
        }
        "urban" if roll < 75 => {
            // Ruined corner: an L of walls with a loose crate beside it.
            let (a, b) = (r(rng, 240., 80), r(rng, 180., 60));
            let t = 24. * k.max(0.6);
            vec![
                (cx - a / 2., cy - b / 2., a, t, "brick"),
                (cx - a / 2., cy - b / 2. + t, t, b - t, "brick"),
                (
                    cx + a / 2. - 60. * k,
                    cy + b / 2. - 60. * k,
                    60. * k,
                    60. * k,
                    "crate",
                ),
            ]
        }
        "urban" => {
            let s = 64. * k;
            let step = 160. * k;
            (0..3)
                .map(|n| {
                    (
                        cx - step + n as f64 * step - s / 2.,
                        cy - s / 2. + if n == 1 { 60. * k } else { 0. },
                        s,
                        s,
                        "crate",
                    )
                })
                .collect()
        }
        "industrial" if roll < 50 => {
            let (len, t, gap) = (r(rng, 240., 60), 76. * k, 110. * k.max(0.85));
            (0..(2 + rng.next_u64() % 2))
                .map(|n| {
                    let off = n as f64 * (t + gap) - (t + gap);
                    if flip {
                        (cx - len / 2., cy + off - t / 2., len, t, "container")
                    } else {
                        (cx + off - t / 2., cy - len / 2., t, len, "container")
                    }
                })
                .collect()
        }
        "industrial" if roll < 80 => {
            let (w, h) = (r(rng, 360., 80), r(rng, 280., 60));
            room(cx, cy, w, h, [true, true, !flip, flip], "metal", k)
        }
        "industrial" => {
            let s = 44. * k.max(0.8);
            [(-1., -1.), (1., -1.), (0., 1.), (1.6, 0.8)]
                .iter()
                .take(3 + (rng.next_u64() % 2) as usize)
                .map(|(dx, dy)| {
                    (
                        cx + dx * 70. * k - s / 2.,
                        cy + dy * 70. * k - s / 2.,
                        s,
                        s,
                        "barrel",
                    )
                })
                .collect()
        }
        "forest" if roll < 75 => {
            // Grove on a 3x3 lattice; lattice spacing keeps 90+ unit lanes.
            let step = 190. * k.max(0.8);
            let mut out = vec![];
            for n in 0..9 {
                if rng.next_u64() % 9 < 4 {
                    continue;
                }
                let s = r(rng, 70., 30);
                let (gx, gy) = ((n % 3) as f64 - 1., (n / 3) as f64 - 1.);
                out.push((
                    cx + gx * step - s / 2.,
                    cy + gy * step - s / 2.,
                    s,
                    s,
                    "tree",
                ));
            }
            out
        }
        "forest" => {
            let len = r(rng, 220., 80);
            let t = 36. * k.max(0.7);
            vec![
                if flip {
                    (cx - len / 2., cy - t / 2., len, t, "hedge")
                } else {
                    (cx - t / 2., cy - len / 2., t, len, "hedge")
                },
                (
                    cx + if flip { 0. } else { 110. * k } - 40. * k,
                    cy + if flip { 110. * k } else { 0. } - 40. * k,
                    80. * k,
                    80. * k,
                    "tree",
                ),
            ]
        }
        "snow" if roll < 60 => {
            // Pine stand on a loose lattice, like a forest grove.
            let step = 180. * k.max(0.8);
            let mut out = vec![];
            for n in 0..9 {
                if rng.next_u64() % 9 < 3 {
                    continue;
                }
                let s = r(rng, 60., 26);
                let (gx, gy) = ((n % 3) as f64 - 1., (n / 3) as f64 - 1.);
                out.push((
                    cx + gx * step - s / 2.,
                    cy + gy * step - s / 2.,
                    s,
                    s,
                    "pine",
                ));
            }
            out
        }
        "snow" => {
            let mut out = vec![];
            for (dx, dy) in [(-0.8, -0.5), (0.9, 0.2), (-0.1, 1.)] {
                let s = r(rng, 70., 50);
                out.push((
                    cx + dx * 150. * k - s / 2.,
                    cy + dy * 150. * k - s / 2.,
                    s,
                    s * 0.75,
                    "ice",
                ));
            }
            out
        }
        "swamp" if roll < 40 => {
            // Pond: two crossed water slabs that read as one pool.
            let (a, b) = (r(rng, 200., 80), r(rng, 120., 40));
            vec![
                (cx - a / 2., cy - b / 2., a, b, "water"),
                (cx - b / 2., cy - a / 2., b, a, "water"),
            ]
        }
        "swamp" if roll < 75 => {
            let step = 200. * k.max(0.8);
            [(-1., -1.), (1., -0.6), (-0.4, 1.), (0.9, 0.9)]
                .iter()
                .take(3 + (rng.next_u64() % 2) as usize)
                .map(|(dx, dy)| {
                    let s = r(rng, 56., 24);
                    (
                        cx + dx * step / 2. - s / 2.,
                        cy + dy * step / 2. - s / 2.,
                        s,
                        s,
                        "deadtree",
                    )
                })
                .collect()
        }
        "swamp" => {
            let len = r(rng, 180., 80);
            let t = 34. * k.max(0.7);
            vec![
                (cx - len / 2., cy - 80. * k, len, t, "reeds"),
                (cx - len / 2. + 60. * k, cy + 60. * k, len * 0.8, t, "reeds"),
            ]
        }
        "desert" if roll < 50 => {
            let mut out = vec![];
            for (dx, dy) in [(-1., -0.4), (0.9, -0.8), (0.2, 1.)] {
                if out.len() == 2 && rng.next_u64().is_multiple_of(2) {
                    break;
                }
                let s = r(rng, 80., 50);
                out.push((
                    cx + dx * 150. * k - s / 2.,
                    cy + dy * 150. * k - s / 2.,
                    s,
                    s * 0.8,
                    "rock",
                ));
            }
            out
        }
        _ => {
            // Sandbag outpost: a U of bag lines with open corners.
            let (w, h, t) = (240. * k, 180. * k, 30. * k.max(0.7));
            let gap = 100.;
            let side = h - gap;
            let mut out = vec![(cx - w / 2. + gap / 2., cy - h / 2., w - gap, t, "sandbag")];
            if side > t {
                out.push((cx - w / 2., cy - h / 2. + gap / 2. + t, t, side, "sandbag"));
                out.push((
                    cx + w / 2. - t,
                    cy - h / 2. + gap / 2. + t,
                    t,
                    side,
                    "sandbag",
                ));
            }
            out
        }
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
