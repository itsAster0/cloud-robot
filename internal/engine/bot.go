package engine

import (
	"context"
	"hash/fnv"
	"math"
	"math/rand/v2"
)

type BotDifficulty string

const (
	BotDummy        BotDifficulty = "dummy"
	BotRookie       BotDifficulty = "rookie"
	BotFighter      BotDifficulty = "fighter"
	BotSharpshooter BotDifficulty = "sharpshooter"
)

type BotPersonality string

const (
	PersonalityAggressive BotPersonality = "aggressive"
	PersonalityEvasive    BotPersonality = "evasive"
	PersonalityCamper     BotPersonality = "camper"
	PersonalityMixed      BotPersonality = "mixed"
)

// botState enumerates the behavior states the controller re-evaluates every
// tick. Transitions are pure functions of the observed world, so a given
// match always produces the same state trace.
type botState string

const (
	stateCombat botState = "combat"
	stateHide   botState = "hide"
	stateEscape botState = "escape"
	stateHunt   botState = "hunt"
	stateItem   botState = "item"
	statePatrol botState = "patrol"
)

const (
	underFireTicks    = 45  // HP-drop recency window that keeps a bot "under fire"
	hideCooldownGate  = 3   // Cooldown above this counts as reloading
	combatMinHP       = 40  // below this HP bots stop dueling and look for cover
	memoryStaleTicks  = 60  // last-known-enemy age before hunting gives up
	huntScanInterval  = 90  // ticks between hunting scans
	coverSampleCount  = 10  // candidate cover points per hide episode
	dodgeThreatRadius = 30  // projectile path clearance that triggers a sidestep
	dodgeLookahead    = 6.0 // ticks of projectile travel checked for threats
	mineFleeOdds      = 0.7 // seeded gate: chance a fleeing bot drops its mine
	botEscapeTicks    = 5   // stuck episode length before a new reverse heading

	wanderProbeInterval = 24 // ticks between travel progress samples
	wanderProbeDistance = 16 // movement below this counts as stuck
	wanderMinTicks      = 12 // shortest wander burst
	wanderExtraTicks    = 18 // extra burst length ceiling
	spottedReactionGap  = 90 // ticks before a re-sighting reopens the reaction
)

// botTuning holds the per-difficulty knobs. Rookie keeps a charge-and-shoot
// shape; fighter and sharpshooter share the full state machine and differ in
// aim lead and preferred engagement distance only.
type botTuning struct {
	combatRange   float64
	leadFactor    float64
	escapeHP      int
	fleeOutgunned bool
	hide          bool
	hunt          bool
	dodge         bool
	dash          bool
	mine          bool
	scan          bool
	hideMax       int
}

func (b *BotController) tuning() botTuning {
	t := botTuning{combatRange: 200, leadFactor: .5, escapeHP: 30, fleeOutgunned: true, hide: true, hunt: true, dodge: true, dash: true, mine: true, scan: true, hideMax: 60}
	switch b.difficulty {
	case BotRookie:
		t = botTuning{combatRange: 140, escapeHP: 20, hideMax: 0}
	case BotSharpshooter:
		t.combatRange = 280
		t.leadFactor = 1
	}
	switch b.personality {
	case PersonalityAggressive:
		t.combatRange -= 40
		t.hideMax = 30
	case PersonalityEvasive:
		t.combatRange += 30
		t.escapeHP += 5
	case PersonalityCamper:
		t.combatRange += 80
	}
	return t
}

// botRolls carries the fixed-shape random draws every tick consumes. Each
// non-dummy tick draws exactly these six values in this order no matter what
// the world looks like, so the bot's RNG stream never branches on what it
// happens to see and replays stay byte-stable.
type botRolls struct{ escape, sign, dodge, dur, move, wander float64 }

func (b *BotController) roll() botRolls {
	return botRolls{escape: b.rng.Float64(), sign: b.rng.Float64(), dodge: b.rng.Float64(), dur: b.rng.Float64(), move: b.rng.Float64(), wander: b.rng.Float64()}
}

type botMemory struct {
	x, y float64
	tick int
}

