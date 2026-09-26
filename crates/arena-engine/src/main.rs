use arena_engine::{catalog, model::*, protocol, simulation::Arena};
use serde_json::{Value, json};
use std::{
    collections::BTreeMap,
    os::unix::{fs::PermissionsExt, net::UnixListener},
};
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    if args.get(1).is_some_and(|a| a == "catalogue") {
        println!("{}", catalog::catalogue());
        return Ok(());
    }
    if args.get(1).is_some_and(|a| a == "benchmark") {
        return benchmark(&args);
    }
    if args.get(1).is_some_and(|a| a == "generate") {
        return generate();
    }
    let path = args
        .get(1)
        .ok_or("usage: arena-engine SOCKET | catalogue | benchmark [robots] [ticks] | generate")?;
    let listener = UnixListener::bind(path)?;
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o600))?;
    let (mut socket, _) = listener.accept()?;
    let mut arena: Option<Arena> = None;
    while let Ok(frame) = protocol::read(&mut socket) {
        let result = (|| -> Result<Value, String> {
            if frame.version != 4 {
                return Err("unsupported protocol".into());
            }
            let body: Value = serde_json::from_slice(&frame.payload).map_err(|e| e.to_string())?;
            match frame.kind.as_str() {
                "start" => {
                    if arena.is_some() {
                        return Err("match already started".into());
                    }
                    arena = Some(Arena::new(
                        serde_json::from_value(body).map_err(|e| e.to_string())?,
                    )?);
                }
                "restore" => {
                    if arena.is_some() {
                        return Err("match already started".into());
                    }
                    if body["engineVersion"] != catalog::VERSION
                        || body["architecture"] != std::env::consts::ARCH
                    {
                        return Err("checkpoint engine or architecture mismatch".into());
                    }
                    let mut restored: Arena =
                        serde_json::from_value(body["arena"].clone()).map_err(|e| e.to_string())?;
                    restored.config.validate()?;
                    restored.reindex();
                    arena = Some(restored);
                }
                "observe" => {
                    let a = arena.as_ref().ok_or("match not started")?;
                    let id = body["robotId"].as_str().ok_or("robotId required")?;
                    let i = a
                        .robots
                        .iter()
                        .position(|r| r.robot_id == id)
                        .ok_or("robot not found")?;
                    return Ok(a.observation(i));
                }
                "step" => {
                    let actions: BTreeMap<String, Action> =
                        serde_json::from_value(body.get("actions").cloned().unwrap_or(json!({})))
                            .map_err(|e| e.to_string())?;
                    let withdrawals: Vec<String> = serde_json::from_value(
                        body.get("withdrawals").cloned().unwrap_or(json!([])),
                    )
                    .map_err(|e| e.to_string())?;
                    arena
                        .as_mut()
                        .ok_or("match not started")?
                        .step(actions, &withdrawals)?;
                }
                "previewEdit" => {
                    return arena
                        .as_ref()
                        .ok_or("match not started")?
                        .preview_edit(&serde_json::from_value(body).map_err(|e| e.to_string())?);
                }
                "edit" => {
                    arena
                        .as_mut()
                        .ok_or("match not started")?
                        .apply_edit(&serde_json::from_value(body).map_err(|e| e.to_string())?)?;
                }
                "checkpoint" => {
                    return Ok(
                        json!({"engineVersion":catalog::VERSION,"architecture":std::env::consts::ARCH,"arena":arena.as_ref().ok_or("match not started")?}),
                    );
                }
                "catalogue" => return Ok(catalog::catalogue()),
                _ => return Err("unknown command".into()),
            }
            let a = arena.as_ref().unwrap();
            let observations: BTreeMap<_, _> = a
                .robots
                .iter()
                .enumerate()
                .filter(|(_, r)| !r.bot && (a.tick.is_multiple_of(2) || a.finished))
                .map(|(i, r)| {
                    let view = if r.alive {
                        i
                    } else {
                        a.robots
                            .iter()
                            .position(|ally| ally.alive && ally.team == r.team)
                            .unwrap_or(i)
                    };
                    let mut observation = a.observation_with_geometry(view, false);
                    observation["controlledRobotId"] = json!(r.robot_id);
                    observation["spectating"] = json!(!r.alive);
                    (r.robot_id.clone(), observation)
                })
                .collect();
            Ok(
                json!({"snapshot":a.snapshot(),"observations":observations,"geometry":a.geometry(),"stateHash":a.state_hash()}),
            )
        })();
        match result {
            Ok(v) => protocol::write(&mut socket, "result", &v)?,
            Err(e) => protocol::write(&mut socket, "error", &json!({"error":e}))?,
        }
    }
    let _ = std::fs::remove_file(path);
    Ok(())
}
fn generate() -> Result<(), Box<dyn std::error::Error>> {
    use arena_engine::world::World;
    use std::io::Read;
    let mut input = String::new();
    std::io::stdin().read_to_string(&mut input)?;
    let mut body: Value = serde_json::from_str(&input)?;
    if body
        .get("matchId")
        .and_then(|v| v.as_str())
        .is_none_or(str::is_empty)
    {
        body["matchId"] = json!("preview");
    }
    let config: Config = serde_json::from_value(body)?;
    config.validate()?;
    let world = World::generate(&config);
    println!(
        "{}",
        json!({"width":config.width,"height":config.height,"seed":config.seed,"sites":world.sites,"obstacles":world.obstacles,"transit":world.transit,"containers":world.containers,"hazards":world.hazards,"zones":world.zones})
    );
    Ok(())
}
fn benchmark(args: &[String]) -> Result<(), Box<dyn std::error::Error>> {
    let count: usize = args.get(2).map_or(Ok(256), |s| s.parse())?;
    let ticks: u32 = args.get(3).map_or(Ok(1000), |s| s.parse())?;
    if ticks == 0 {
        return Err("ticks must be positive".into());
    }
    let scenario = args.get(4).map_or("spread", String::as_str);
    if !["spread", "dense", "match"].contains(&scenario) {
        return Err("scenario must be spread, dense or match".into());
    }
    let c: Config = serde_json::from_value(
        json!({"matchId":"benchmark","mode":if scenario=="match"{"br-solo"}else{"sandbox"},"capacity":count,"durationSeconds":if scenario=="match"{1080}else{2700},"seed":42}),
    )?;
    let mut arena = Arena::new(c)?;
    let mut samples = vec![];
    let mut pipeline = vec![];
    let mut max_projectiles = 0;
    if scenario == "dense" {
        arena.world.obstacles.clear();
        arena.world.containers.clear();
        arena.reindex();
    }
    for tick in 0..ticks {
        if scenario == "dense" {
            for (i, r) in arena.robots.iter_mut().enumerate() {
                r.hp = r.max_hp;
                r.alive = true;
                r.x = 20000. + (i % 16) as f64 * 36.;
                r.y = 12000. + (i / 16) as f64 * 36.;
            }
            arena.reindex();
        }
        let start = std::time::Instant::now();
        arena.step(BTreeMap::new(), &[])?;
        samples.push(start.elapsed().as_micros());
        if tick % 2 == 0 {
            let _ = serde_json::to_vec(&arena.snapshot())?;
            let _ = arena.state_hash();
        }
        pipeline.push(start.elapsed().as_micros());
        max_projectiles = max_projectiles.max(arena.projectiles.len());
        if arena.finished {
            break;
        }
    }
    samples.sort();
    pipeline.sort();
    println!(
        "{}",
        json!({"robots":count,"ticks":samples.len(),"scenario":scenario,"p50Us":samples[samples.len()/2],"p99Us":samples[samples.len()*99/100],"pipelineP99Us":pipeline[pipeline.len()*99/100],"maxProjectiles":max_projectiles,"alive":arena.robots.iter().filter(|r|r.alive).count(),"finished":arena.finished,"hash":arena.state_hash()})
    );
    Ok(())
}
