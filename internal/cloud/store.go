package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/kryxen/cloud-robot/internal/model"
)

type Config struct {
	Endpoint string
	Region   string
	Bucket   string
	Table    string
	Queue    string
}

func ConfigFromEnv() Config {
	return Config{
		Endpoint: envOr("AWS_ENDPOINT_URL", "http://localhost:4566"),
		Region:   envOr("AWS_REGION", "us-east-1"),
		Bucket:   envOr("S3_SCRIPTS_BUCKET", "robot-arena-scripts"),
		Table:    envOr("DYNAMODB_MATCHES_TABLE", "robot-arena-matches"),
		Queue:    envOr("SQS_MATCH_JOBS_QUEUE", "robot-arena-match-jobs"),
	}
}

type Store struct {
	config   Config
	s3       *s3.Client
	dynamo   *dynamodb.Client
	sqs      *sqs.Client
	queueURL string
}

func New(ctx context.Context, settings Config) (*Store, error) {
	key := envOr("AWS_ACCESS_KEY_ID", "test")
	secret := envOr("AWS_SECRET_ACCESS_KEY", "test")
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(settings.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(key, secret, "")),
	}
	if settings.Endpoint != "" {
		options = append(options, awsconfig.WithBaseEndpoint(settings.Endpoint))
	}
	config, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	return &Store{
		config: settings,
		s3: s3.NewFromConfig(config, func(options *s3.Options) {
			options.UsePathStyle = settings.Endpoint != ""
		}),
		dynamo: dynamodb.NewFromConfig(config),
		sqs:    sqs.NewFromConfig(config),
	}, nil
}

func (s *Store) Ensure(ctx context.Context) error {
	if _, err := s.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.config.Bucket)}); err != nil {
		if _, createErr := s.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.config.Bucket)}); createErr != nil {
			return fmt.Errorf("create S3 bucket %q: %w", s.config.Bucket, createErr)
		}
	}

	if _, err := s.dynamo.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(s.config.Table)}); err != nil {
		_, createErr := s.dynamo.CreateTable(ctx, &dynamodb.CreateTableInput{
			TableName:            aws.String(s.config.Table),
			BillingMode:          types.BillingModePayPerRequest,
			AttributeDefinitions: []types.AttributeDefinition{{AttributeName: aws.String("matchId"), AttributeType: types.ScalarAttributeTypeS}},
			KeySchema:            []types.KeySchemaElement{{AttributeName: aws.String("matchId"), KeyType: types.KeyTypeHash}},
		})
		if createErr != nil {
			return fmt.Errorf("create DynamoDB table %q: %w", s.config.Table, createErr)
		}
	}

	queue, err := s.sqs.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(s.config.Queue)})
	if err != nil {
		return fmt.Errorf("create SQS queue %q: %w", s.config.Queue, err)
	}
	s.queueURL = aws.ToString(queue.QueueUrl)
	if s.queueURL == "" {
		return errors.New("SQS returned empty queue URL")
	}
	return nil
}

func (s *Store) Ready(ctx context.Context) error {
	if s.queueURL == "" {
		return errors.New("cloud resources are not initialized")
	}
	if _, err := s.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.config.Bucket)}); err != nil {
		return fmt.Errorf("S3 bucket unavailable: %w", err)
	}
	if _, err := s.dynamo.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(s.config.Table)}); err != nil {
		return fmt.Errorf("DynamoDB table unavailable: %w", err)
	}
	return nil
}

func (s *Store) PutScript(ctx context.Context, key, source string) error {
	_, err := s.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.config.Bucket), Key: aws.String(key), Body: bytesReader(source),
		ContentType: aws.String("text/x-lua"),
	})
	if err != nil {
		return fmt.Errorf("store robot script: %w", err)
	}
	return nil
}

func (s *Store) GetScript(ctx context.Context, key string) (string, error) {
	result, err := s.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(key)})
	if err != nil {
		return "", fmt.Errorf("read robot script: %w", err)
	}
	defer result.Body.Close()
	data, err := readAllLimited(result.Body, 17*1024)
	if err != nil {
		return "", fmt.Errorf("read robot script body: %w", err)
	}
	return string(data), nil
}

// Replay objects have a separate bound from executable Lua source.
const maxReplayBytes = 16 * 1024 * 1024

