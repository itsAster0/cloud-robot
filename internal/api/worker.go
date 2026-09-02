package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/kryxen/cloud-robot/internal/engine"
	"github.com/kryxen/cloud-robot/internal/model"
)

// RecoverMatches reconciles stored matches after a server restart. A match
// that was running lost its in-memory simulation and can only be failed
// honestly; queued matches may have lost their job when emulator state was
// reset, so they are enqueued again. Both paths release boxes so no restart
// leaves a player stuck with an "already in a match" marker.
func (s *Server) RecoverMatches(ctx context.Context) {
	running, err := s.store.ListMatches(ctx, model.MatchRunning, 100)
	if err != nil {
		slog.Error("recover running matches", "error", err)
	}
	for _, match := range running {
		if err := s.failMatch(ctx, match, errors.New("server restarted before the match finished")); err != nil {
			slog.Error("fail match after restart", "matchId", match.MatchID, "error", err)
		}
	}
	queued, err := s.store.ListMatches(ctx, model.MatchQueued, 100)
	if err != nil {
		slog.Error("recover queued matches", "error", err)
	}
	for _, match := range queued {
		if err := s.store.EnqueueMatch(ctx, match.MatchID); err != nil {
			slog.Error("requeue match after restart", "matchId", match.MatchID, "error", err)
		}
	}
}

func (s *Server) RunWorker(ctx context.Context) {
	for ctx.Err() == nil {
		job, ok, err := s.store.ReceiveJob(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("receive match job", "error", err)
			time.Sleep(time.Second)
			continue
		}
		if !ok {
			continue
		}
		if err := s.runMatch(ctx, job.MatchID); err != nil {
			slog.Error("run match", "matchId", job.MatchID, "error", err)
		}
		if err := s.store.DeleteJob(ctx, job.ReceiptHandle); err != nil {
			slog.Error("delete match job", "error", err)
		}
	}
}

func (s *Server) runMatch(ctx context.Context, matchID string) error {
	match, err := s.store.GetMatch(ctx, matchID)
	if err != nil {
		return err
	}
	if match.Status != model.MatchQueued {
		return nil
	}
	now := time.Now().UTC()
	match.Status, match.StartedAt = model.MatchRunning, &now
	if err := s.store.PutMatch(ctx, match); err != nil {
		return err
	}
	// The queue hands ownership off at start. Entries cleared here stop
	// /api/queue from reporting "matched" after the match ends, which would
	// bounce the player between /play and the finished match page.
	s.clearQueueEntriesForMatch(matchID)
	s.hub.Publish(matchID, map[string]any{"type": "match_state", "version": 1, "match": match})

	robots := make([]engine.RobotState, 0, len(match.Robots))
	controllers := make(map[string]engine.Controller, len(match.Robots))
	config := engineConfigFor(match)
	personality := botPersonalityFor(match)
	// Solo matches end when the single human robot stops being alive; with
	// zero or multiple humans the id stays empty and the sandbox rules apply.
	if match.Mode == "solo" {
		humanID, ambiguous := "", false
		for _, submission := range match.Robots {
			if submission.Bot || strings.HasPrefix(submission.StartCommand, "bot:") {
				continue
			}
			if humanID != "" {
				ambiguous = true
				break
			}
			humanID = submission.RobotID
		}
		if !ambiguous {
			config.SoloRobotID = humanID
		}
	}
	for _, submission := range match.Robots {
		if submission.Bot {
			difficulty := engine.BotFighter
			switch submission.StartCommand {
			case "bot:dummy":
				difficulty = engine.BotDummy
			case "bot:rookie":
				difficulty = engine.BotRookie
			case "bot:sharpshooter":
				difficulty = engine.BotSharpshooter
			}
			controllers[submission.RobotID] = engine.NewBotController(submission.RobotID, difficulty, personality, config.Map)
		} else if !s.agents.Connected(submission.RobotID) {
			return s.failMatch(ctx, match, fmt.Errorf("robot agent %s disconnected", submission.RobotID))
		} else {
			controllers[submission.RobotID] = s.agents.Controller(submission.RobotID)
		}
		robots = append(robots, engine.RobotState{RobotID: submission.RobotID, Name: submission.DisplayName, Team: submission.Team})
	}
	robots = engine.SpawnPositionsForMap(robots, config.Map, config.Width, config.Height)
	arena := engine.NewWithConfig(matchID, robots, controllers, config)
	defer arena.Close()
	s.registerActiveArena(matchID, arena)
	defer s.unregisterActiveArena(matchID)
	events := make([]model.MatchEvent, 0, 128)

	tickRate := match.TickRate
	if tickRate <= 0 {
		tickRate = 10
	}
	ticker := time.NewTicker(time.Second / time.Duration(tickRate))
	defer ticker.Stop()
	for !arena.Finished() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			snapshot := arena.Step(ctx)
			for _, event := range snapshot.Events {
				events = append(events, model.MatchEvent{Sequence: len(events) + 1, Tick: event.Tick, Type: event.Type, RobotID: event.RobotID, TargetID: event.TargetID, Damage: event.Damage, Message: event.Message})
			}
			s.hub.Publish(matchID, snapshot)
		}
	}

	finished := time.Now().UTC()
	match.Status, match.WinnerTeam, match.FinishedAt = model.MatchFinished, arena.Winner(), &finished
	for _, robot := range arena.Robots {
		match.RobotSummaries = append(match.RobotSummaries, model.RobotSummary{
			RobotID: robot.RobotID, Name: robot.Name, Team: robot.Team, HP: robot.HP, Alive: robot.Alive, Failed: robot.Failed,
			AvgResponseMS: robot.AvgResponseMS, Equipment: robot.Equipment, DamageDealt: robot.DamageDealt, DamageTaken: robot.DamageTaken, Kills: robot.Kills, ItemsPickedUp: robot.ItemsPickedUp,
		})
	}
	match.EventSummary = tailEvents(events, 100)
	match.ReplayObjectKey = fmt.Sprintf("replays/%s/events.json", match.MatchID)
	if err := s.store.PutReplay(ctx, match.ReplayObjectKey, events); err != nil {
		slog.Error("store match replay", "matchId", match.MatchID, "error", err)
		match.ReplayObjectKey = ""
	}
	if err := s.store.PutMatch(ctx, match); err != nil {
		return err
	}
	s.releaseBoxes(ctx, match)
	if !match.Practice {
		s.updatePlayerStats(ctx, match)
	}
	s.hub.Publish(matchID, map[string]any{"type": "match_finished", "version": 1, "matchId": matchID, "winnerTeam": match.WinnerTeam, "match": match})
	s.hub.Forget(matchID)
	return nil
}