type BotController struct {
	difficulty  BotDifficulty
	personality BotPersonality
	obstacles   []Obstacle
	width       float64
	height      float64
	rng         *rand.Rand
	ticks       int
	strafePhase int
	stuckTicks  int
	escapeTurn  float64
	driftBias   float64
	nextDriftAt int
	state       botState
	prevHP      int
	hpSeen      bool
	// lastDamageTick is the controller tick of the most recent observed HP
	// drop; the engine never reports damage directly to controllers.
	lastDamageTick int
	lastKnown      botMemory
	hideTicks      int
	hideBase       float64
	escapeTicks    int
	escapeMineDone bool
	dodgeTicks     int
	dodgeSide      float64
	dodgeAngle     float64
	anchors        [4][2]float64
	patrolIndex    int
	patrolUntil    int
	patrolOffsetX  float64
	patrolOffsetY  float64
	// Travel-progress probe: when a moving state stops making headway the
	// controller opens a seeded wander burst toward a fresh heading.
	probeTick      int
	probeX, probeY float64
	wanderTicks    int
	wanderHeading  float64
	// Orbit window bookkeeping so combat ring-strafing flips side randomly
	// instead of tracing the same circle every episode.
	orbitWindow int
	orbitSide   float64
	// First-sight bookkeeping: each persona opens a short reaction episode
	// when an enemy appears after a long gap.
	lastSeenTick int
	spottedTicks int
	spottedSide  float64
	nextScanAt   int
	scanX, scanY float64
	scanPending  bool
	world        WorldState
	hasWorld     bool
}

func NewBotController(id string, difficulty BotDifficulty, personality BotPersonality, m MapDefinition) *BotController {
	personality = resolvePersonality(id, personality)
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	seed := h.Sum64()
	rng := rand.New(rand.NewPCG(seed, seed^0xd1b54a32d192ed03))
	bot := &BotController{
		difficulty:  difficulty,
		personality: personality,
		obstacles:   append([]Obstacle(nil), m.Obstacles...),
		width:       m.Width,
		height:      m.Height,
		rng:         rng,
		strafePhase: rng.IntN(30),
		driftBias:   rng.Float64()*4 - 2,
		nextDriftAt: 30 + rng.IntN(30),
		patrolUntil: 120,
	}
	if bot.width <= 0 {
		bot.width = ArenaWidth
	}
	if bot.height <= 0 {
		bot.height = ArenaHeight
	}
	bot.anchors = patrolAnchors(bot.obstacles, bot.width, bot.height, id, personality == PersonalityCamper)
	bot.patrolIndex = int(seed % 4)
	return bot
}