func (s *Store) PutReplayObject(ctx context.Context, key, source string) error {
	if len(source) > maxReplayBytes {
		return fmt.Errorf("replay exceeds %d bytes", maxReplayBytes)
	}
	_, err := s.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(key), Body: bytesReader(source), ContentType: aws.String("application/octet-stream")})
	if err != nil {
		return fmt.Errorf("store replay: %w", err)
	}
	return nil
}

func (s *Store) GetReplayObject(ctx context.Context, key string) (string, error) {
	result, err := s.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(key)})
	if err != nil {
		return "", fmt.Errorf("read replay: %w", err)
	}
	defer result.Body.Close()
	data, err := readAllLimited(result.Body, maxReplayBytes)
	if err != nil {
		return "", fmt.Errorf("read replay body: %w", err)
	}
	return string(data), nil
}

func (s *Store) ListScriptVersions(ctx context.Context, boxID string, limit int) ([]model.ScriptVersion, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	prefix := "versions/" + boxID + "/"
	result, err := s.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(s.config.Bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(int32(limit))})
	if err != nil {
		return nil, fmt.Errorf("list script versions: %w", err)
	}
	versions := make([]model.ScriptVersion, 0, len(result.Contents))
	for _, object := range result.Contents {
		key := aws.ToString(object.Key)
		parts := strings.Split(strings.TrimPrefix(key, prefix), "/")
		if len(parts) != 2 || parts[1] != "main.lua" {
			continue
		}
		created := time.Time{}
		if object.LastModified != nil {
			created = object.LastModified.UTC()
		}
		versions = append(versions, model.ScriptVersion{VersionID: parts[0], ObjectKey: key, CreatedAt: created})
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].CreatedAt.After(versions[j].CreatedAt) })
	return versions, nil
}

func (s *Store) PutMatch(ctx context.Context, match model.Match) error {
	payload, err := encodeStoredMatch(match)
	if err != nil {
		return fmt.Errorf("encode match: %w", err)
	}
	_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.config.Table),
		Item: map[string]types.AttributeValue{
			"matchId":   &types.AttributeValueMemberS{Value: match.MatchID},
			"status":    &types.AttributeValueMemberS{Value: string(match.Status)},
			"payload":   &types.AttributeValueMemberS{Value: string(payload)},
			"updatedAt": &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339Nano)},
		},
	})
	if err != nil {
		return fmt.Errorf("store match: %w", err)
	}
	return nil
}

func (s *Store) GetMatch(ctx context.Context, matchID string) (model.Match, error) {
	result, err := s.dynamo.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.config.Table),
		Key:            map[string]types.AttributeValue{"matchId": &types.AttributeValueMemberS{Value: matchID}},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return model.Match{}, fmt.Errorf("read match: %w", err)
	}
	value, ok := result.Item["payload"].(*types.AttributeValueMemberS)
	if !ok {
		return model.Match{}, errors.New("match not found")
	}
	match, err := decodeStoredMatch([]byte(value.Value))
	if err != nil {
		return model.Match{}, fmt.Errorf("decode match: %w", err)
	}
	return match, nil
}

func (s *Store) ListMatches(ctx context.Context, status model.MatchStatus, limit int) ([]model.Match, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	result, err := s.dynamo.Scan(ctx, &dynamodb.ScanInput{
		TableName:            aws.String(s.config.Table),
		ProjectionExpression: aws.String("matchId, payload"),
	})
	if err != nil {
		return nil, fmt.Errorf("list matches: %w", err)
	}
	matches := make([]model.Match, 0, min(limit, len(result.Items)))
	for _, item := range result.Items {
		id, ok := item["matchId"].(*types.AttributeValueMemberS)
		if !ok || strings.Contains(id.Value, "#") {
			continue
		}
		value, ok := item["payload"].(*types.AttributeValueMemberS)
		if !ok {
			continue
		}
		match, decodeErr := decodeStoredMatch([]byte(value.Value))
		if decodeErr != nil || status != "" && match.Status != status {
			continue
		}
		matches = append(matches, match)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].CreatedAt.After(matches[j].CreatedAt) })
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

type storedMatch struct {
	model.Match
	PlayerIDs map[string]string `json:"_playerIds,omitempty"`
}

func encodeStoredMatch(match model.Match) ([]byte, error) {
	players := make(map[string]string)
	for _, robot := range match.Robots {
		if robot.PlayerID != "" {
			players[robot.RobotID] = robot.PlayerID
		}
	}
	return json.Marshal(storedMatch{Match: match, PlayerIDs: players})
}

