package model

import "time"

type MatchStatus string

const (
	MatchLobby    MatchStatus = "lobby"
	MatchQueued   MatchStatus = "queued"
	MatchRunning  MatchStatus = "running"
	MatchFinished MatchStatus = "finished"
	MatchFailed   MatchStatus = "failed"
)

type RobotSubmission struct {
	RobotID         string    `json:"robotId"`
	OwnerBoxID      string    `json:"ownerBoxId,omitempty"`
	DisplayName     string    `json:"displayName"`
	Team            string    `json:"team"`
	ScriptObjectKey string    `json:"scriptObjectKey,omitempty"`
	StartCommand    string    `json:"startCommand"`
	Runtime         string    `json:"runtime"`
	SubmittedAt     time.Time `json:"submittedAt"`
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
}

type Match struct {
	MatchID        string            `json:"matchId"`
	OwnerID        string            `json:"ownerId,omitempty"`
	Status         MatchStatus       `json:"status"`
	Mode           string            `json:"mode"`
	Seed           int64             `json:"seed"`
	TickRate       int               `json:"tickRate"`
	Robots         []RobotSubmission `json:"robots"`
	WinnerTeam     string            `json:"winnerTeam,omitempty"`
	RobotSummaries []RobotSummary    `json:"robotSummaries,omitempty"`
	Error          string            `json:"error,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	StartedAt      *time.Time        `json:"startedAt,omitempty"`
	FinishedAt     *time.Time        `json:"finishedAt,omitempty"`
}
