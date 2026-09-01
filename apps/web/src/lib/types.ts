export type Team = 'red' | 'blue';
export type MatchStatus = 'lobby' | 'queued' | 'running' | 'finished' | 'failed';

export interface RobotSubmission {
  robotId: string;
  ownerBoxId?: string;
  displayName: string;
  team: Team;
  startCommand: string;
  runtime: string;
  submittedAt: string;
}

export interface RobotSummary {
  robotId: string;
  name: string;
  team: Team;
  hp: number;
  alive: boolean;
  failed: boolean;
  avgResponseMs: number;
  equipment?: string[];
}

export interface Match {
  matchId: string;
  status: MatchStatus;
  mode: string;
  seed: number;
  tickRate: number;
  robots: RobotSubmission[];
  winnerTeam?: Team | 'draw';
  robotSummaries?: RobotSummary[];
  error?: string;
  createdAt: string;
}

export interface RobotState {
  robotId: string;
  name: string;
  team: Team;
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
  lastAction?: string;
  logs?: string[];
}

export interface ArenaEvent {
  type: string;
  robotId?: string;
  targetId?: string;
  damage?: number;
  message?: string;
}

export interface Projectile {
  projectileId: string;
  ownerId: string;
  team: Team;
  kind: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  damage: number;
  ttl: number;
}

export interface Snapshot {
  type: 'snapshot';
  version: number;
  matchId: string;
  sequence: number;
  tick: number;
  status: 'running' | 'finished';
  winnerTeam?: Team | 'draw';
  robots: RobotState[];
  projectiles: Projectile[];
  events?: ArenaEvent[];
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