func decodeStoredMatch(payload []byte) (model.Match, error) {
	var stored storedMatch
	if err := json.Unmarshal(payload, &stored); err != nil {
		return model.Match{}, err
	}
	for index := range stored.Robots {
		stored.Robots[index].PlayerID = stored.PlayerIDs[stored.Robots[index].RobotID]
	}
	return stored.Match, nil
}

func (s *Store) PutPlayerStats(ctx context.Context, stats model.PlayerStats) error {
	stats.UpdatedAt = time.Now().UTC()
	payload, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("encode player stats: %w", err)
	}
	_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(s.config.Table), Item: map[string]types.AttributeValue{
		"matchId":   &types.AttributeValueMemberS{Value: "player#" + stats.PlayerID},
		"status":    &types.AttributeValueMemberS{Value: "player"},
		"payload":   &types.AttributeValueMemberS{Value: string(payload)},
		"updatedAt": &types.AttributeValueMemberS{Value: stats.UpdatedAt.Format(time.RFC3339Nano)},
	}})
	if err != nil {
		return fmt.Errorf("store player stats: %w", err)
	}
	return nil
}

func (s *Store) GetPlayerStats(ctx context.Context, playerID string) (model.PlayerStats, error) {
	result, err := s.dynamo.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.config.Table), Key: map[string]types.AttributeValue{
		"matchId": &types.AttributeValueMemberS{Value: "player#" + playerID},
	}, ConsistentRead: aws.Bool(true)})
	if err != nil {
		return model.PlayerStats{}, fmt.Errorf("read player stats: %w", err)
	}
	value, ok := result.Item["payload"].(*types.AttributeValueMemberS)
	if !ok {
		return model.PlayerStats{}, errors.New("player not found")
	}
	var stats model.PlayerStats
	if err := json.Unmarshal([]byte(value.Value), &stats); err != nil {
		return model.PlayerStats{}, fmt.Errorf("decode player stats: %w", err)
	}
	return stats, nil
}

func (s *Store) ListPlayerStats(ctx context.Context, limit int) ([]model.PlayerStats, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	result, err := s.dynamo.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(s.config.Table), ProjectionExpression: aws.String("matchId, payload")})
	if err != nil {
		return nil, fmt.Errorf("list player stats: %w", err)
	}
	players := make([]model.PlayerStats, 0, limit)
	for _, item := range result.Items {
		id, ok := item["matchId"].(*types.AttributeValueMemberS)
		if !ok || !strings.HasPrefix(id.Value, "player#") {
			continue
		}
		value, ok := item["payload"].(*types.AttributeValueMemberS)
		if !ok {
			continue
		}
		var stats model.PlayerStats
		if json.Unmarshal([]byte(value.Value), &stats) == nil {
			players = append(players, stats)
		}
	}
	sort.Slice(players, func(i, j int) bool {
		left, right := players[i].Ratings["duel"], players[j].Ratings["duel"]
		if left == right {
			return players[i].Wins > players[j].Wins
		}
		return left > right
	})
	if len(players) > limit {
		players = players[:limit]
	}
	return players, nil
}

func (s *Store) PutReplay(ctx context.Context, key string, events []model.MatchEvent) error {
	payload, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("encode replay: %w", err)
	}
	_, err = s.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(key), Body: bytes.NewReader(payload), ContentType: aws.String("application/json")})
	if err != nil {
		return fmt.Errorf("store replay: %w", err)
	}
	return nil
}

func (s *Store) GetReplay(ctx context.Context, key string) ([]model.MatchEvent, error) {
	result, err := s.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("read replay: %w", err)
	}
	defer result.Body.Close()
	payload, err := readAllLimited(result.Body, 8*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("read replay body: %w", err)
	}
	var events []model.MatchEvent
	if err := json.Unmarshal(payload, &events); err != nil {
		return nil, fmt.Errorf("decode replay: %w", err)
	}
	return events, nil
}

func (s *Store) PutBox(ctx context.Context, userID string, box model.BoxRecord) error {
	box.UserID = userID
	box.UpdatedAt = time.Now().UTC()
	payload, err := json.Marshal(box)
	if err != nil {
		return fmt.Errorf("encode box: %w", err)
	}
	_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.config.Table),
		Item: map[string]types.AttributeValue{
			"matchId":   &types.AttributeValueMemberS{Value: "box#" + userID},
			"status":    &types.AttributeValueMemberS{Value: box.Status},
			"payload":   &types.AttributeValueMemberS{Value: string(payload)},
			"updatedAt": &types.AttributeValueMemberS{Value: box.UpdatedAt.Format(time.RFC3339Nano)},
		},
	})
	if err != nil {
		return fmt.Errorf("store box: %w", err)
	}
	return nil
}

