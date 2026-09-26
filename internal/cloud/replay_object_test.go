package cloud

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type replayTransport func(*http.Request) (*http.Response, error)

func (f replayTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReplayObjectBoundsDoNotRelaxScriptLimit(t *testing.T) {
	payload := strings.Repeat("x", 32*1024)
	calls := 0
	client := s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""), HTTPClient: &http.Client{Transport: replayTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
	})}})
	store := &Store{config: Config{Bucket: "test"}, s3: client}
	ctx := context.Background()
	got, err := store.GetReplayObject(ctx, "replay")
	if err != nil || got != payload {
		t.Fatalf("replay larger than script: %v", err)
	}
	if _, err := store.GetScript(ctx, "script"); err == nil {
		t.Fatal("script read accepted oversized source")
	}
	payload = strings.Repeat("x", maxReplayBytes+1)
	if _, err := store.GetReplayObject(ctx, "oversized"); err == nil {
		t.Fatal("replay read accepted oversized object")
	}
	before := calls
	if err := store.PutReplayObject(ctx, "oversized", payload); err == nil {
		t.Fatal("replay write accepted oversized object")
	}
	if calls != before {
		t.Fatal("oversized write reached storage")
	}
}
