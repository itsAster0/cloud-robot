use arena_engine::{
    catalog::{self, Loadout},
    edit::Edit,
    model::*,
    simulation::{
        Arena, BOT_WAVE_TICKS, DRIFT_LEG_TICKS, KILL_POINTS, RENEW_LEGS, RENEW_MARGIN,
        RESTOCK_TICKS, SAFE_SPAWN_DISTANCE, SPAWN_PROTECT_TICKS,
    },
    world::*,
};
use serde_json::json;
use std::collections::BTreeMap;
/// Site rings and wild scatter; district fill is covered by its own test.
/// Site cover plus five pieces per gap between sites; a gap that became a
/// landform (ridge or lake) counts as its five scatter slots.
fn core_obstacles(w: &World) -> usize {
    let landform_gaps: std::collections::BTreeSet<&str> = w
        .obstacles
        .iter()
        .filter(|o| o.id.starts_with("land-"))
        .map(|o| &o.id[..o.id.rfind('-').unwrap()])
        .collect();
    w.obstacles
        .iter()
        .filter(|o| o.id.starts_with("cover-") || o.id.starts_with("wild-"))
        .count()
        + 5 * landform_gaps.len()
}
fn config(n: usize) -> Config {
    serde_json::from_value(json!({"matchId":"test","mode":"sandbox","capacity":n,"width":42000,"height":26250,"durationSeconds":1080,"seed":42,"robots":(0..n).map(|i|json!({"robotId":format!("human-{i:03}"),"name":format!("Robot {i}"),"bot":false})).collect::<Vec<_>>()})).unwrap()
}

#[test]
fn rejected_consumable_drop_preserves_inventory_at_chunk_limit() {
    let mut a = Arena::new(config(1)).unwrap();
    clear(&mut a);
    a.robots[0].inventory.push(Stack {
        kind: "medkit".into(),
        count: 2,
        ..Default::default()
    });
    for i in 0..64 {
        a.world.containers.push(Container {
            item_id: format!("full-{i}"),
            x: 100.,
            y: 100.,
            contents: vec![],
            expires_at: None,
        });
    }
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                drop_slot: Some(0),
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    assert_eq!(a.world.containers.len(), 64);
    assert_eq!(a.robots[0].inventory[0].count, 2);
    assert!(
        a.robots[0]
            .action_results
            .iter()
            .any(|r| r == "CONTAINER_BUDGET_EXCEEDED")
    );
}
fn clear(a: &mut Arena) {
    a.world.obstacles.clear();
    a.world.containers.clear();
    for (i, r) in a.robots.iter_mut().enumerate() {
        r.x = 500. + i as f64 * 200.;
        r.y = 500.;
        r.heading = 0.;
        r.turret_heading = 0.;
    }
    a.reindex();
}
#[test]
fn catalogue_budget_and_slots() {
    let mut l = Loadout::default();
    assert_eq!(catalog::validate(&l).unwrap(), 25);
    l.chassis = "heavy".into();
    l.weapon = "railgun".into();
    l.modules = vec!["optics".into(), "capacitor".into()];
    assert!(catalog::validate(&l).is_err());
    l.modules = vec!["optics".into(), "optics".into()];
    assert!(catalog::validate(&l).is_err());
    l.weapon = "unknown".into();
    assert!(catalog::validate(&l).is_err());
}
#[test]
fn capacity_is_256_not_257() {
    assert!(Arena::new(config(256)).is_ok());
    assert!(Arena::new(config(257)).is_err());
}
#[test]
fn generation_and_inputs_replay_exactly() {
    let mut a = Arena::new(config(16)).unwrap();
    let mut b = Arena::new(config(16)).unwrap();
    assert_eq!(a.state_hash(), b.state_hash());
    for tick in 0..100 {
        let mut inputs = BTreeMap::new();
        inputs.insert(
            "human-000".into(),
            Action {
                throttle: 1.,
                turn: if tick < 20 { 0.5 } else { 0. },
                fire: true,
                ..Default::default()
            },
        );
        a.step(inputs.clone(), &[]).unwrap();
        b.step(inputs, &[]).unwrap();
        assert_eq!(a.state_hash(), b.state_hash());
    }
}
#[test]
fn checkpoint_restores_simulation() {
    let mut a = Arena::new(config(2)).unwrap();
    for _ in 0..20 {
        a.step(BTreeMap::new(), &[]).unwrap();
    }
    let mut b: Arena = serde_json::from_slice(&serde_json::to_vec(&a).unwrap()).unwrap();
    b.reindex();
    a.step(BTreeMap::new(), &[]).unwrap();
    b.step(BTreeMap::new(), &[]).unwrap();
    assert_eq!(a.state_hash(), b.state_hash());
}
#[test]
fn acceleration_and_independent_turret() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    let mut actions = BTreeMap::new();
    actions.insert(
        "human-000".into(),
        Action {
            throttle: 1.,
            aim: Some(90.),
            ..Default::default()
        },
    );
    a.step(actions, &[]).unwrap();
    let r = &a.robots[0];
    assert!((r.x - 500.6).abs() < 0.001);
    assert_eq!(r.heading, 0.);
    assert_eq!(r.turret_heading, 13.5);
}
#[test]
fn dash_cannot_cross_wall() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.world.obstacles.push(Obstacle {
        id: "wall".into(),
        shape: "aabb".into(),
        x: 540.,
        y: 400.,
        width: 10.,
        height: 200.,
        material: "wall".into(),
    });
    a.reindex();
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                dash: true,
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    assert!(a.robots[0].x < 526.1);
    assert_eq!(a.robots[0].energy, 70.);
}
#[test]
fn shield_and_projectile_damage() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[1].shield = 10.;
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                fire: true,
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    for _ in 0..20 {
        a.step(BTreeMap::new(), &[]).unwrap();
    }
    assert_eq!(a.robots[1].shield, 0.);
    assert_eq!(a.robots[1].hp, 90.);
    assert_eq!(a.robots[0].damage_dealt, 20.);
}
#[test]
fn holstering_preserves_heat_and_cooldown() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[0].weapons.push(WeaponSlot {
        kind: "cannon".into(),
        ..Default::default()
    });
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                fire: true,
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                weapon: Some(1),
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    assert!(a.robots[0].weapons[0].heat > 0.);
    assert!(a.robots[0].weapons[0].ready_at > a.tick);
}
#[test]
fn observation_does_not_leak_hidden_or_private_opponent_state() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    let obs = a.observation(0);
    assert_eq!(obs["robots"].as_array().unwrap().len(), 1);
    assert!(obs["robots"][0].get("inventory").is_none());
    assert!(obs["robots"][0].get("loadout").is_none());
    a.robots[1].x = 4000.;
    assert!(a.observation(0)["robots"].as_array().unwrap().is_empty());
    a.robots[1].alive = false;
    assert!(a.observation(0)["robots"].as_array().unwrap().is_empty());
}
#[test]
fn cover_and_cloak_hide_enemies() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[1].cloak_until = 100;
    assert!(!a.visible(0, 1));
    a.robots[1].cloak_until = 0;
    a.world.obstacles.push(Obstacle {
        id: "wall".into(),
        shape: "aabb".into(),
        x: 600.,
        y: 450.,
        width: 20.,
        height: 100.,
        material: "wall".into(),
    });
    a.reindex();
    assert!(!a.visible(0, 1));
}
#[test]
fn pickup_has_no_silent_weapon_replacement() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[0].weapons.push(WeaponSlot {
        kind: "cannon".into(),
        ..Default::default()
    });
    a.world.containers.push(Container {
        item_id: "box".into(),
        x: 500.,
        y: 500.,
        contents: vec![Stack {
            kind: "weapon:railgun".into(),
            count: 1,
            ..Default::default()
        }],
        expires_at: None,
    });
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                pickup: Some("box".into()),
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    assert_eq!(a.robots[0].weapons.len(), 2);
    assert_eq!(a.world.containers[0].contents.len(), 1);
}
#[test]
fn simultaneous_elimination_draws() {
    let mut c = config(2);
    c.mode = "br-solo".into();
    let mut a = Arena::new(c).unwrap();
    clear(&mut a);
    a.step(BTreeMap::new(), &["human-000".into(), "human-001".into()])
        .unwrap();
    assert_eq!(a.winner_team, "draw");
    assert_eq!(a.robots[0].placement, a.robots[1].placement);
}
#[test]
fn squads_never_exceed_four() {
    let mut c = config(8);
    c.mode = "br-squad".into();
    for r in &mut c.robots {
        r.team = "same".into();
    }
    assert!(Arena::new(c).is_err());
}
#[test]
fn edits_validate_revision_and_robot_clearance() {
    let mut c = config(2);
    c.live_edit = true;
    let mut a = Arena::new(c).unwrap();
    clear(&mut a);
    let mut edit = Edit {
        hazards: vec![],
        remove_hazards: vec![],
        future_zones: None,
        expected_revision: 1,
        effective_tick: 10,
        obstacles: vec![Obstacle {
            id: "bad".into(),
            shape: "aabb".into(),
            x: 495.,
            y: 495.,
            width: 30.,
            height: 30.,
            material: "wall".into(),
        }],
        remove_obstacles: vec![],
        containers: vec![],
        transit: vec![],
    };
    assert!(a.preview_edit(&edit).is_err());
    assert_eq!(a.world.revision, 1);
    edit.obstacles[0].x = 1200.;
    assert!(a.preview_edit(&edit).is_ok());
    a.apply_edit(&edit).unwrap();
    assert_eq!(a.world.revision, 2);
    assert!(a.apply_edit(&edit).is_err());
}
#[test]
fn swept_geometry_and_short_rotation() {
    let o = Obstacle {
        id: "w".into(),
        shape: "aabb".into(),
        x: 50.,
        y: 0.,
        width: 2.,
        height: 100.,
        material: "wall".into(),
    };
    assert_eq!(segment_box(0., 50., 100., 50., &o, 0.), Some(0.5));
    assert!((rotate(359., 1., 10.) - 1.).abs() < 0.001);
    assert!(segment_circle(0., 0., 100., 0., 50., 0., 2.).is_some());
}

