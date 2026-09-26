package model

import (
	"encoding/json"
	"time"
)

type MatchStatus string

const (
	MatchLobby    MatchStatus = "lobby"
	MatchQueued   MatchStatus = "queued"
	MatchRunning  MatchStatus = "running"
	MatchFinished MatchStatus = "finished"
	MatchFailed   MatchStatus = "failed"
)

type RobotSubmission struct {
	Loadout         json.RawMessage `json:"loadout,omitempty"`
	SDKVersion      string          `json:"sdkVersion,omitempty"`
	RobotID         string          `json:"robotId"`
	OwnerBoxID      string          `json:"ownerBoxId,omitempty"`
	DisplayName     string          `json:"displayName"`
	Team            string          `json:"team"`
	ScriptObjectKey string          `json:"scriptObjectKey,omitempty"`
	StartCommand    string          `json:"startCommand"`
	Runtime         string          `json:"runtime"`
	SubmittedAt     time.Time       `json:"submittedAt"`
	PlayerID        string          `json:"-"`
	Bot             bool            `json:"bot,omitempty"`
}

type BoxLimits struct {
	CPUs         float64 `json:"cpus"`
	MemoryMB     int64   `json:"memoryMb"`
	StorageBytes int64   `json:"storageBytes"`
	PIDs         int64   `json:"pids"`
}

type BoxRecord struct {
	BoxID          string    `json:"boxId"`
	UserID         string    `json:"-"`
	Status         string    `json:"status"`
	SSHHost        string    `json:"sshHost"`
	SSHPort        int       `json:"sshPort"`
	SSHUser        string    `json:"sshUser"`
	KeyFingerprint string    `json:"keyFingerprint,omitempty"`
	UsageBytes     int64     `json:"usageBytes"`
	AgentStatus    string    `json:"agentStatus"`
	ActiveRobotID  string    `json:"activeRobotId,omitempty"`
	ActiveMatchID  string    `json:"activeMatchId,omitempty"`
	Error          string    `json:"error,omitempty"`
	Limits         BoxLimits `json:"limits"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type RobotSummary struct {
	RobotID       string   `json:"robotId"`
	Name          string   `json:"name"`
	Team          string   `json:"team"`
	HP            int      `json:"hp"`
	Alive         bool     `json:"alive"`
	Failed        bool     `json:"failed"`
	AvgResponseMS float64  `json:"avgResponseMs"`
	Equipment     []string `json:"equipment,omitempty"`
	DamageDealt   int      `json:"damageDealt"`
	DamageTaken   int      `json:"damageTaken"`
	Kills         int      `json:"kills"`
	ItemsPickedUp int      `json:"itemsPickedUp"`
}

type MatchEvent struct {
	Sequence int    `json:"sequence"`
	Tick     int    `json:"tick"`
	Type     string `json:"type"`
	RobotID  string `json:"robotId,omitempty"`
	TargetID string `json:"targetId,omitempty"`
	Damage   int    `json:"damage,omitempty"`
	Message  string `json:"message,omitempty"`
}

type PlayerStats struct {
	PlayerID    string         `json:"playerId"`
	Handle      string         `json:"handle"`
	Matches     int            `json:"matches"`
	Wins        int            `json:"wins"`
	Losses      int            `json:"losses"`
	Draws       int            `json:"draws"`
	DamageDealt int            `json:"damageDealt"`
	DamageTaken int            `json:"damageTaken"`
	Ratings     map[string]int `json:"ratings"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type ScriptVersion struct {
	VersionID string    `json:"versionId"`
	ObjectKey string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
}

type Match struct {
	EngineVersion int             `json:"engineVersion,omitempty"`
	ArenaConfig   json.RawMessage `json:"arenaConfig,omitempty"`
	MatchID       string          `json:"matchId"`
	OwnerID       string          `json:"ownerId,omitempty"`
	Status        MatchStatus     `json:"status"`
	Mode          string          `json:"mode"`
	MapID         string          `json:"mapId"`
	ArenaWidth    float64         `json:"arenaWidth"`
	ArenaHeight   float64         `json:"arenaHeight"`
	Practice      bool            `json:"practice,omitempty"`
	Seed          int64           `json:"seed"`
	TickRate      int             `json:"tickRate"`
	// Combat options mirror the engine.Config field types; zero values keep
	// stock behavior (friendly fire off, no regen, no ramming damage).
	FriendlyFire    bool `json:"friendlyFire,omitempty"`
	RegenPerTick    int  `json:"regenPerTick,omitempty"`
	RegenDelayTicks int  `json:"regenDelayTicks,omitempty"`
	RammingDamage   bool `json:"rammingDamage,omitempty"`
	// BotPersonality selects server-bot behavior: aggressive, evasive, camper,
	// or mixed (mixed derives a stable persona per bot from its robot ID).
	BotPersonality  string            `json:"botPersonality,omitempty"`
	Robots          []RobotSubmission `json:"robots"`
	WinnerTeam      string            `json:"winnerTeam,omitempty"`
	RobotSummaries  []RobotSummary    `json:"robotSummaries,omitempty"`
	EventSummary    []MatchEvent      `json:"eventSummary,omitempty"`
	ReplayObjectKey string            `json:"replayObjectKey,omitempty"`
	Error           string            `json:"error,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	StartedAt       *time.Time        `json:"startedAt,omitempty"`
	FinishedAt      *time.Time        `json:"finishedAt,omitempty"`
}
