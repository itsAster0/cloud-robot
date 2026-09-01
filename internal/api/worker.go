package api

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kryxen/cloud-robot/internal/engine"
	"github.com/kryxen/cloud-robot/internal/model"
)

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
	s.hub.Publish(matchID, map[string]any{"type": "match_state", "version": 1, "match": match})

	robots := make([]engine.RobotState, 0, len(match.Robots))
	controllers := make(map[string]engine.Controller, len(match.Robots))
	for _, submission := range match.Robots {
		if !s.agents.Connected(submission.RobotID) {
			return s.failMatch(ctx, match, fmt.Errorf("robot agent %s disconnected", submission.RobotID))
		}
		controllers[submission.RobotID] = s.agents.Controller(submission.RobotID)
		robots = append(robots, engine.RobotState{RobotID: submission.RobotID, Name: submission.DisplayName, Team: submission.Team})
	}
	robots = engine.SpawnPositions(robots)
	arena := engine.New(matchID, robots, controllers)
	defer arena.Close()

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
			s.hub.Publish(matchID, snapshot)
		}
	}

	finished := time.Now().UTC()
	match.Status, match.WinnerTeam, match.FinishedAt = model.MatchFinished, arena.Winner(), &finished
	for _, robot := range arena.Robots {
		match.RobotSummaries = append(match.RobotSummaries, model.RobotSummary{
			RobotID: robot.RobotID, Name: robot.Name, Team: robot.Team, HP: robot.HP, Alive: robot.Alive, Failed: robot.Failed,
			AvgResponseMS: robot.AvgResponseMS, Equipment: robot.Equipment,
		})
	}
	if err := s.store.PutMatch(ctx, match); err != nil {
		return err
	}
	s.hub.Publish(matchID, map[string]any{"type": "match_finished", "version": 1, "matchId": matchID, "winnerTeam": match.WinnerTeam, "match": match})
	return nil
}

func (s *Server) failMatch(ctx context.Context, match model.Match, failure error) error {
	finished := time.Now().UTC()
	match.Status, match.Error, match.FinishedAt = model.MatchFailed, failure.Error(), &finished
	_ = s.store.PutMatch(ctx, match)
	s.hub.Publish(match.MatchID, map[string]any{"type": "error", "version": 1, "matchId": match.MatchID, "error": failure.Error()})
	return fmt.Errorf("match %s failed: %w", match.MatchID, failure)
}