#[test]
fn damage_on_completion_tick_cancels_healing() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[1].hp = 50.;
    a.robots[1].inventory.push(Stack {
        kind: "medkit".into(),
        count: 1,
        ..Default::default()
    });
    a.robots[1].channel = Some(Channel {
        kind: "medkit".into(),
        until: 0,
        slot: 0,
    });
    a.projectiles.push(Projectile {
        projectile_id: "test".into(),
        owner_id: "human-000".into(),
        team: "human-000".into(),
        kind: "plasma".into(),
        x: 680.,
        y: 500.,
        vx: 640.,
        vy: 0.,
        damage: 20.,
        remaining: 100.,
    });
    a.step(BTreeMap::new(), &[]).unwrap();
    assert_eq!(a.robots[1].hp, 30.);
    assert!(a.robots[1].channel.is_none());
    assert_eq!(a.robots[1].inventory[0].count, 1);
}
#[test]
fn human_squad_is_filled_before_new_squads() {
    let mut c = config(1);
    c.capacity = 8;
    c.mode = "br-squad".into();
    c.robots[0].team = "friends".into();
    let a = Arena::new(c).unwrap();
    assert_eq!(a.robots.iter().filter(|r| r.team == "friends").count(), 4);
    let human = a.robots.iter().find(|r| !r.bot).unwrap();
    for r in a.robots.iter().filter(|r| r.team == "friends") {
        assert!(distance(human.x, human.y, r.x, r.y) < 400.);
    }
}
#[test]
fn every_generated_spawn_is_separated() {
    let mut c = config(256);
    c.width = 2400.;
    c.height = 1500.;
    let a = Arena::new(c).unwrap();
    for (i, r) in a.robots.iter().enumerate() {
        for other in a.robots.iter().skip(i + 1) {
            assert!(distance(r.x, r.y, other.x, other.y) >= RADIUS * 2.);
        }
    }
}
#[test]
fn scan_is_local_and_reports_coarse_contacts() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[1].cloak_until = 100;
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                scan: true,
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    let obs = a.observation(0);
    let event = obs["events"]
        .as_array()
        .unwrap()
        .iter()
        .find(|e| e["type"] == "scan_result")
        .unwrap();
    let contacts: serde_json::Value =
        serde_json::from_str(event["message"].as_str().unwrap()).unwrap();
    assert_eq!(contacts.as_array().unwrap().len(), 1);
    assert!(contacts[0].get("hp").is_none());
    assert_eq!(a.robots[0].energy, 75.);
}
#[test]
fn shield_and_consumable_caps() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[0].shield = 45.;
    a.robots[0].inventory.push(Stack {
        kind: "shield_cell".into(),
        count: 1,
        ..Default::default()
    });
    a.robots[0].channel = Some(Channel {
        kind: "shield_cell".into(),
        until: 0,
        slot: 0,
    });
    a.step(BTreeMap::new(), &[]).unwrap();
    assert_eq!(a.robots[0].shield, 50.);
    assert!(a.robots[0].inventory.is_empty());
}

