export type Team = 'red' | 'blue';
export type MatchStatus = 'lobby' | 'queued' | 'running' | 'finished' | 'failed';

// Solo free-for-all matches assign every robot its own team (solo-01,
// solo-02, ...), so stored team fields are wider than the red|blue picker.
export type ArenaTeam = Team | (string & {});

export interface RobotSubmission {
  robotId: string;
  ownerBoxId?: string;
  displayName: string;
  bot?: boolean;
  team: ArenaTeam;
  startCommand: string;
  runtime: string;
  submittedAt: string;
}

export interface RobotSummary {
  robotId: string;
  name: string;
  team: ArenaTeam;
  hp: number;
  alive: boolean;
  failed: boolean;
  avgResponseMs: number;
  equipment?: string[];
  damageDealt?: number;
  damageTaken?: number;
  kills?: number;
  itemsPickedUp?: number;
  // Arena V2: public player handle, bot flag, deaths, and arena score.
  playerId?: string;
  bot?: boolean;
  deaths?: number;
  score?: number;
}

export interface PlayerLine {
  handle: string;
  name: string;
  matches: number;
  wins: number;
  kills: number;
  deaths: number;
  score: number;
  damageDealt: number;
  bestScore: number;
}

export interface Match {
 engineVersion?: number;
  matchId: string;
  status: MatchStatus;
  // Engine v4 lobby configuration (capacity, teamSize, bots, map).
  arenaConfig?: Record<string, unknown>;
  mode: string;
  seed: number;
  tickRate: number;
  robots: RobotSubmission[];
  winnerTeam?: string;
  robotSummaries?: RobotSummary[];
  error?: string;
  createdAt: string;
  mapId?: string;
  arenaWidth?: number;
  arenaHeight?: number;
  viewerCount?: number;
  durationTicks?: number;
  damageStats?: Record<string, { dealt: number; taken: number; kills: number; itemsPickedUp: number }>;
  eventSummary?: MatchEvent[];
  startedAt?: string;
  finishedAt?: string;
  practice?: boolean;
}

export interface MatchEvent extends ArenaEvent {
  sequence: number;
  tick: number;
}

export interface PlayerStats {
  playerId: string;
  handle: string;
  matches: number;
  wins: number;
  losses: number;
  draws: number;
  damageDealt: number;
  damageTaken: number;
  ratings: Record<string, number>;
  updatedAt: string;
}

export interface QueueStatus {
  status: 'idle' | 'waiting' | 'pairing' | 'matched';
  joinedAt?: string;
  matchId?: string;
  match?: Match;
}

export interface Replay {
  matchId: string;
  events: MatchEvent[];
  complete: boolean;
}

export interface AdminStatus {
  matches: Partial<Record<MatchStatus, number>>;
  queueDepth: number;
  viewers: Record<string, number>;
  agents: number;
  cloud: Record<string, string>;
}

export interface WeaponSlotState { kind: string; heat: number; readyAt: number; overheated: boolean }
export interface InventoryStack { kind: string; count: number }

export interface RobotState {
  // Engine v4 public fields.
  bot?: boolean;
  effects?: string[];
  // Engine v4 owner-only fields, present in your robot's private observation.
  loadout?: { chassis: string; weapon: string; modules: string[]; utilities: string[] };
  weapons?: WeaponSlotState[];
  activeWeapon?: number;
  inventory?: InventoryStack[];
  charges?: Record<string, number>;
  cooldowns?: Record<string, number>;
  actionResults?: string[];
  maxHp?: number;
  turretHeading?: number;
  energy?: number;
  maxEnergy?: number;
  shield?: number;
  maxShield?: number;
  // Active weapon kind from v4 snapshots (plasma, railgun, …).
  weapon?: string;
  vx?: number;
  vy?: number;
  placement?: number;
  damageDealt?: number;
  damageTaken?: number;
  kills?: number;
  // Arena mode: deaths so far and ticks until the robot respawns.
  deaths?: number;
  respawnIn?: number | null;
  // Arena scoring: points, current kill streak, bounty on this robot, and
  // spawn protection after a respawn.
  score?: number;
  streak?: number;
  bounty?: number;
  protected?: boolean;
  robotId: string;
  name: string;
  team: ArenaTeam;
  x: number;
  y: number;
  heading: number;
  hp: number;
  cooldown: number;
  alive: boolean;
  failed: boolean;
  connected: boolean;
  avgResponseMs: number;
  lastResponseMs: number;
  computeMs: number;
  memoryMb: number;
  equipment?: string[];
  itemsPickedUp?: number;
  killStreak?: number;
  streakName?: string;
  lastAction?: string;
  logs?: string[];
  dashCharges?: number;
  mineCharges?: number;
  visionRange?: number;
  scanResult?: ScanReport;
  events?: ArenaEvent[];
  messages?: string[];
}

export interface MineState {
  mineId: string;
  ownerId: string;
  team: ArenaTeam;
  x: number;
  y: number;
  spawnTick: number;
  armTick: number;
  active: boolean;
}

// Declared for protocol v3; the engine keeps turrets empty for now.
export interface TurretState {
  turretId: string;
  x: number;
  y: number;
  hp: number;
  maxHp: number;
  alive: boolean;
}

