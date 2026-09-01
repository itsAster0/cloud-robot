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
)

type BotController struct {
	difficulty  BotDifficulty
	personality BotPersonality
	obstacles   []Obstacle
	rng         *rand.Rand
	ticks       int
	turn        float64
	strafePhase int
	stuckTicks  int
	escapeTurn  float64
	driftBias   float64
	nextDriftAt int
}

const botEscapeTicks = 5

func NewBotController(id string, difficulty BotDifficulty, personality BotPersonality, m MapDefinition) *BotController {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	seed := h.Sum64()
	rng := rand.New(rand.NewPCG(seed, seed^0xd1b54a32d192ed03))
	bot := &BotController{
		difficulty:  difficulty,
		personality: personality,
		obstacles:   append([]Obstacle(nil), m.Obstacles...),
		rng:         rng,
		strafePhase: rng.IntN(30),
		driftBias:   rng.Float64()*4 - 2,
		nextDriftAt: 30 + rng.IntN(30),
	}
	return bot
}
func (b *BotController) Close() {}

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

func (b *BotController) Tick(_ context.Context, self RobotState, robots []RobotState) (Intent, error) {
	b.ticks++
	if b.difficulty == BotDummy {
		return Intent{}, nil
	}
	enemy, ok := nearestVisibleEnemy(self, robots, b.obstacles)
	if !ok {
		if b.ticks%20 == 1 || b.rng.Float64() < .02 {
			b.turn = b.rng.Float64()*24 - 12
		}
		return Intent{Move: MaxMovePerTick * .5, Turn: b.jitter(b.turn)}, nil
	}
	dx, dy := enemy.X-self.X, enemy.Y-self.Y
	distance := math.Hypot(dx, dy)
	targetX, targetY := enemy.X, enemy.Y
	if b.difficulty == BotSharpshooter {
		speed := WeaponByName(self.Weapon).ProjectileSpeed
		if speed <= 0 {
			speed = ProjectileSpeed
		}
		lead := distance / speed
		heading := enemy.Heading * math.Pi / 180
		targetX += math.Cos(heading) * MaxMovePerTick * lead
		targetY += math.Sin(heading) * MaxMovePerTick * lead
	}
	desired := normalizeDegrees(math.Atan2(targetY-self.Y, targetX-self.X) * 180 / math.Pi)
	turn := shortestTurn(self.Heading, desired)
	move := MaxMovePerTick
	if (b.difficulty == BotFighter || b.difficulty == BotSharpshooter) && self.HP < 30 {
		move = -MaxMovePerTick / 2
	}
	if b.personality == PersonalityCamper && distance < 500 {
		move = 0
	}
	if b.personality == PersonalityEvasive || b.difficulty == BotSharpshooter {
		if (b.ticks+b.strafePhase)%30 < 15 {
			turn += 12
		} else {
			turn -= 12
		}
		move = MaxMovePerTick * .75
	}
	if b.personality == PersonalityAggressive {
		move = MaxMovePerTick
	}
	blocked := collidesRobot(b.obstacles, self.X+math.Cos(self.Heading*math.Pi/180)*RobotRadius*2, self.Y+math.Sin(self.Heading*math.Pi/180)*RobotRadius*2)
	if blocked {
		if b.stuckTicks >= botEscapeTicks {
			b.stuckTicks = 0
		}
		if b.stuckTicks == 0 {
			// Random back-off heading each escape episode; grinding toward the
			// target never frees a wedged robot.
			b.escapeTurn = (12 + b.rng.Float64()*12) * b.escapeSign()
		}
		b.stuckTicks++
		return Intent{Move: -MaxMovePerTick / 2, Turn: b.escapeTurn}, nil
	}
	b.stuckTicks = 0
	fire := math.Abs(shortestTurn(self.Heading, desired)) < 8 && LineOfSight(b.obstacles, self.X, self.Y, enemy.X, enemy.Y)
	return Intent{Move: move * (.9 + b.rng.Float64()*.2), Turn: b.jitter(turn), Fire: fire, TargetX: &targetX, TargetY: &targetY}, nil
}

func (b *BotController) escapeSign() float64 {
	if b.rng.Float64() < .5 {
		return -1
	}
	return 1
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