// resolvePersonality maps the match-level setting onto the bot persona.
// Mixed assigns every bot a stable persona derived from its robot ID, so one
// match fields varied opponents without extra configuration.
func resolvePersonality(id string, personality BotPersonality) BotPersonality {
	if personality != PersonalityMixed {
		return personality
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	_, _ = h.Write([]byte("|persona"))
	switch h.Sum64() % 3 {
	case 0:
		return PersonalityAggressive
	case 1:
		return PersonalityEvasive
	default:
		return PersonalityCamper
	}
}

func (b *BotController) Close() {}

// SetWorld receives the engine's vision-filtered view each tick. Items,
// projectiles, and the zone drive the item-seeking, dodge, and escape
// behaviors; enemies arrive separately through the Tick robot view, already
// filtered by range and cloak.
func (b *BotController) SetWorld(world WorldState) {
	b.world = world
	b.hasWorld = true
}

// jitter adds per-tick noise plus a slow random drift bias. Both are seeded
// from the robot ID, so a match stays deterministic, but the drift breaks the
// symmetric loops that deterministic steering otherwise settles into.
func (b *BotController) jitter(turn float64) float64 {
	if b.ticks >= b.nextDriftAt {
		b.driftBias = b.rng.Float64()*4 - 2
		b.nextDriftAt = b.ticks + 30 + b.rng.IntN(30)
	}
	return turn + b.driftBias + (b.rng.Float64()*2-1)*1.5
}

// trackProgress samples the bot position every wanderProbeInterval ticks. A
// traveling state that barely moved opens a seeded wander burst toward a
// fresh heading, which breaks anchor orbits and wall-hugging loops that
// deterministic steering otherwise repeats forever.
func (b *BotController) trackProgress(self RobotState, rolls botRolls) {
	if b.wanderTicks > 0 || b.ticks-b.probeTick < wanderProbeInterval {
		return
	}
	traveled := math.Hypot(self.X-b.probeX, self.Y-b.probeY)
	b.probeTick, b.probeX, b.probeY = b.ticks, self.X, self.Y
	if traveled >= wanderProbeDistance {
		return
	}
	b.wanderTicks = wanderMinTicks + int(rolls.wander*wanderExtraTicks)
	b.wanderHeading = normalizeDegrees(rolls.escape*360 + rolls.dodge*47)
}

// wanderIntent drives the burst: a committed run toward the burst heading
// with normal jitter, long enough to carry the bot out of the loop it was in.
func (b *BotController) wanderIntent(self RobotState, rolls botRolls) Intent {
	return b.finish(Intent{Move: MaxMovePerTick * .8, Turn: b.jitter(shortestTurn(self.Heading, b.wanderHeading))}, rolls.move)
}

func (b *BotController) Tick(_ context.Context, self RobotState, robots []RobotState) (Intent, error) {
	b.ticks++
	if b.difficulty == BotDummy {
		return Intent{}, nil
	}
	rolls := b.roll()
	b.trackDamage(self)
	b.applyScanResult(self)
	b.trackProgress(self, rolls)
	tuning := b.tuning()
	enemy, seen := nearestVisibleEnemy(self, robots, b.obstacles)
	if seen {
		b.lastKnown = botMemory{x: enemy.X, y: enemy.Y, tick: b.ticks}
		// First sight or re-contact after a long gap opens a short
		// personality-flavored reaction episode inside combat.
		if b.lastSeenTick == 0 || b.ticks-b.lastSeenTick > spottedReactionGap {
			b.spottedTicks = 6 + int(rolls.wander*6)
			if b.personality == PersonalityEvasive {
				b.spottedTicks += 6
			}
			b.spottedSide = 1
			if rolls.sign < .5 {
				b.spottedSide = -1
			}
		}
		b.lastSeenTick = b.ticks
	}
	b.enterState(b.selectState(self, enemy, seen, tuning), rolls)
	// A stuck traveler breaks off into a seeded wander burst; fights, hides,
	// and retreats keep their own dedicated steering.
	if b.wanderTicks > 0 {
		switch b.state {
		case stateCombat, stateHide, stateEscape:
			b.wanderTicks = 0
		default:
			b.wanderTicks--
			return b.avoidObstacles(self, b.wanderIntent(self, rolls), rolls), nil
		}
	}
	var intent Intent
	switch b.state {
	case stateCombat:
		intent = b.combatIntent(self, enemy, tuning, rolls)
	case stateHide:
		intent = b.hideIntent(self, enemy, seen, tuning, rolls)
	case stateEscape:
		intent = b.escapeIntent(self, enemy, seen, tuning, rolls)
	case stateHunt:
		intent = b.huntIntent(self, tuning, rolls)
	case stateItem:
		intent = b.itemIntent(self, rolls)
	default:
		intent = b.patrolIntent(self, rolls)
	}
	return b.avoidObstacles(self, intent, rolls), nil
}

// trackDamage timestamps HP drops so the bot knows when it is under fire
// without trusting anything agents report.
func (b *BotController) trackDamage(self RobotState) {
	if !b.hpSeen {
		b.prevHP, b.hpSeen = self.HP, true
		return
	}
	if self.HP < b.prevHP {
		b.lastDamageTick = b.ticks
	}
	b.prevHP = self.HP
}

// applyScanResult folds a fresh arena scan into the enemy memory. The engine
// attaches the report to the robot state the tick after the scan intent, so
// the pending marker and the report center must match.
func (b *BotController) applyScanResult(self RobotState) {
	if !b.scanPending || self.ScanResult == nil {
		return
	}
	report := self.ScanResult
	if report.X != b.scanX || report.Y != b.scanY {
		return
	}
	b.scanPending = false
	bestDistance := math.MaxFloat64
	for _, scanned := range report.Enemies {
		d := math.Hypot(scanned.X-self.X, scanned.Y-self.Y)
		if d < bestDistance {
			bestDistance = d
			b.lastKnown = botMemory{x: scanned.X, y: scanned.Y, tick: b.ticks}
		}
	}
}

func (b *BotController) enterState(state botState, rolls botRolls) {
	if b.state == state {
		return
	}
	b.state = state
	b.hideTicks, b.escapeTicks = 0, 0
	b.escapeMineDone = false
	b.dodgeTicks = 0
	if state == stateHide {
		b.hideBase = rolls.dodge * 360
	}
}

func (b *BotController) selectState(self RobotState, enemy RobotState, seen bool, tuning botTuning) botState {
	if seen {
		if self.HP < tuning.escapeHP || (tuning.fleeOutgunned && enemy.HP > self.HP+40) {
			return stateEscape
		}
		if tuning.hide && b.hideTicks < tuning.hideMax && b.hideTriggered(self) {
			return stateHide
		}
		return stateCombat
	}
	// A running hide episode survives lost line of sight: the bot reached
	// cover, which by construction blocks the view it was hiding from.
	if b.state == stateHide && tuning.hide && b.hideTicks > 0 && b.hideTicks < tuning.hideMax && b.hideTriggered(self) {
		return stateHide
	}
	if self.HP < tuning.escapeHP && (b.underFire() || b.memoryFresh(30)) {
		return stateEscape
	}
	if self.HP < 70 {
		if item := b.bestItem(self, true); item != nil {
			return stateItem
		}
	}
	if tuning.hunt && b.memoryFresh(memoryStaleTicks) {
		return stateHunt
	}
	if item := b.bestItem(self, false); item != nil {
		return stateItem
	}
	return statePatrol
}

func (b *BotController) hideTriggered(self RobotState) bool {
	return b.underFire() || self.Cooldown > hideCooldownGate || self.HP <= combatMinHP
}

func (b *BotController) underFire() bool {
	return b.lastDamageTick > 0 && b.ticks-b.lastDamageTick <= underFireTicks
}

func (b *BotController) memoryFresh(maxAge int) bool {
	return b.lastKnown.tick > 0 && b.ticks-b.lastKnown.tick <= maxAge
}

func (b *BotController) combatIntent(self RobotState, enemy RobotState, tuning botTuning, rolls botRolls) Intent {
	dx, dy := enemy.X-self.X, enemy.Y-self.Y
	distance := math.Hypot(dx, dy)
	bearing := normalizeDegrees(math.Atan2(dy, dx) * 180 / math.Pi)
	speed := WeaponByName(self.Weapon).ProjectileSpeed
	if speed <= 0 {
		speed = ProjectileSpeed
	}
	lead := tuning.leadFactor * distance / speed
	heading := enemy.Heading * math.Pi / 180
	targetX, targetY := enemy.X+math.Cos(heading)*MaxMovePerTick*lead, enemy.Y+math.Sin(heading)*MaxMovePerTick*lead
	desired := normalizeDegrees(math.Atan2(targetY-self.Y, targetX-self.X) * 180 / math.Pi)

	// Committed or freshly triggered dodges steer perpendicular to the
	// incoming shot. TargetX stays unset so the engine honors the dodge turn
	// instead of re-aiming at the enemy mid-sidestep.
	if b.dodgeTicks > 0 {
		b.dodgeTicks--
		return b.dodgeIntent(self, rolls)
	}
	if tuning.dodge {
		if angle, threatened := b.projectileThreat(self); threatened {
			b.dodgeTicks = b.dodgeLength(rolls.dur)
			b.dodgeAngle = angle
			b.dodgeSide = 1
			if rolls.dodge < .5 {
				b.dodgeSide = -1
			}
			return b.dodgeIntent(self, rolls)
		}
	}

	move := MaxMovePerTick
	aimError := math.Abs(shortestTurn(self.Heading, desired))
	switch {
	case distance < tuning.combatRange-40:
		move = -MaxMovePerTick / 2
	case distance > tuning.combatRange+40:
	default:
		move = MaxMovePerTick / 2
		// Orbit window: while the gun cannot fire anyway, spend the window
		// sliding sideways around the preferred-range ring. The orbit side
		// re-rolls at every window start so bots stop tracing identical rings.
		if window := (b.ticks + b.strafePhase) / 16; window != b.orbitWindow || b.orbitSide == 0 {
			b.orbitWindow = window
			b.orbitSide = 1
			if rolls.wander < .5 {
				b.orbitSide = -1
			}
		}
		if aimError >= 8 && (b.ticks+b.strafePhase)%16 < 8 {
			orbit := normalizeDegrees(bearing + 90*b.orbitSide)
			return b.finish(Intent{Move: MaxMovePerTick * .7, Turn: b.jitter(shortestTurn(self.Heading, orbit))}, rolls.move)
		}
	}
	if b.personality == PersonalityCamper && distance < 500 {
		move = 0
	}
	// A fresh sighting opens a short persona reaction before normal combat
	// steering resumes: aggressive bots charge, evasive bots re-establish
	// range, campers plant and aim.
	if b.spottedTicks > 0 {
		b.spottedTicks--
		return b.finish(b.spottedIntent(self, enemy, targetX, targetY, desired, tuning), rolls.move)
	}
	fire := aimError < 8 && LineOfSight(b.obstacles, self.X, self.Y, enemy.X, enemy.Y)
	return b.finish(Intent{Move: move, Turn: b.jitter(shortestTurn(self.Heading, desired)), Fire: fire, TargetX: &targetX, TargetY: &targetY}, rolls.move)
}

// spottedIntent is the first-sight reaction of each persona. It only steers
// for a handful of ticks, then regular combat intent takes over.
func (b *BotController) spottedIntent(self RobotState, enemy RobotState, targetX, targetY, desired float64, tuning botTuning) Intent {
	aimError := math.Abs(shortestTurn(self.Heading, desired))
	switch b.personality {
	case PersonalityEvasive:
		// Re-establish range before trading shots: run directly away while
		// close, otherwise slide sideways around the enemy.
		dx, dy := enemy.X-self.X, enemy.Y-self.Y
		bearing := normalizeDegrees(math.Atan2(dy, dx) * 180 / math.Pi)
		heading := bearing + 90*b.spottedSide
		if math.Hypot(dx, dy) < tuning.combatRange+60 {
			heading = bearing + 180
		}
		return Intent{Move: MaxMovePerTick * .9, Turn: b.jitter(shortestTurn(self.Heading, normalizeDegrees(heading)))}
	case PersonalityCamper:
		// Plant, track, and only shoot once the barrel settles.
		return Intent{Move: 0, Turn: b.jitter(shortestTurn(self.Heading, desired)), Fire: aimError < 8, TargetX: &targetX, TargetY: &targetY}
	default:
		// Aggressive: close the gap immediately, dashing across open ground.
		intent := Intent{Move: MaxMovePerTick, Turn: b.jitter(shortestTurn(self.Heading, desired)), Fire: aimError < 8, TargetX: &targetX, TargetY: &targetY}
		if self.DashCharges > 0 && math.Hypot(enemy.X-self.X, enemy.Y-self.Y) > 260 {
			intent.Dash = true
		}
		return intent
	}
}

func (b *BotController) dodgeIntent(self RobotState, rolls botRolls) Intent {
	perp := normalizeDegrees(b.dodgeAngle + 90*b.dodgeSide)
	return b.finish(Intent{Move: MaxMovePerTick * .9, Turn: b.jitter(shortestTurn(self.Heading, perp))}, rolls.move)
}

// dodgeLength commits a sidestep for 10-15 ticks (12-20 for evasive bots).
func (b *BotController) dodgeLength(roll float64) int {
	if b.personality == PersonalityEvasive {
		return 12 + int(roll*9)
	}
	return 10 + int(roll*6)
}

// projectileThreat reports the travel angle of the first hostile projectile
// whose path over the next few ticks passes within dodgeThreatRadius of the
// bot. WorldState.Projectiles is already vision-filtered engine-side.
func (b *BotController) projectileThreat(self RobotState) (float64, bool) {
	if !b.hasWorld {
		return 0, false
	}
	for _, p := range b.world.Projectiles {
		if p.OwnerID == self.RobotID || p.Team == self.Team {
			continue
		}
		if (self.X-p.X)*p.VX+(self.Y-p.Y)*p.VY <= 0 {
			continue // moving away from us
		}
		endX, endY := p.X+p.VX*dodgeLookahead, p.Y+p.VY*dodgeLookahead
		if distancePointSegment(self.X, self.Y, p.X, p.Y, endX, endY) < dodgeThreatRadius {
			return normalizeDegrees(math.Atan2(p.VY, p.VX) * 180 / math.Pi), true
		}
	}
	return 0, false
}

func (b *BotController) hideIntent(self RobotState, enemy RobotState, seen bool, tuning botTuning, rolls botRolls) Intent {
	b.hideTicks++
	cx, cy, ok := b.findCover(self)
	if !ok {
		// Nothing nearby shields the last known enemy position: fall back to
		// a run for the rest of this episode.
		b.state = stateEscape
		b.escapeTicks, b.escapeMineDone = 0, false
		return b.escapeIntent(self, RobotState{}, false, tuning, rolls)
	}
	distance := math.Hypot(cx-self.X, cy-self.Y)
	desired := normalizeDegrees(math.Atan2(cy-self.Y, cx-self.X) * 180 / math.Pi)
	move := MaxMovePerTick * .9
	if distance < 15 {
		move = 0 // holding in cover until the gun is ready
	}
	fire := false
	if seen {
		enemyBearing := normalizeDegrees(math.Atan2(enemy.Y-self.Y, enemy.X-self.X) * 180 / math.Pi)
		fire = self.Cooldown == 0 && math.Abs(shortestTurn(self.Heading, enemyBearing)) < 8 && LineOfSight(b.obstacles, self.X, self.Y, enemy.X, enemy.Y)
	}
	return b.finish(Intent{Move: move, Turn: b.jitter(shortestTurn(self.Heading, desired)), Fire: fire}, rolls.move)
}

// findCover samples ten points on rings 60-120 units out, seeded by the hide
// episode's base angle, and returns the first spot an obstacle shields from
// the last known enemy position.
func (b *BotController) findCover(self RobotState) (float64, float64, bool) {
	if !b.memoryFresh(60) {
		return 0, 0, false
	}
	for i := 0; i < coverSampleCount; i++ {
		angle := (b.hideBase + float64(i)*360/coverSampleCount) * math.Pi / 180
		radius := 60 + float64(i%3)*30
		cx := clamp(self.X+math.Cos(angle)*radius, RobotRadius+2, b.width-RobotRadius-2)
		cy := clamp(self.Y+math.Sin(angle)*radius, RobotRadius+2, b.height-RobotRadius-2)
		if collidesRobot(b.obstacles, cx, cy) {
			continue
		}
		if segmentBlocked(b.obstacles, cx, cy, b.lastKnown.x, b.lastKnown.y) {
			return cx, cy, true
		}
	}
	return 0, 0, false
}

func (b *BotController) escapeIntent(self RobotState, enemy RobotState, seen bool, tuning botTuning, rolls botRolls) Intent {
	b.escapeTicks++
	flee := self.Heading + 180 // no remembered threat: run straight back
	if seen {
		flee = normalizeDegrees(math.Atan2(self.Y-enemy.Y, self.X-enemy.X) * 180 / math.Pi)
	} else if b.memoryFresh(60) {
		flee = normalizeDegrees(math.Atan2(self.Y-b.lastKnown.y, self.X-b.lastKnown.x) * 180 / math.Pi)
	}
	// Prefer a flight direction with nearby cover that stays inside an active
	// zone; plain "away" wins ties by candidate order.
	type flight struct {
		bearing float64
		score   float64
	}
	best := flight{bearing: flee, score: -1}
	for _, offset := range []float64{0, -35, 35, -70, 70} {
		bearing := normalizeDegrees(flee + offset)
		rad := bearing * math.Pi / 180
		px, py := self.X+math.Cos(rad)*70, self.Y+math.Sin(rad)*70
		score := -1.0
		if !collidesRobot(b.obstacles, px, py) {
			score = 0
			if segmentBlocked(b.obstacles, px, py, px+math.Cos(rad)*40, py+math.Sin(rad)*40) {
				score++
			}
			if b.insideZone(px, py) {
				score += 2
			}
		}
		if score > best.score {
			best = flight{bearing: bearing, score: score}
		}
	}
	if best.score < 0 {
		// Every flight bearing runs into cover: pick a seeded slide along
		// the wall line instead of charging the blocked bearing head-on.
		best.bearing = normalizeDegrees(flee + rolls.wander*140 - 70)
	}
	intent := Intent{Move: MaxMovePerTick, Turn: b.jitter(shortestTurn(self.Heading, best.bearing))}
	if tuning.dash && self.DashCharges > 0 {
		intent.Dash = true
	}
	if tuning.mine && self.MineCharges > 0 && !b.escapeMineDone && rolls.escape < mineFleeOdds {
		b.escapeMineDone = true
		intent.Deploy = "mine"
	}
	return b.finish(intent, rolls.move)
}

// insideZone reports whether a point stays comfortably inside an active
// collapse zone; without zone data every point qualifies.
func (b *BotController) insideZone(x, y float64) bool {
	zone := b.world.Zone
	if !b.hasWorld || zone == nil || !zone.Active {
		return true
	}
	return math.Hypot(x-zone.X, y-zone.Y) <= zone.Radius*.95
}

func (b *BotController) huntIntent(self RobotState, tuning botTuning, rolls botRolls) Intent {
	desired := normalizeDegrees(math.Atan2(b.lastKnown.y-self.Y, b.lastKnown.x-self.X) * 180 / math.Pi)
	// Close to the memory the bot sweeps a seeded fan instead of beelining,
	// so searching the spot does not degenerate into identical approach loops.
	if math.Hypot(b.lastKnown.x-self.X, b.lastKnown.y-self.Y) < 80 {
		desired = normalizeDegrees(desired + rolls.wander*90 - 45)
	}
	intent := Intent{Move: MaxMovePerTick * .85, Turn: b.jitter(shortestTurn(self.Heading, desired))}
	// Sweep the quadrant around the last known position periodically; the
	// report lands on self.ScanResult the next tick (applyScanResult).
	if tuning.scan && b.ticks >= b.nextScanAt {
		b.nextScanAt = b.ticks + huntScanInterval
		b.scanX, b.scanY, b.scanPending = b.lastKnown.x, b.lastKnown.y, true
		intent.Scan = &ScanRequest{X: b.scanX, Y: b.scanY, Radius: 300}
	}
	if math.Hypot(b.lastKnown.x-self.X, b.lastKnown.y-self.Y) < 25 {
		// Searched the spot, found nothing: drop the memory so the next tick
		// falls through to items or patrol instead of circling the spot.
		b.lastKnown.tick = 0
	}
	return b.finish(intent, rolls.move)
}

func (b *BotController) itemIntent(self RobotState, rolls botRolls) Intent {
	item := b.bestItem(self, self.HP < 70)
	if item == nil {
		// The wanted pickup vanished mid-run; wander for this tick and let
		// the next selection pass pick a real state.
		return b.patrolIntent(self, rolls)
	}
	desired := normalizeDegrees(math.Atan2(item.Y-self.Y, item.X-self.X) * 180 / math.Pi)
	return b.finish(Intent{Move: MaxMovePerTick * .9, Turn: b.jitter(shortestTurn(self.Heading, desired))}, rolls.move)
}

// bestItem picks the most valuable pickup in the vision-filtered world.
// healing restricts the search to restorative items; otherwise upgrades rank
// scope > scanner > weapon > shield/armor > utility. Ties fall back to
// distance and then the engine's ItemID iteration order.
func (b *BotController) bestItem(self RobotState, healing bool) *Item {
	if !b.hasWorld {
		return nil
	}
	var best *Item
	bestScore, bestDistance := 0.0, math.MaxFloat64
	for i := range b.world.Items {
		item := &b.world.Items[i]
		score := itemScore(self, item.Type)
		if healing {
			switch item.Type {
			case "medkit", "nano_repair", "heal", "repair-core":
			default:
				score = 0
			}
		} else {
			switch item.Type {
			case "medkit", "nano_repair", "heal", "repair-core":
				score = 0 // restorative items only matter when hurt
			}
		}
		if score <= 0 {
			continue
		}
		d := math.Hypot(item.X-self.X, item.Y-self.Y)
		if score > bestScore || (score == bestScore && d < bestDistance) {
			best, bestScore, bestDistance = item, score, d
		}
	}
	return best
}

// itemScore ranks a pickup for a bot that is not healing. Zero means "not
// worth a detour"; weapons only score when they outrank the carried gun.
func itemScore(self RobotState, kind string) float64 {
	switch kind {
	// Restorative pickups: the healing/upgrade filters in bestItem decide
	// when these are reachable, the scores here only order them.
	case "medkit":
		return 100
	case "nano_repair":
		return 90
	case "heal":
		return 85
	case "repair-core":
		return 80
	case "scope":
		if effectActive(self, "optics") {
			return 0
		}
		return 70
	case "scanner":
		if effectActive(self, "radar") {
			return 0
		}
		return 55
	case "shield":
		if self.Shield > 0 {
			return 0
		}
		return 45
	case "armor_plate":
		if effectActive(self, "armor") {
			return 0
		}
		return 40
	case "dash_cell":
		if self.DashCharges >= 2 {
			return 0
		}
		return 35
	case "frenzy", "berserker_charm":
		return 30
	case "vampiric_fang":
		return 28
	case "cloak":
		return 26
	case "battery":
		return 25
	case "overdrive":
		return 22
	case "rapid_fire":
		return 20
	case "teleport_beacon":
		return 8
	}
	if rank := weaponRank(kind); rank > 0 {
		if upgrade := (rank - weaponRank(self.Weapon)) * 22; upgrade > 0 {
			return upgrade
		}
	}
	return 0
}

// weaponRank orders guns for upgrade decisions; mine_layer ranks below plasma
// because bots have no deploy loop to feed, and unknown kinds rank at zero.
func weaponRank(name string) float64 {
	if len(name) > 7 && name[:7] == "weapon_" {
		name = name[7:]
	}
	switch name {
	case "railgun":
		return 3
	case "grenade":
		return 2.5
	case "cannon":
		return 2
	case "incendiary", "cryo", "emp":
		return 1.5
	case "shotgun", "machine_gun":
		return 1
	}
	return 0
}

func (b *BotController) patrolIntent(self RobotState, rolls botRolls) Intent {
	anchor := b.anchors[b.patrolIndex]
	if b.ticks >= b.patrolUntil || math.Hypot(anchor[0]-self.X, anchor[1]-self.Y) < 50 {
		// About a third of the legs skip to a random anchor instead of the
		// next one, and every leg aims at a seeded offset beside the anchor
		// so repeat visits stop tracing the same approach vector.
		if rolls.wander < .35 {
			b.patrolIndex = int(rolls.dodge*float64(len(b.anchors))) % len(b.anchors)
		} else {
			b.patrolIndex = (b.patrolIndex + 1) % len(b.anchors)
		}
		b.patrolOffsetX = rolls.dodge*70 - 35
		b.patrolOffsetY = rolls.move*70 - 35
		b.patrolUntil = b.ticks + 150
		anchor = b.anchors[b.patrolIndex]
	}
	desired := normalizeDegrees(math.Atan2(anchor[1]+b.patrolOffsetY-self.Y, anchor[0]+b.patrolOffsetX-self.X) * 180 / math.Pi)
	return b.finish(Intent{Move: MaxMovePerTick * .55, Turn: b.jitter(shortestTurn(self.Heading, desired))}, rolls.move)
}

// patrolAnchors derives four waypoints from the arena size and the robot ID:
// one per quadrant, snapped beside the nearest obstacle when one is close
// enough to serve as cover. Campers pull their loop toward the arena center.
func patrolAnchors(obstacles []Obstacle, width, height float64, id string, camper bool) [4][2]float64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	_, _ = h.Write([]byte("|patrol"))
	sum := h.Sum64()
	quadrants := [4][2]float64{{.25, .25}, {.75, .25}, {.25, .75}, {.75, .75}}
	var anchors [4][2]float64
	for i, q := range quadrants {
		x, y := width*q[0], height*q[1]
		if obstacle, ok := nearestObstacle(obstacles, x, y, 220); ok {
			extent := obstacleExtent(obstacle)
			dx, dy := x-obstacle.X, y-obstacle.Y
			if d := math.Hypot(dx, dy); d > 0 {
				x = obstacle.X + dx/d*(extent+45)
				y = obstacle.Y + dy/d*(extent+45)
			}
		}
		// Hash-scaled nudge keeps identical maps from producing identical
		// loops for every bot without touching the RNG stream.
		x += float64(sum>>(uint(i)*8)%40) - 20
		y += float64(sum>>(uint(i)*8+3)%40) - 20
		for n := 0; n < 12 && collidesRobot(obstacles, x, y); n++ {
			x += (width/2 - x) * .1
			y += (height/2 - y) * .1
		}
		if camper {
			x += (width/2 - x) * .55
			y += (height/2 - y) * .55
		}
		anchors[i] = [2]float64{clamp(x, RobotRadius+10, width-RobotRadius-10), clamp(y, RobotRadius+10, height-RobotRadius-10)}
	}
	return anchors
}