func tailEvents(events []model.MatchEvent, limit int) []model.MatchEvent {
	if len(events) <= limit {
		return append([]model.MatchEvent(nil), events...)
	}
	return append([]model.MatchEvent(nil), events[len(events)-limit:]...)
}

// engineConfigFor layers the persisted match combat options over the engine
// defaults. Zero values keep stock behavior: friendly fire off, no
// regeneration, no ramming damage.
func engineConfigFor(match model.Match) engine.Config {
	config := engine.DefaultConfig()
	config.Map = mapForMatch(match)
	if match.ArenaWidth > 0 {
		config.Width = match.ArenaWidth
	}
	if match.ArenaHeight > 0 {
		config.Height = match.ArenaHeight
	}
	config.Seed = uint64(match.Seed)
	config.FriendlyFire = match.FriendlyFire
	config.RegenPerTick = match.RegenPerTick
	config.RegenDelayTicks = match.RegenDelayTicks
	config.RammingDamage = match.RammingDamage
	return config
}

// mapForMatch resolves the persisted map id: starter maps come from the
// registry and "random-*" ids generate deterministically from the match seed,
// so every worker rebuilds an identical arena for the same match. Custom match
// dims are passed through, which keeps ScaleMap a no-op for generated maps.
func mapForMatch(match model.Match) engine.MapDefinition {
	if style, ok := engine.ProceduralMapStyles[match.MapID]; ok {
		width, height := match.ArenaWidth, match.ArenaHeight
		if width <= 0 {
			width = 900
		}
		if height <= 0 {
			height = 600
		}
		generated, err := engine.GenerateMap(uint64(match.Seed), style, width, height)
		if err == nil {
			return generated
		}
		slog.Error("generate procedural map", "mapId", match.MapID, "error", err)
	}
	if selected, ok := engine.StarterMaps()[match.MapID]; ok {
		return selected
	}
	return engine.DefaultMap(900, 600)
}