#[test]
fn every_weapon_can_damage_an_exposed_target() {
    for w in catalog::WEAPONS {
        let mut a = Arena::new(config(2)).unwrap();
        clear(&mut a);
        a.robots[0].weapons[0].kind = w.id.into();
        for _ in 0..70 {
            a.step(
                BTreeMap::from([(
                    "human-000".into(),
                    Action {
                        fire: true,
                        aim: Some(0.),
                        ..Default::default()
                    },
                )]),
                &[],
            )
            .unwrap();
        }
        assert!(a.robots[1].hp < 100., "{} did no damage", w.id);
    }
}
#[test]
fn corpse_and_smoke_do_not_reveal_contacts() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[0].alive = false;
    assert_eq!(a.observation(0)["robots"].as_array().unwrap().len(), 0);
    a.robots[0].alive = true;
    a.fields.push(Field {
        id: "smoke".into(),
        kind: "smoke".into(),
        owner_id: "human-000".into(),
        team: "blue".into(),
        x: 600.,
        y: 500.,
        end_tick: 160,
    });
    a.world.containers.push(Container {
        item_id: "loot".into(),
        x: 700.,
        y: 500.,
        contents: vec![Stack {
            kind: "medkit".into(),
            count: 1,
            ..Default::default()
        }],
        expires_at: None,
    });
    assert_eq!(a.observation(0)["robots"].as_array().unwrap().len(), 0);
    assert_eq!(a.observation(0)["items"].as_array().unwrap().len(), 0);
}
#[test]
fn scans_emit_only_coarse_local_event() {
    let mut a = Arena::new(config(3)).unwrap();
    clear(&mut a);
    a.robots[2].x = 3000.;
    a.reindex();
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                scan: true,
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    let near = a.observation(1);
    let far = a.observation(2);
    assert!(
        near["events"]
            .as_array()
            .unwrap()
            .iter()
            .any(|e| e["type"] == "scan_emitted")
    );
    assert!(
        !near["events"]
            .as_array()
            .unwrap()
            .iter()
            .any(|e| e["type"] == "scan_result")
    );
    assert!(
        !far["events"]
            .as_array()
            .unwrap()
            .iter()
            .any(|e| e["type"] == "scan_emitted")
    );
}
#[test]
fn bot_discrete_commands_are_not_repeated_on_intermediate_ticks() {
    let action = Action {
        throttle: 1.,
        fire: true,
        dash: true,
        scan: true,
        message: Some("report".into()),
        ..Default::default()
    };
    let held = action.continuous();
    assert!(held.fire);
    assert_eq!(held.throttle, 1.);
    assert!(!held.dash);
    assert!(!held.scan);
    assert!(held.message.is_none());
}

#[test]
fn explicit_swap_preserves_weapon_state_and_last_weapon() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[0].weapons[0].heat = 80.;
    a.robots[0].weapons[0].ready_at = 100;
    a.world.containers.push(Container {
        item_id: "gear".into(),
        x: 500.,
        y: 500.,
        contents: vec![Stack {
            kind: "weapon:railgun".into(),
            count: 1,
            ..Default::default()
        }],
        expires_at: None,
    });
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                equip: Some(Equip {
                    container: "gear".into(),
                    kind: "weapon:railgun".into(),
                    slot: 0,
                }),
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    assert_eq!(a.robots[0].weapon(), "railgun");
    let dropped = &a.world.containers[0].contents[0];
    assert_eq!(dropped.kind, "weapon:plasma");
    assert_eq!(dropped.weapon_state.as_ref().unwrap().ready_at, 100);
    assert!(dropped.weapon_state.as_ref().unwrap().heat > 75.);
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                drop_equipment: Some(EquipmentSlot {
                    group: "weapon".into(),
                    slot: 0,
                }),
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    assert_eq!(a.robots[0].weapons.len(), 1);
    assert!(
        a.robots[0]
            .action_results
            .iter()
            .any(|s| s == "LAST_WEAPON_REQUIRED")
    );
}
#[test]
fn automatic_pickup_obeys_preferences_without_swapping() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.world.containers.push(Container {
        item_id: "gear".into(),
        x: 500.,
        y: 500.,
        contents: vec![
            Stack {
                kind: "medkit".into(),
                count: 1,
                ..Default::default()
            },
            Stack {
                kind: "weapon:railgun".into(),
                count: 1,
                ..Default::default()
            },
        ],
        expires_at: None,
    });
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                pickup_priorities: Some(vec!["medkit".into()]),
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    assert_eq!(a.robots[0].inventory[0].kind, "medkit");
    assert_eq!(a.robots[0].weapons.len(), 1);
    assert_eq!(a.world.containers[0].contents[0].kind, "weapon:railgun");
}
#[test]
fn utility_swaps_keep_spent_charges_and_cooldowns() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[0].loadout.utilities.push("smoke_projector".into());
    a.robots[0].charges.insert("smoke_projector".into(), 0);
    a.robots[0].cooldowns.insert("smoke_projector".into(), 100);
    a.world.containers.push(Container {
        item_id: "gear".into(),
        x: 500.,
        y: 500.,
        contents: vec![Stack {
            kind: "utility:mine_dispenser".into(),
            count: 1,
            ..Default::default()
        }],
        expires_at: None,
    });
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                equip: Some(Equip {
                    container: "gear".into(),
                    kind: "utility:mine_dispenser".into(),
                    slot: 0,
                }),
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    let old = &a.world.containers[0].contents[0];
    assert_eq!(old.charges, Some(0));
    assert_eq!(old.ready_at, Some(100));
    assert_eq!(a.robots[0].charges["mine_dispenser"], 3);
}