func nearestObstacle(obstacles []Obstacle, x, y, maxRange float64) (Obstacle, bool) {
	best, bestDistance := Obstacle{}, maxRange
	for _, o := range obstacles {
		d := math.Hypot(o.X-x, o.Y-y)
		if d < bestDistance {
			best, bestDistance = o, d
		}
	}
	return best, bestDistance < maxRange
}

func obstacleExtent(o Obstacle) float64 {
	if o.Shape == "circle" {
		return o.Radius
	}
	return math.Max(o.Width, o.Height) / 2
}

// finish clamps the movement envelope and applies the seeded speed noise so
// deterministic steering still varies pace without branching the RNG stream.
func (b *BotController) finish(in Intent, moveRoll float64) Intent {
	in.Move = clamp(in.Move*(.9+moveRoll*.2), -MaxMovePerTick/2, MaxMovePerTick)
	in.Turn = clamp(in.Turn, -MaxTurnPerTick, MaxTurnPerTick)
	return in
}

// avoidObstacles keeps the legacy stuck probe: when the point ahead of the
// heading sits inside an obstacle, back off with a seeded reverse heading
// instead of grinding into the wall.
func (b *BotController) avoidObstacles(self RobotState, intent Intent, rolls botRolls) Intent {
	probeX := self.X + math.Cos(self.Heading*math.Pi/180)*RobotRadius*2
	probeY := self.Y + math.Sin(self.Heading*math.Pi/180)*RobotRadius*2
	if !collidesRobot(b.obstacles, probeX, probeY) {
		b.stuckTicks = 0
		return intent
	}
	if b.stuckTicks >= botEscapeTicks {
		b.stuckTicks = 0
	}
	if b.stuckTicks == 0 {
		sign := -1.0
		if rolls.sign < .5 {
			sign = 1
		}
		b.escapeTurn = (12 + rolls.escape*12) * sign
	}
	b.stuckTicks++
	return Intent{Move: -MaxMovePerTick / 2, Turn: b.escapeTurn}
}

func nearestVisibleEnemy(self RobotState, robots []RobotState, obstacles []Obstacle) (RobotState, bool) {
	var best RobotState
	distance := math.MaxFloat64
	for _, r := range robots {
		// Bots carry no radar, so cloaked enemies are invisible to them.
		if !r.Alive || r.Team == self.Team || isCloaked(r) {
			continue
		}
		d := math.Hypot(r.X-self.X, r.Y-self.Y)
		if d < distance && LineOfSight(obstacles, self.X, self.Y, r.X, r.Y) {
			best, distance = r, d
		}
	}
	return best, distance < math.MaxFloat64
}