// botPersonalityFor maps the persisted match setting onto the engine.
// Mixed keeps flowing through: NewBotController resolves it per bot from the
// robot ID. Unknown or empty values fall back to aggressive.
func botPersonalityFor(match model.Match) engine.BotPersonality {
	switch match.BotPersonality {
	case "evasive":
		return engine.PersonalityEvasive
	case "camper":
		return engine.PersonalityCamper
	case "mixed":
		return engine.PersonalityMixed
	default:
		return engine.PersonalityAggressive
	}
}

func (s *Server) updatePlayerStats(ctx context.Context, match model.Match) {
	summaries := make(map[string]model.RobotSummary, len(match.RobotSummaries))
	for _, summary := range match.RobotSummaries {
		summaries[summary.RobotID] = summary
	}
	players := make(map[string]model.PlayerStats)
	baseRatings := make(map[string]int)
	for _, robot := range match.Robots {
		if robot.PlayerID == "" || robot.Bot {
			continue
		}
		stats, err := s.store.GetPlayerStats(ctx, robot.PlayerID)
		if err != nil {
			stats = model.PlayerStats{PlayerID: robot.PlayerID, Handle: robot.PlayerID, Ratings: map[string]int{}}
		}
		if stats.Ratings == nil {
			stats.Ratings = map[string]int{}
		}
		if stats.Ratings[match.Mode] == 0 {
			stats.Ratings[match.Mode] = 1500
		}
		players[robot.PlayerID] = stats
		baseRatings[robot.PlayerID] = stats.Ratings[match.Mode]
	}
	for _, robot := range match.Robots {
		stats, ok := players[robot.PlayerID]
		if !ok {
			continue
		}
		stats.Matches++
		summary := summaries[robot.RobotID]
		stats.DamageDealt += summary.DamageDealt
		stats.DamageTaken += summary.DamageTaken
		score := .0
		switch {
		case match.WinnerTeam == "draw" || match.WinnerTeam == "":
			stats.Draws++
			score = .5
		case robot.Team == match.WinnerTeam:
			stats.Wins++
			score = 1
		default:
			stats.Losses++
		}
		opponentTotal, opponentCount := 0, 0
		for _, opponent := range match.Robots {
			if opponent.Team != robot.Team {
				if opponentRating, found := baseRatings[opponent.PlayerID]; found {
					opponentTotal += opponentRating
					opponentCount++
				}
			}
		}
		if opponentCount > 0 {
			current := baseRatings[robot.PlayerID]
			expected := 1 / (1 + math.Pow(10, float64(opponentTotal/opponentCount-current)/400))
			stats.Ratings[match.Mode] = current + int(math.Round(32*(score-expected)))
		}
		if err := s.store.PutPlayerStats(ctx, stats); err != nil {
			slog.Error("update player stats", "playerId", robot.PlayerID, "error", err)
		}
	}
}

func (s *Server) failMatch(ctx context.Context, match model.Match, failure error) error {
	finished := time.Now().UTC()
	match.Status, match.Error, match.FinishedAt = model.MatchFailed, failure.Error(), &finished
	_ = s.store.PutMatch(ctx, match)
	s.releaseBoxes(ctx, match)
	s.hub.Publish(match.MatchID, map[string]any{"type": "error", "version": 1, "matchId": match.MatchID, "error": failure.Error()})
	s.hub.Forget(match.MatchID)
	s.requeueIfQueued(ctx, match)
	return fmt.Errorf("match %s failed: %w", match.MatchID, failure)
}

// releaseBoxes clears the active robot/match markers on every player box in a
// match once the match can no longer run. Without this, a finished or failed
// match leaves the marker behind and blocks queueing and registration.
func (s *Server) releaseBoxes(ctx context.Context, match model.Match) {
	for _, robot := range match.Robots {
		if robot.Bot || robot.PlayerID == "" {
			continue
		}
		box, err := s.store.GetBox(ctx, robot.PlayerID)
		if err != nil || box.ActiveMatchID != match.MatchID {
			continue
		}
		box.ActiveRobotID, box.ActiveMatchID = "", ""
		if err := s.store.PutBox(ctx, robot.PlayerID, box); err != nil {
			slog.Error("release box after match end", "matchId", match.MatchID, "error", err)
		}
	}
}