// Mirrors engine ZoneState; stage arrives once the backend adds staged
// collapse (1..3, where 3 marks the final zone).
export interface ZoneState {
  active: boolean;
  x: number;
  y: number;
  radius: number;
  damage: number;
  stage?: number;
}

export interface ScannedItem {
  itemId: string;
  type: string;
  x: number;
  y: number;
  distance: number;
}

export interface ScannedRobot {
  robotId: string;
  team: ArenaTeam;
  x: number;
  y: number;
  heading: number;
  hp: number;
  distance: number;
  cloaked: boolean;
}

export interface ScannedMine {
  mineId: string;
  x: number;
  y: number;
  distance: number;
  armed: boolean;
}

export interface ScannedHazard {
  id: string;
  type: string;
  x: number;
  y: number;
  width: number;
  height: number;
  distance: number;
}

export interface ScanReport {
  x: number;
  y: number;
  radius: number;
  items: ScannedItem[];
  enemies: ScannedRobot[];
  mines: ScannedMine[];
  hazards: ScannedHazard[];
}

export interface ArenaEvent {
  type: string;
  robotId?: string;
  targetId?: string;
  itemId?: string;
  damage?: number;
  value?: number;
  message?: string;
  itemType?: string;
  x?: number;
  y?: number;
  tick?: number;
}

export interface ArenaObstacle {
  id: string;
  shape: 'circle' | 'aabb';
  x: number;
  y: number;
  radius?: number;
  width?: number;
  height?: number;
  // Visual theme from the Rust generator: wall, hedge, glass, rock, brick,
  // metal, container, crate, barrel, tree, or sandbag. Collision and vision
  // treat every material identically.
  material?: string;
}

export interface ArenaItem {
  itemId: string;
  type: string;
  x: number;
  y: number;
  active: boolean;
  spawnTick: number;
  pickupRadius: number;
  respawnTick?: number;
  source?: string;
  rarity?: string;
  // Engine v4 loot containers list what they hold.
  contents?: { kind: string; count: number; charges?: number }[];
}

export interface Projectile {
  projectileId: string;
  ownerId: string;
  team: ArenaTeam;
  kind: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  damage: number;
  ttl: number;
}

export interface WorldSite {
  id?: string;
  kind: string;
  x: number;
  y: number;
  // District theme: urban, industrial, forest, or desert. Visual only.
  biome?: string;
}

export interface WorldHazard {
  id?: string;
  kind: string;
  x: number;
  y: number;
  width: number;
  height: number;
  damagePerSecond?: number;
}

// Owner-only drawings from a robot script (arena.draw); never simulated.
export interface DebugMark { kind: 'point' | 'line' | 'circle' | 'text'; x: number; y: number; x2?: number; y2?: number; r?: number; text?: string; color?: string }

export interface Snapshot {
  debug?: DebugMark[];
 revision?: number;
 transit?: {id:string; x:number; y:number; targetX:number; targetY:number}[];
  // Engine v4 static layout: sent on first delivery and on revision changes.
  hazards?: WorldHazard[];
  sites?: WorldSite[];
  type: 'snapshot';
  // Protocol version 3: adds mines, turrets (empty for now), dash/scan state.
  version: number;
  matchId: string;
  sequence: number;
  tick: number;
  tickRate?: number;
  status: 'running' | 'finished';
  winnerTeam?: string;
  robots: RobotState[];
  projectiles: Projectile[];
  events?: ArenaEvent[];
  // Recent kills, bounties, joins, and Uplink moves (last 10 ticks).
  feed?: ArenaEvent[];
  width?: number;
  height?: number;
  mapId?: string;
  mode?: string;
  endTick?: number;
  // Arena Uplink objective; null outside arena mode.
  hill?: { siteId: string; x: number; y: number; radius: number; movesAt: number; holder: string; contested: boolean; pointsPerSecond: number } | null;
  obstacles?: ArenaObstacle[];
  items?: ArenaItem[];
  mines?: MineState[];
  turrets?: TurretState[];
  zone?: ZoneState;
  announcements?: string[];
  overtime?: boolean;
}

export interface ArenaLayout {
  type: 'arena_layout';
  version: 1;
  matchId: string;
  mapId: string;
  width: number;
  height: number;
  obstacles: ArenaObstacle[];
}

export interface CloudStatus {
  status: 'ready' | 'unavailable';
  provider: string;
  endpoint: string;
  s3Bucket: string;
  dynamoTable: string;
  sqsQueue: string;
  error?: string;
}

export interface AgentEnrollment {
  robotId: string;
  status: string;
}

export interface ScriptTemplate {
  name: string;
  description: string;
  source: string;
}

export interface ScriptVersion {
  versionId: string;
  createdAt: string;
}

export interface BoxLimits {
  cpus: number;
  memoryMb: number;
  storageBytes: number;
  pids: number;
}

export interface RobotBox {
  boxId: string;
  status: string;
  sshHost: string;
  sshPort: number;
  sshUser: string;
  keyFingerprint?: string;
  usageBytes: number;
  agentStatus: string;
  activeRobotId?: string;
  activeMatchId?: string;
  error?: string;
  limits: BoxLimits;
  updatedAt: string;
}

export interface RobotEnrollmentResponse {
  match: Match;
  agent: AgentEnrollment;
}

export function isNewerSnapshot(current: Snapshot | null, incoming: Snapshot): boolean {
  return current === null || incoming.matchId !== current.matchId || incoming.sequence > current.sequence;
}