#[test]
fn cannon_knockback_stops_at_cover() {
    let mut a = Arena::new(config(2)).unwrap();
    clear(&mut a);
    a.robots[0].weapons[0].kind = "cannon".into();
    a.world.obstacles.push(Obstacle {
        id: "behind-target".into(),
        shape: "aabb".into(),
        x: 730.,
        y: 450.,
        width: 40.,
        height: 100.,
        material: "wall".into(),
    });
    a.reindex();
    a.step(
        BTreeMap::from([(
            "human-000".into(),
            Action {
                fire: true,
                ..Default::default()
            },
        )]),
        &[],
    )
    .unwrap();
    for _ in 0..40 {
        a.step(BTreeMap::new(), &[]).unwrap();
    }
    assert!(a.robots[1].hp < 100.);
    assert!(a.robots[1].x > 700.);
    assert!(a.robots[1].x <= 730. - RADIUS);
}

#[test]
fn bounded_navigation_detours_and_rejects_sealed_routes() {
    let mut a = Arena::new(config(1)).unwrap();
    clear(&mut a);
    a.world.obstacles.push(Obstacle {
        id: "wall".into(),
        shape: "aabb".into(),
        x: 600.,
        y: 350.,
        width: 30.,
        height: 300.,
        material: "wall".into(),
    });
    a.reindex();
    let p = local_waypoint(&a.world, (500., 500.), (800., 500.), (1200., 1000.), 18.).unwrap();
    assert!(a.world.wall_hit(500., 500., p.0, p.1, 18.).is_none());
    assert_ne!(p, (800., 500.));
    assert_eq!(
        Some(p),
        local_waypoint(&a.world, (500., 500.), (800., 500.), (1200., 1000.), 18.)
    );
    a.world.obstacles[0].y = 0.;
    a.world.obstacles[0].height = 1000.;
    a.reindex();
    assert!(local_waypoint(&a.world, (500., 500.), (800., 500.), (1200., 1000.), 18.).is_none());
}

#[test]
fn bots_keep_moving_on_dense_maps() {
    let mk = || {
        serde_json::from_value::<Config>(json!({"matchId":"dense","mode":"sandbox","capacity":1,"width":24000,"height":15000,"durationSeconds":1080,"seed":99,"siteCount":24,"coverPerSite":8,"lootPerSite":24,"robots":[{"robotId":"bot-000","name":"Bot 0","bot":true}]})).unwrap()
    };
    let mut a = Arena::new(mk()).unwrap();
    let start = (a.robots[0].x, a.robots[0].y);
    for _ in 0..600 {
        a.step(BTreeMap::new(), &[]).unwrap();
    }
    assert!(
        distance(a.robots[0].x, a.robots[0].y, start.0, start.1) > 100.,
        "bot froze at {:?}",
        (a.robots[0].x, a.robots[0].y)
    );
    // Same seed replays identical tracks: stuck recovery stays deterministic.
    let mut b = Arena::new(mk()).unwrap();
    for _ in 0..600 {
        b.step(BTreeMap::new(), &[]).unwrap();
    }
    assert!(
        (a.robots[0].x - b.robots[0].x).abs() < 1e-9
            && (a.robots[0].y - b.robots[0].y).abs() < 1e-9,
        "bot tracks diverged"
    );
}

#[test]
fn map_structures_vary_with_hazards_and_clear_spawns() {
    let c: Config = serde_json::from_value(json!({"matchId":"structs","mode":"br-solo","capacity":64,"width":42000,"height":26250,"durationSeconds":1080,"seed":7,"siteCount":16,"coverPerSite":8,"lootPerSite":16,"robots":[]})).unwrap();
    let w = World::generate(&c);
    assert_eq!(core_obstacles(&w), 16 * 8 + 16 * 5);
    // Structures read as architecture: varied segment sizes, not one stamp.
    let sizes: std::collections::BTreeSet<(u64, u64)> = w
        .obstacles
        .iter()
        .map(|o| (o.width as u64, o.height as u64))
        .collect();
    assert!(sizes.len() >= 3, "cover segments are uniform: {sizes:?}");
    // Material themes vary; collision stays uniform.
    let materials: std::collections::BTreeSet<&str> =
        w.obstacles.iter().map(|o| o.material.as_str()).collect();
    assert!(
        materials.len() >= 3,
        "cover materials are uniform: {materials:?}"
    );
    assert!(materials.contains("wall"));
    // No enclosures: bots path out of every site in all compass directions
    // through gaps (direct or detoured), so structures never seal anyone in.
    // Goals sit on clear ground along each ray; fully blocked rays cannot
    // occur by construction and do not count against the site.
    for s in &w.sites {
        let mut open = 0;
        for k in 0..8 {
            let a = k as f64 * std::f64::consts::TAU / 8.;
            let mut goal = None;
            let mut d = 250.;
            while d <= 600. {
                let (px, py) = (s.x + a.cos() * d, s.y + a.sin() * d);
                if px > 17.
                    && py > 17.
                    && px < 42000. - 17.
                    && py < 26250. - 17.
                    && w.clear(px, py, 17.)
                {
                    goal = Some((px, py));
                }
                d += 50.;
            }
            match goal {
                None => open += 1,
                Some(g) => {
                    if local_waypoint(&w, (s.x, s.y), g, (42000., 26250.), 17.).is_some() {
                        open += 1;
                    }
                }
            }
        }
        assert!(open >= 6, "site {} is boxed in", s.id);
    }
    assert!(w.hazards.len() >= 2, "large maps need hazard fields");
    for h in &w.hazards {
        assert!(
            h.kind == "slow" || h.kind == "slag",
            "unknown hazard {}",
            h.kind
        );
        if h.kind == "slow" {
            assert_eq!(h.damage_per_second, 0.);
        }
    }
    for s in &w.sites {
        // Spawn ground: the site center or a nearby ring point must be clear.
        let mut open = w.clear(s.x, s.y, 14.);
        for ring in [150., 350.] {
            if open {
                break;
            }
            for k in 0..12 {
                let a = k as f64 * std::f64::consts::TAU / 12.;
                if w.clear(s.x + a.cos() * ring, s.y + a.sin() * ring, 14.) {
                    open = true;
                    break;
                }
            }
        }
        assert!(open, "site {} has no clear spawn ground", s.id);
        for h in &w.hazards {
            assert!(
                distance(s.x, s.y, h.x + h.width / 2., h.y + h.height / 2.) > 200.,
                "hazard {} sits on site {}",
                h.id,
                s.id
            );
        }
    }
}
#[test]
fn dense_map_controls_scale_sites_cover_loot_deterministically() {
    let base = serde_json::json!({"matchId":"dense","mode":"sandbox","capacity":1,"width":42000,"height":26250,"durationSeconds":1080,"seed":7,"siteCount":16,"coverPerSite":8,"lootPerSite":24,"robots":[]});
    let c1: Config = serde_json::from_value(base.clone()).unwrap();
    let c2: Config = serde_json::from_value(base).unwrap();
    c1.validate().unwrap();
    let w1 = World::generate(&c1);
    let w2 = World::generate(&c2);
    assert_eq!(w1.sites.len(), 16);
    assert_eq!(core_obstacles(&w1), 16 * 8 + 16 * 5);
    assert_eq!(w1.containers.len(), 16 * 24);
    assert_eq!(w1.sites.len(), w2.sites.len());
    assert_eq!(w1.obstacles.len(), w2.obstacles.len());
    assert!(
        w1.sites
            .iter()
            .zip(w2.sites.iter())
            .all(|(a, b)| a.x == b.x && a.y == b.y)
    );
    let def: Config = serde_json::from_value(json!({"matchId":"def","mode":"sandbox","capacity":1,"width":42000,"height":26250,"durationSeconds":1080,"seed":7,"robots":[]})).unwrap();
    let wdef = World::generate(&def);
    assert_eq!(wdef.sites.len(), 64);
    assert_eq!(core_obstacles(&wdef), 64 * 8 + 64 * 5);
    assert_eq!(wdef.containers.len(), 64 * 16);
}

