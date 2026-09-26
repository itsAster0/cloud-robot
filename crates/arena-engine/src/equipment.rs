use crate::{catalog, model::*, simulation::Arena, world::*};

impl Arena {
    pub(crate) fn equipment_actions(&mut self, i: usize, action: &Action) {
        if let Some(e) = &action.equip {
            let result = self.equip_from_container(i, e);
            if let Err(code) = result {
                self.robots[i].action_results.push(code.into());
            }
        }
        if let Some(e) = &action.drop_equipment {
            let r = &self.robots[i];
            if !self.world.can_add_container(r.x, r.y) {
                self.robots[i]
                    .action_results
                    .push("CONTAINER_BUDGET_EXCEEDED".into());
                return;
            }
            match Self::remove_equipment(&mut self.robots[i], &e.group, e.slot) {
                Ok(item) => {
                    let r = &self.robots[i];
                    let (x, y) = (r.x, r.y);
                    let item_id = self.id("equipment");
                    self.world.containers.push(Container {
                        item_id,
                        x,
                        y,
                        contents: vec![item],
                    });
                }
                Err(code) => self.robots[i].action_results.push(code.into()),
            }
        }
    }
    fn equip_from_container(&mut self, i: usize, e: &Equip) -> Result<(), &'static str> {
        let c = self
            .world
            .containers
            .iter()
            .position(|c| c.item_id == e.container)
            .ok_or("CONTAINER_NOT_FOUND")?;
        let r = &self.robots[i];
        let container = &self.world.containers[c];
        if distance(r.x, r.y, container.x, container.y) > 35.
            || !self.world.los(r.x, r.y, container.x, container.y)
        {
            return Err("PICKUP_OUT_OF_RANGE");
        }
        let slot = container
            .contents
            .iter()
            .position(|s| s.kind == e.kind && s.count > 0)
            .ok_or("ITEM_NOT_FOUND")?;
        let item = container.contents[slot].clone();
        // Work on a copy. Invalid swaps leave both inventories unchanged.
        let mut next = self.robots[i].clone();
        let replaced = if let Some(kind) = item.kind.strip_prefix("weapon:") {
            if catalog::weapon(kind).is_none() || e.slot > next.weapons.len() || e.slot >= 2 {
                return Err("INVALID_EQUIPMENT_SLOT");
            }
            if next
                .weapons
                .iter()
                .enumerate()
                .any(|(n, w)| n != e.slot && w.kind == kind)
            {
                return Err("DUPLICATE_EQUIPMENT");
            }
            let weapon = item.weapon_state.clone().unwrap_or_else(|| WeaponSlot {
                kind: kind.into(),
                ..Default::default()
            });
            if e.slot == next.weapons.len() {
                next.weapons.push(weapon);
                None
            } else {
                let old = std::mem::replace(&mut next.weapons[e.slot], weapon);
                Some(Stack {
                    kind: format!("weapon:{}", old.kind),
                    count: 1,
                    weapon_state: Some(old),
                    ..Default::default()
                })
            }
        } else if catalog::MODULES.contains(&item.kind.as_str()) {
            if e.slot > next.loadout.modules.len() || e.slot >= 2 {
                return Err("INVALID_EQUIPMENT_SLOT");
            }
            if next
                .loadout
                .modules
                .iter()
                .enumerate()
                .any(|(n, m)| n != e.slot && m == &item.kind)
            {
                return Err("DUPLICATE_EQUIPMENT");
            }
            let old = if e.slot == next.loadout.modules.len() {
                next.loadout.modules.push(item.kind.clone());
                None
            } else {
                Some(Stack {
                    kind: std::mem::replace(&mut next.loadout.modules[e.slot], item.kind.clone()),
                    count: 1,
                    ..Default::default()
                })
            };
            Self::module_stats(&mut next);
            old
        } else if let Some(kind) = item.kind.strip_prefix("utility:") {
            if !catalog::UTILITIES.iter().any(|(id, _)| *id == kind)
                || e.slot > next.loadout.utilities.len()
                || e.slot >= 2
            {
                return Err("INVALID_EQUIPMENT_SLOT");
            }
            if next
                .loadout
                .utilities
                .iter()
                .enumerate()
                .any(|(n, u)| n != e.slot && u == kind)
            {
                return Err("DUPLICATE_EQUIPMENT");
            }
            let old = if e.slot < next.loadout.utilities.len() {
                Some(Self::remove_equipment(&mut next, "utility", e.slot)?)
            } else {
                None
            };
            next.loadout.utilities.insert(e.slot, kind.into());
            next.charges.insert(
                kind.into(),
                item.charges.unwrap_or(match kind {
                    "mine_dispenser" => 3,
                    "smoke_projector" => 2,
                    _ => 0,
                }),
            );
            next.cooldowns
                .insert(kind.into(), item.ready_at.unwrap_or(0));
            old
        } else {
            return Err("ITEM_NOT_EQUIPMENT");
        };
        // A swap can add one stack while consuming only part of a loot stack.
        if replaced.is_some() && item.count > 1 && container.contents.len() >= 12 {
            return Err("CONTAINER_FULL");
        }
        self.robots[i] = next;
        self.robots[i].channel = None;
        let contents = &mut self.world.containers[c].contents;
        contents[slot].count -= 1;
        contents.retain(|s| s.count > 0);
        if let Some(old) = replaced {
            contents.push(old);
        }
        Ok(())
    }
    fn remove_equipment(r: &mut Robot, group: &str, slot: usize) -> Result<Stack, &'static str> {
        let item = match group {
            "weapon" => {
                if r.weapons.len() <= 1 {
                    return Err("LAST_WEAPON_REQUIRED");
                }
                if slot >= r.weapons.len() {
                    return Err("INVALID_EQUIPMENT_SLOT");
                }
                let w = r.weapons.remove(slot);
                r.active_weapon = 0;
                Stack {
                    kind: format!("weapon:{}", w.kind),
                    count: 1,
                    weapon_state: Some(w),
                    ..Default::default()
                }
            }
            "module" => {
                if slot >= r.loadout.modules.len() {
                    return Err("INVALID_EQUIPMENT_SLOT");
                }
                let kind = r.loadout.modules.remove(slot);
                Self::module_stats(r);
                Stack {
                    kind,
                    count: 1,
                    ..Default::default()
                }
            }
            "utility" => {
                if slot >= r.loadout.utilities.len() {
                    return Err("INVALID_EQUIPMENT_SLOT");
                }
                let kind = r.loadout.utilities.remove(slot);
                Stack {
                    kind: format!("utility:{kind}"),
                    count: 1,
                    charges: r.charges.remove(&kind),
                    ready_at: r.cooldowns.remove(&kind),
                    ..Default::default()
                }
            }
            _ => return Err("INVALID_EQUIPMENT_SLOT"),
        };
        r.channel = None;
        Ok(item)
    }
}