func (s *Store) GetBox(ctx context.Context, userID string) (model.BoxRecord, error) {
	result, err := s.dynamo.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.config.Table),
		Key:            map[string]types.AttributeValue{"matchId": &types.AttributeValueMemberS{Value: "box#" + userID}},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return model.BoxRecord{}, fmt.Errorf("read box: %w", err)
	}
	value, ok := result.Item["payload"].(*types.AttributeValueMemberS)
	if !ok {
		return model.BoxRecord{}, errors.New("box not found")
	}
	var box model.BoxRecord
	if err := json.Unmarshal([]byte(value.Value), &box); err != nil {
		return model.BoxRecord{}, fmt.Errorf("decode box: %w", err)
	}
	return box, nil
}

func (s *Store) EnqueueMatch(ctx context.Context, matchID string) error {
	payload, _ := json.Marshal(map[string]string{"matchId": matchID})
	_, err := s.sqs.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: aws.String(s.queueURL), MessageBody: aws.String(string(payload))})
	if err != nil {
		return fmt.Errorf("enqueue match: %w", err)
	}
	return nil
}

type Job struct{ MatchID, ReceiptHandle string }

type AgentCredential struct {
	RobotID   string `json:"robotId"`
	MatchID   string `json:"matchId"`
	TokenHash string `json:"tokenHash"`
}

func (s *Store) PutAgentCredential(ctx context.Context, credential AgentCredential) error {
	payload, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	_, err = s.dynamo.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.config.Table),
		Item: map[string]types.AttributeValue{
			"matchId": &types.AttributeValueMemberS{Value: "agent#" + credential.RobotID},
			"status":  &types.AttributeValueMemberS{Value: "credential"},
			"payload": &types.AttributeValueMemberS{Value: string(payload)},
		},
	})
	if err != nil {
		return fmt.Errorf("store agent credential: %w", err)
	}
	return nil
}

func (s *Store) GetAgentCredential(ctx context.Context, robotID string) (AgentCredential, error) {
	result, err := s.dynamo.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(s.config.Table),
		Key:            map[string]types.AttributeValue{"matchId": &types.AttributeValueMemberS{Value: "agent#" + robotID}},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return AgentCredential{}, fmt.Errorf("read agent credential: %w", err)
	}
	value, ok := result.Item["payload"].(*types.AttributeValueMemberS)
	if !ok {
		return AgentCredential{}, errors.New("agent credential not found")
	}
	var credential AgentCredential
	if err := json.Unmarshal([]byte(value.Value), &credential); err != nil {
		return AgentCredential{}, err
	}
	return credential, nil
}

func (s *Store) ReceiveJob(ctx context.Context) (Job, bool, error) {
	result, err := s.sqs.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl: aws.String(s.queueURL), MaxNumberOfMessages: 1, WaitTimeSeconds: 2, VisibilityTimeout: 120,
	})
	if err != nil {
		return Job{}, false, fmt.Errorf("receive match job: %w", err)
	}
	if len(result.Messages) == 0 {
		return Job{}, false, nil
	}
	var body struct {
		MatchID string `json:"matchId"`
	}
	if err := json.Unmarshal([]byte(aws.ToString(result.Messages[0].Body)), &body); err != nil {
		return Job{}, false, fmt.Errorf("decode match job: %w", err)
	}
	return Job{MatchID: body.MatchID, ReceiptHandle: aws.ToString(result.Messages[0].ReceiptHandle)}, true, nil
}

func (s *Store) DeleteJob(ctx context.Context, receiptHandle string) error {
	_, err := s.sqs.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(s.queueURL), ReceiptHandle: aws.String(receiptHandle)})
	if err != nil {
		return fmt.Errorf("delete match job: %w", err)
	}
	return nil
}

func (s *Store) Status() map[string]string {
	return map[string]string{
		"provider": "Floci (local AWS emulator)", "endpoint": s.config.Endpoint,
		"s3Bucket": s.config.Bucket, "dynamoTable": s.config.Table, "sqsQueue": s.config.Queue,
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