fn world_config(extra: serde_json::Value) -> Config {
    let mut v = json!({"matchId":"districts","mode":"br-solo","capacity":64,"width":42000,"height":26250,"durationSeconds":1080,"seed":7,"robots":[]});
    v.as_object_mut()
        .unwrap()
        .extend(extra.as_object().unwrap().clone());
    serde_json::from_value(v).unwrap()
}
fn structure_key(id: &str) -> &str {
    &id[..id.rfind('-').unwrap()]
}
#[test]
fn districts_fill_cells_with_themed_walkable_structures() {
    let w = World::generate(&world_config(json!({})));
    let biomes: std::collections::BTreeSet<&str> =
        w.sites.iter().map(|s| s.biome.as_str()).collect();
    assert!(
        biomes.len() >= 5,
        "climate yields varied biomes: {biomes:?}"
    );
    let district: Vec<&Obstacle> = w
        .obstacles
        .iter()
        .filter(|o| o.id.starts_with("district-"))
        .collect();
    assert!(
        district.len() > 3000,
        "sparse districts: {}",
        district.len()
    );
    assert!(w.obstacles.len() <= 16384, "exceeds the edit budget");
    let materials: std::collections::BTreeSet<&str> =
        district.iter().map(|o| o.material.as_str()).collect();
    for m in ["brick", "metal", "container", "tree", "rock", "sandbag"] {
        assert!(materials.contains(m), "missing {m}: {materials:?}");
    }
    let index: std::collections::HashMap<&str, usize> = w
        .obstacles
        .iter()
        .enumerate()
        .map(|(i, o)| (o.id.as_str(), i))
        .collect();
    for o in &district {
        assert!(o.x >= 40. && o.y >= 40. && o.x + o.width <= 41960. && o.y + o.height <= 26210.);
        for h in &w.hazards {
            assert!(
                !(o.x < h.x + h.width
                    && o.x + o.width > h.x
                    && o.y < h.y + h.height
                    && o.y + o.height > h.y),
                "{} sits in hazard {}",
                o.id,
                h.id
            );
        }
        // Distinct structures never touch, so gaps between them stay walkable.
        for j in w
            .grid
            .query(o.x - 80., o.y - 80., o.width + 160., o.height + 160.)
        {
            let other = &w.obstacles[j];
            if index[o.id.as_str()] == j || structure_key(&other.id) == structure_key(&o.id) {
                continue;
            }
            assert!(
                !(other.x < o.x + o.width + 80.
                    && other.x + other.width > o.x - 80.
                    && other.y < o.y + o.height + 80.
                    && other.y + other.height > o.y - 80.),
                "{} crowds {}",
                o.id,
                other.id
            );
        }
    }
    // Rooms keep doorways: a robot at each room center finds a way out.
    let mut rooms: BTreeMap<&str, (f64, f64, f64, f64)> = BTreeMap::new();
    for o in district
        .iter()
        .filter(|o| o.material == "brick" || o.material == "metal")
    {
        let b =
            rooms
                .entry(structure_key(&o.id))
                .or_insert((f64::MAX, f64::MAX, f64::MIN, f64::MIN));
        *b = (
            b.0.min(o.x),
            b.1.min(o.y),
            b.2.max(o.x + o.width),
            b.3.max(o.y + o.height),
        );
    }
    let mut checked = 0;
    for (id, (x0, y0, x1, y1)) in rooms.iter().step_by(7) {
        let (cx, cy) = ((x0 + x1) / 2., (y0 + y1) / 2.);
        if !w.clear(cx, cy, 24.) {
            continue;
        }
        let escaped = [(0., -1.), (1., 0.), (0., 1.), (-1., 0.)]
            .iter()
            .any(|(dx, dy)| {
                let goal = (
                    cx + dx * ((x1 - x0) / 2. + 60.),
                    cy + dy * ((y1 - y0) / 2. + 60.),
                );
                w.clear(goal.0, goal.1, 24.)
                    && local_waypoint(&w, (cx, cy), goal, (42000., 26250.), 24.).is_some()
            });
        assert!(escaped, "room {id} seals its interior");
        checked += 1;
    }
    assert!(checked > 20, "too few rooms checked: {checked}");
    let open = World::generate(&world_config(json!({"siteCount":16,"coverPerSite":0})));
    assert!(
        open.obstacles.is_empty(),
        "zero cover must stay open ground"
    );
}
#[test]
fn district_layout_is_byte_stable() {
    let w = World::generate(&world_config(json!({})));
    // Sites and districts use arithmetic only; the trig-placed core rings are
    // left out so the pin holds across platforms' libm.
    let district: Vec<&Obstacle> = w
        .obstacles
        .iter()
        .filter(|o| o.id.starts_with("district-"))
        .collect();
    let bytes = serde_json::to_vec(&(&w.sites, district)).unwrap();
    let hash = bytes.iter().fold(0xcbf29ce484222325u64, |h, b| {
        (h ^ u64::from(*b)).wrapping_mul(0x100000001b3)
    });
    assert_eq!(format!("{hash:016x}"), "7ac4a5ee7e2f0b9a");
}

