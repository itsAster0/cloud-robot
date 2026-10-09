// Plain-language descriptions of map things for the viewer's info panel.
// Numbers mirror crates/arena-engine (catalog.rs, simulation.rs, world.rs).

export type LootGroup = 'weapon' | 'heal' | 'module' | 'utility' | 'supply';

export const lootColor: Record<LootGroup, string> = {
  weapon: '#ff7a5c', heal: '#5fd68a', module: '#6aa8ff', utility: '#c08cff', supply: '#efc86b',
};

const CONSUMABLES: Record<string, string> = {
  repair_pack: 'Restores 25 HP.',
  medkit: 'Restores 60 HP; slower to use.',
  shield_cell: 'Adds 25 shield.',
  energy_cell: 'Adds 40 energy.',
  cleanser: 'Clears burn, slow, and other status effects.',
  utility_refill: 'Refills utility charges.',
};
const MODULES: Record<string, string> = {
  reinforced_plating: 'Module: more maximum health.',
  optics: 'Module: longer vision range.',
  capacitor: 'Module: larger energy pool.',
  cooling_system: 'Module: weapons overheat more slowly.',
  mobility_tuning: 'Module: faster movement.',
  shield_reservoir: 'Module: larger shield.',
};
const UTILITIES: Record<string, string> = {
  cloak_emitter: 'Utility: turn briefly invisible.',
  mine_dispenser: 'Utility: drop proximity mines.',
  smoke_projector: 'Utility: smoke that blocks sight.',
  repair_field: 'Utility: heals robots standing in it.',
};

const title = (id: string) => id.replace(/_/g, ' ').replace(/^\w/, c => c.toUpperCase());

/** Label, group, and effect of one loot kind such as `weapon:railgun`. */
export function lootInfo(kind: string): { label: string; group: LootGroup; text: string } {
  const [prefix, rest] = kind.includes(':') ? kind.split(':') : ['', kind];
  if (prefix === 'weapon') return { label: title(rest), group: 'weapon', text: 'Weapon: swaps into your weapon slot when picked up.' };
  if (prefix === 'utility') return { label: title(rest), group: 'utility', text: UTILITIES[rest] ?? 'Utility.' };
  if (MODULES[rest]) return { label: title(rest), group: 'module', text: MODULES[rest] };
  if (rest === 'repair_pack' || rest === 'medkit' || rest === 'shield_cell') return { label: title(rest), group: 'heal', text: CONSUMABLES[rest] };
  return { label: title(rest), group: 'supply', text: CONSUMABLES[rest] ?? 'Supply.' };
}

/** Colour for a loot container, from its first item. */
export function lootGroupOf(contents?: { kind: string }[]): LootGroup {
  return contents?.length ? lootInfo(contents[0].kind).group : 'supply';
}

export const biomeInfo: Record<string, { name: string; text: string }> = {
  urban: { name: 'City district', text: 'Brick rooms with doorways and crate stacks. Lots of close cover.' },
  industrial: { name: 'Industrial yard', text: 'Shipping-container rows, metal sheds, and barrels. Long lanes between walls.' },
  forest: { name: 'Forest', text: 'Tree groves and hedgerows. Soft cover that breaks line of sight.' },
  desert: { name: 'Desert', text: 'Open sand with rock outcrops and sandbag outposts. Heat vents burn robots that stand in them.' },
  snow: { name: 'Snowfield', text: 'Pine stands and ice boulders. Snowdrifts slow robots down.' },
  swamp: { name: 'Swamp', text: 'Ponds, dead trees, and reeds. Bogs slow robots down; water blocks movement but not shots.' },
};

export const siteInfo: Record<string, string> = {
  repair_depot: 'Stocks repair packs, medkits, shield cells, and repair fields.',
  armoury: 'Stocks machine guns, shotguns, and cooling systems.',
  relay_station: 'Stocks optics, energy cells, and capacitors.',
  transit_station: 'Stocks energy, mobility tuning, and utility refills. Has a transit pad.',
  bunker: 'Stocks plating, shotguns, mines, and smoke.',
  exposed_supply: 'Stocks railguns, grenades, and cleansers, out in the open.',
};

export const materialInfo: Record<string, { name: string; text: string }> = {
  water: { name: 'Water', text: 'Robots cannot drive through it, but shots and sight pass over.' },
  cliff: { name: 'Mountain rock', text: 'Part of a ridge. Blocks movement and shots; look for the pass.' },
  pine: { name: 'Pine tree', text: 'Blocks movement and shots.' },
  ice: { name: 'Ice boulder', text: 'Blocks movement and shots.' },
  deadtree: { name: 'Dead tree', text: 'Blocks movement and shots.' },
  reeds: { name: 'Reeds', text: 'Dense reeds. Block movement and shots.' },
  tree: { name: 'Tree', text: 'Blocks movement and shots.' },
  hedge: { name: 'Hedgerow', text: 'Blocks movement and shots.' },
  glass: { name: 'Glass pane', text: 'Blocks movement and shots.' },
  rock: { name: 'Rock', text: 'Blocks movement and shots.' },
  brick: { name: 'Brick wall', text: 'Blocks movement and shots.' },
  metal: { name: 'Metal wall', text: 'Blocks movement and shots.' },
  container: { name: 'Shipping container', text: 'Blocks movement and shots.' },
  crate: { name: 'Crate', text: 'Blocks movement and shots.' },
  barrel: { name: 'Barrel', text: 'Blocks movement and shots.' },
  sandbag: { name: 'Sandbags', text: 'Blocks movement and shots.' },
  wall: { name: 'Wall', text: 'Blocks movement and shots.' },
};

/** Name and effect of a hazard, which depends on the biome it sits in. */
export function hazardInfo(kind: string, biome: string, damagePerSecond = 0): { name: string; text: string } {
  if (kind === 'slow') {
    const name = biome === 'snow' ? 'Snowdrift' : biome === 'swamp' ? 'Bog' : 'Flooded ground';
    return { name, text: 'Robots inside move slowly. No damage.' };
  }
  const name = biome === 'desert' ? 'Heat vent' : 'Slag pool';
  return { name, text: `Burns robots inside for ${damagePerSecond || 4} HP per second.` };
}