#[test]
fn team_size_and_bot_cap_shape_the_roster() {
    let duo: Config = serde_json::from_value(json!({"matchId":"duo","mode":"br-squad","capacity":8,"teamSize":2,"width":2400,"height":1500,"durationSeconds":60,"seed":3,"robots":[]})).unwrap();
    let arena = Arena::new(duo).unwrap();
    let mut teams: BTreeMap<String, usize> = BTreeMap::new();
    for r in &arena.robots {
        *teams.entry(r.team.clone()).or_default() += 1;
    }
    assert_eq!(teams.len(), 4, "8 robots in duos make 4 teams: {teams:?}");
    assert!(teams.values().all(|n| *n == 2));

    let bad: Config = serde_json::from_value(json!({"matchId":"bad","mode":"br-squad","capacity":7,"teamSize":2,"width":2400,"height":1500,"durationSeconds":60,"robots":[]})).unwrap();
    assert!(bad.validate().is_err(), "capacity must divide into teams");

    let capped: Config = serde_json::from_value(json!({"matchId":"cap","mode":"br-solo","capacity":16,"bots":4,"width":2400,"height":1500,"durationSeconds":60,"seed":3,"robots":(0..2).map(|i|json!({"robotId":format!("human-{i}"),"name":"h","team":format!("t{i}"),"bot":false})).collect::<Vec<_>>()})).unwrap();
    let arena = Arena::new(capped).unwrap();
    assert_eq!(arena.robots.len(), 6, "2 humans plus a 4-bot cap");
    assert_eq!(arena.robots.iter().filter(|r| r.bot).count(), 4);
}

fn arena_config(capacity: usize, seconds: u32) -> Config {
    serde_json::from_value(json!({"matchId":"arena","mode":"arena","capacity":capacity,"width":2400,"height":1500,"durationSeconds":seconds,"seed":11,"robots":[]})).unwrap()
}

#[test]
fn arena_mode_respawns_and_never_closes() {
    let mut a = Arena::new(arena_config(6, 600)).unwrap();
    assert_eq!(a.robots.len(), 6);
    let mut respawned = false;
    for _ in 0..3000 {
        a.step(BTreeMap::new(), &[]).unwrap();
        assert!(
            a.zone().next.is_some(),
            "arena zone drifts instead of closing"
        );
        assert!(!a.finished, "arena never ends");
        respawned |= a.events.iter().any(|e| e.r#type == "robot_respawned");
    }
    assert!(respawned, "destroyed robots come back");
    assert!(a.robots.iter().any(|r| r.deaths > 0));
    assert!(
        a.robots.iter().all(|r| r.placement.is_none()),
        "no placements in the arena"
    );
}

#[test]
fn arena_join_displaces_a_bot_and_leave_refills() {
    let mut a = Arena::new(arena_config(4, 600)).unwrap();
    let human = serde_json::from_value(json!({"robotId":"human-1","name":"Ada","team":"","bot":false,"loadout":{"chassis":"generalist","weapon":"plasma"}})).unwrap();
    a.join(human).unwrap();
    assert_eq!(a.robots.len(), 4, "a bot made room");
    assert!(a.robots.iter().any(|r| r.robot_id == "human-1" && r.alive));
    a.step(BTreeMap::new(), &[]).unwrap();
    // Leave: the robot is destroyed, then removed, and a bot takes the slot.
    a.step(BTreeMap::new(), &["human-1".to_string()]).unwrap();
    a.step(BTreeMap::new(), &[]).unwrap();
    assert!(
        !a.robots.iter().any(|r| r.robot_id == "human-1"),
        "left robots are removed"
    );
    assert_eq!(
        a.robots.iter().filter(|r| !r.left).count(),
        3,
        "bots do not refill instantly"
    );
    while a.tick < BOT_WAVE_TICKS + 1 {
        a.step(BTreeMap::new(), &[]).unwrap();
    }
    assert_eq!(
        a.robots.iter().filter(|r| !r.left).count(),
        4,
        "a reinforcement wave refills the arena"
    );
    let solo = Arena::new(serde_json::from_value(json!({"matchId":"s","mode":"br-solo","capacity":2,"width":2400,"height":1500,"durationSeconds":60,"robots":[]})).unwrap());
    let reg = serde_json::from_value(
        json!({"robotId":"late","name":"x","loadout":{"chassis":"generalist","weapon":"plasma"}}),
    )
    .unwrap();
    assert!(
        solo.unwrap().join(reg).is_err(),
        "only arenas accept late joins"
    );
}

#[test]
fn arena_zone_drifts_with_overlap_and_never_ends() {
    let c: Config = serde_json::from_value(json!({"matchId":"drift","mode":"arena","capacity":12,"width":24000,"height":15000,"durationSeconds":10,"seed":5,"siteCount":16,"robots":[]})).unwrap();
    let mut a = Arena::new(c).unwrap();
    let start = a.zone();
    assert!(
        start.active && start.next.is_some(),
        "arena zone is live and heading somewhere"
    );
    for r in &a.robots {
        assert!(
            distance(r.x, r.y, start.x, start.y) <= start.radius,
            "{} spawned outside the zone",
            r.robot_id
        );
    }
    let mut previous = (start.x, start.y, start.radius);
    let mut travelled = 0.;
    for leg in 0..4 {
        for _ in 0..DRIFT_LEG_TICKS {
            a.step(BTreeMap::new(), &[]).unwrap();
            let z = a.zone();
            assert!(
                z.x - z.radius >= 0. && z.x + z.radius <= 24000.,
                "zone stays in the world"
            );
        }
        let z = a.zone();
        let moved = distance(previous.0, previous.1, z.x, z.y);
        assert!(
            moved < previous.2 + z.radius,
            "leg {leg}: new zone overlaps the old one"
        );
        travelled += moved;
        previous = (z.x, z.y, z.radius);
    }
    assert!(travelled > 2000., "the zone keeps moving: {travelled}");
    assert!(!a.finished, "the arena never ends");
    // Respawns land in the current zone.
    let z = a.zone();
    for r in a.robots.iter().filter(|r| r.alive && r.deaths > 0) {
        assert!(
            distance(r.x, r.y, z.x, z.y) <= z.radius + 600.,
            "respawned far outside the zone"
        );
    }
}

#[test]
fn arena_grows_past_the_bot_target_for_players() {
    let c: Config = serde_json::from_value(json!({"matchId":"grow","mode":"arena","capacity":6,"bots":2,"width":6000,"height":3750,"durationSeconds":10,"seed":3,"robots":[]})).unwrap();
    let mut a = Arena::new(c).unwrap();
    assert_eq!(a.robots.len(), 2, "bots fill to the target");
    for n in 0..4 {
        let reg = serde_json::from_value(json!({"robotId":format!("p{n}"),"name":"P","loadout":{"chassis":"generalist","weapon":"plasma"}})).unwrap();
        a.join(reg).unwrap();
    }
    a.step(BTreeMap::new(), &[]).unwrap();
    let humans = a.robots.iter().filter(|r| !r.bot).count();
    assert_eq!(humans, 4, "players join beyond the bot target");
    assert!(
        a.robots.iter().all(|r| !r.bot),
        "bots stepped aside for players"
    );
    let extra = serde_json::from_value(
        json!({"robotId":"p9","name":"P","loadout":{"chassis":"generalist","weapon":"plasma"}}),
    )
    .unwrap();
    a.join(extra).unwrap();
    let full = serde_json::from_value(
        json!({"robotId":"p10","name":"P","loadout":{"chassis":"generalist","weapon":"plasma"}}),
    )
    .unwrap();
    a.join(full).unwrap();
    let over = serde_json::from_value(
        json!({"robotId":"p11","name":"P","loadout":{"chassis":"generalist","weapon":"plasma"}}),
    )
    .unwrap();
    assert!(a.join(over).is_err(), "capacity is the hard limit");
}

#[test]
fn arena_joins_inside_a_step_are_reported() {
    let mut a = Arena::new(arena_config(4, 600)).unwrap();
    let good: Registration = serde_json::from_value(json!({"robotId":"human-9","name":"Bo","loadout":{"chassis":"generalist","weapon":"plasma"}})).unwrap();
    let dup = good.clone();
    a.step_joining(BTreeMap::new(), &[], vec![good, dup])
        .unwrap();
    assert!(a.events.iter().any(|e| e.r#type == "robot_joined"));
    assert!(
        a.events.iter().any(|e| e.r#type == "join_rejected"),
        "duplicate join rejected in the same tick"
    );
}

#[test]
fn climate_biomes_bring_landforms_and_ground_effects() {
    let w = World::generate(&world_config(json!({})));
    let lands: Vec<&Obstacle> = w
        .obstacles
        .iter()
        .filter(|o| o.id.starts_with("land-"))
        .collect();
    assert!(lands.iter().any(|o| o.material == "water"), "no lakes");
    assert!(lands.iter().any(|o| o.material == "cliff"), "no ridges");
    for o in &lands {
        for s in &w.sites {
            let (cx, cy) = (o.x + o.width / 2., o.y + o.height / 2.);
            assert!(
                distance(cx, cy, s.x, s.y) > 300.,
                "{} crowds {}",
                o.id,
                s.id
            );
        }
    }
    let materials: std::collections::BTreeSet<&str> =
        w.obstacles.iter().map(|o| o.material.as_str()).collect();
    for m in ["pine", "ice", "deadtree", "reeds", "water"] {
        assert!(materials.contains(m), "missing {m}: {materials:?}");
    }
    assert!(
        w.hazards.iter().any(|h| h.id.starts_with("biome-")),
        "no biome ground effects"
    );
    // Same seed, same climate.
    let again = World::generate(&world_config(json!({})));
    assert!(
        w.sites
            .iter()
            .zip(&again.sites)
            .all(|(a, b)| a.biome == b.biome)
    );
}

#[test]
fn small_maps_still_mix_biomes() {
    for seed in 1..40u64 {
        let w = World::generate(&world_config(
            json!({"width":2400,"height":1500,"siteCount":4,"seed":seed}),
        ));
        let biomes: std::collections::BTreeSet<&str> =
            w.sites.iter().map(|s| s.biome.as_str()).collect();
        assert_eq!(biomes.len(), 4, "seed {seed}: {biomes:?}");
    }
}

#[test]
fn water_blocks_movement_but_not_shots() {
    let mut w = World::generate(&world_config(
        json!({"width":2400,"height":1500,"siteCount":1,"coverPerSite":0,"seed":3}),
    ));
    w.obstacles.retain(|o| o.material == "water");
    w.obstacles.push(Obstacle {
        id: "pond".into(),
        shape: "aabb".into(),
        x: 1000.,
        y: 100.,
        width: 200.,
        height: 200.,
        material: "water".into(),
    });
    w.reindex();
    assert!(!w.clear(1100., 200., 18.), "robots cannot stand in water");
    assert!(
        w.wall_hit(900., 200., 1300., 200., 18.).is_some(),
        "water stops movement"
    );
    assert!(
        w.los(900., 200., 1300., 200.),
        "shots and sight cross water"
    );
}

#[test]
fn snapshot_loot_lists_its_contents_and_arena_fields() {
    let a = Arena::new(config(2)).unwrap();
    let snap = a.snapshot();
    let item = &snap["items"][0];
    assert!(
        item["contents"][0]["kind"].is_string(),
        "viewer needs loot contents: {item}"
    );
    assert_eq!(snap["mode"], "sandbox");
    assert_eq!(snap["endTick"], 1080 * 20);
    assert!(snap["robots"][0]["deaths"].is_number());
}

#[test]
fn arena_scores_kills_bounties_and_the_uplink() {
    let mut a = Arena::new(arena_config(8, 600)).unwrap();
    let (mut bounty, mut kills, mut moved) = (false, 0, false);
    for _ in 0..6000 {
        a.step(BTreeMap::new(), &[]).unwrap();
        bounty |= a.events.iter().any(|e| e.r#type == "bounty_claimed");
        kills += a.events.iter().filter(|e| e.r#type == "kill").count();
        moved |= a.events.iter().any(|e| e.r#type == "hill_moved");
    }
    assert!(kills > 0 && moved, "kills {kills}, hill moved {moved}");
    let total_kills: u32 = a.robots.iter().map(|r| r.kills).sum();
    let total_score: u32 = a.robots.iter().map(|r| r.score).sum();
    assert!(
        total_score >= total_kills * KILL_POINTS,
        "kills score points"
    );
    assert!(
        total_score > total_kills * KILL_POINTS,
        "Uplink or bounties add points (bounty seen: {bounty})"
    );
    let snap = a.snapshot();
    assert!(snap["hill"]["x"].is_number() && snap["robots"][0]["score"].is_number());
}

#[test]
fn arena_spawn_protection_blocks_damage_until_expiry() {
    let mut a = Arena::new(arena_config(2, 600)).unwrap();
    let t = a.tick;
    // A bot would fire and end its own protection; make it idle.
    a.robots[0].bot = false;
    a.robots[0].protected_until = t + SPAWN_PROTECT_TICKS;
    a.robots[0].burn_until = t + 10;
    let hp = a.robots[0].hp;
    a.step(BTreeMap::new(), &[]).unwrap();
    assert_eq!(a.robots[0].hp, hp, "protected robots ignore burn damage");
}

#[test]
fn arena_salvage_expires_and_site_loot_restocks() {
    let mut a = Arena::new(arena_config(2, 600)).unwrap();
    let site_loot = a.restock.len();
    assert!(site_loot > 0);
    a.world.containers.clear();
    a.world.containers.push(Container {
        item_id: "salvage-x".into(),
        x: 100.,
        y: 100.,
        contents: vec![],
        expires_at: Some(5),
    });
    for _ in 0..10 {
        a.step(BTreeMap::new(), &[]).unwrap();
    }
    assert!(
        !a.world.containers.iter().any(|c| c.item_id == "salvage-x"),
        "salvage fades"
    );
    for _ in 0..RESTOCK_TICKS {
        a.step(BTreeMap::new(), &[]).unwrap();
    }
    assert!(
        a.world
            .containers
            .iter()
            .filter(|c| c.item_id.starts_with("loot-"))
            .count()
            > site_loot / 2,
        "site loot restocks"
    );
}

#[test]
fn arena_spawns_are_protected_and_away_from_other_robots() {
    let c: Config = serde_json::from_value(json!({"matchId":"safe","mode":"arena","capacity":20,"width":24000,"height":15000,"durationSeconds":10,"seed":9,"siteCount":16,"robots":[]})).unwrap();
    let mut a = Arena::new(c).unwrap();
    assert!(
        a.robots
            .iter()
            .all(|r| r.protected_until == SPAWN_PROTECT_TICKS),
        "starting robots are protected"
    );
    let reg = serde_json::from_value(json!({"robotId":"late","name":"Late","loadout":{"chassis":"generalist","weapon":"plasma"}})).unwrap();
    a.step(BTreeMap::new(), &[]).unwrap();
    a.join(reg).unwrap();
    let me = a.robots.iter().find(|r| r.robot_id == "late").unwrap();
    assert!(
        me.protected_until >= a.tick + SPAWN_PROTECT_TICKS - 1,
        "joiners are protected for 3 seconds"
    );
    let nearest = a
        .robots
        .iter()
        .filter(|r| r.robot_id != "late" && r.alive)
        .map(|r| distance(r.x, r.y, me.x, me.y))
        .fold(f64::MAX, f64::min);
    assert!(
        nearest >= SAFE_SPAWN_DISTANCE,
        "joined {nearest:.0} units from another robot"
    );
}

#[test]
fn arena_land_renews_far_from_play_and_stays_deterministic() {
    let config = || -> Config {
        serde_json::from_value(json!({"matchId":"renew","mode":"arena","capacity":16,"bots":8,"width":42000,"height":26250,"durationSeconds":21600,"seed":21,"siteCount":64,"coverPerSite":8,"lootPerSite":16,"robots":[]})).unwrap()
    };
    let mut a = Arena::new(config()).unwrap();
    let revision = a.world.revision;
    let mut renewed = false;
    for _ in 0..(DRIFT_LEG_TICKS * (RENEW_LEGS + 2)) {
        a.step(BTreeMap::new(), &[]).unwrap();
        if a.events.iter().any(|e| e.r#type == "land_renewed") {
            renewed = true;
            let z = a.zone();
            // Nothing new appears inside or near the zone, or on a robot.
            for o in a.world.obstacles.iter().filter(|o| o.id.contains(':')) {
                let (x, y) = (o.x + o.width / 2., o.y + o.height / 2.);
                assert!(
                    distance(x, y, z.x, z.y) > z.radius + RENEW_MARGIN * 0.5,
                    "{} renewed inside play",
                    o.id
                );
            }
            for r in a.robots.iter().filter(|r| r.alive) {
                assert!(
                    a.world.clear(r.x, r.y, 1.)
                        || !a.world.obstacles.iter().any(|o| o.id.contains(':')
                            && r.x >= o.x
                            && r.x <= o.x + o.width
                            && r.y >= o.y
                            && r.y <= o.y + o.height),
                    "{} buried by new land",
                    r.robot_id
                );
            }
        }
    }
    assert!(renewed, "far land renews as the zone moves");
    assert!(
        a.world.revision > revision,
        "renewal bumps the layout revision"
    );
    assert!(
        a.world.obstacles.iter().any(|o| o.id.contains(':')),
        "renewed pieces are tagged by leg"
    );
    // Same seed, same renewals.
    let mut b = Arena::new(config()).unwrap();
    for _ in 0..(DRIFT_LEG_TICKS * (RENEW_LEGS + 2)) {
        b.step(BTreeMap::new(), &[]).unwrap();
    }
    let ids = |w: &World| w.obstacles.iter().map(|o| o.id.clone()).collect::<Vec<_>>();
    assert_eq!(ids(&a.world), ids(&b.world), "renewal is deterministic");
}
