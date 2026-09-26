package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kryxen/cloud-robot/internal/model"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type recordingWrite struct{ key, source string }
type v4Recorder struct {
	queue chan recordingWrite
	done  chan struct{}
	mu    sync.Mutex
	err   error
	once  sync.Once
}

func (s *Server) newV4Recorder(ctx context.Context) *v4Recorder {
	r := &v4Recorder{queue: make(chan recordingWrite, 16), done: make(chan struct{})}
	go func() {
		defer close(r.done)
		for item := range r.queue {
			if r.Err() != nil {
				continue
			}
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := s.store.PutReplayObject(writeCtx, item.key, item.source)
			cancel()
			if err != nil {
				r.mu.Lock()
				r.err = err
				r.mu.Unlock()
			}
		}
	}()
	return r
}
func (r *v4Recorder) Err() error { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *v4Recorder) Add(key, source string) error {
	if err := r.Err(); err != nil {
		return err
	}
	select {
	case r.queue <- recordingWrite{key, source}:
		return nil
	default:
		return errors.New("replay storage backlog exceeded; match stopped to preserve a complete input record")
	}
}
func (r *v4Recorder) Close() error { r.once.Do(func() { close(r.queue) }); <-r.done; return r.Err() }

// staticLayoutKeys hold map geometry that only changes with the revision.
var staticLayoutKeys = []string{"obstacles", "hazards", "transit", "sites"}

// withoutStaticLayout drops map geometry from a replay frame. Each stored page
// keeps the layout on its first frame only; readers merge it into the rest.
// A dense world's geometry is far larger than its moving entities.
func withoutStaticLayout(frame json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(frame, &fields) != nil {
		return frame
	}
	for _, key := range staticLayoutKeys {
		delete(fields, key)
	}
	stripped, err := json.Marshal(fields)
	if err != nil {
		return frame
	}
	return stripped
}

func packFrames(frames []json.RawMessage) (string, error) {
	raw, err := json.Marshal(frames)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	if _, err = z.Write(raw); err != nil {
		return "", err
	}
	if err = z.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b.Bytes()), nil
}
func unpackFrames(source string) ([]json.RawMessage, error) {
	raw, err := base64.StdEncoding.DecodeString(source)
	if err != nil {
		return nil, err
	}
	z, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	data, err := io.ReadAll(io.LimitReader(z, 16*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16*1024*1024 {
		return nil, errors.New("replay page exceeds size limit")
	}
	var frames []json.RawMessage
	err = json.Unmarshal(data, &frames)
	return frames, err
}
func (s *Server) v4ReplayPage(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, 404, "match not found")
		return
	}
	if m.EngineVersion != 4 || m.Status != model.MatchFinished {
		writeError(w, 409, "complete replay unlocks after the match")
		return
	}
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 0 || page > 540 {
		writeError(w, 400, "page must be 0..540")
		return
	}
	raw, err := s.store.GetReplayObject(r.Context(), fmt.Sprintf("replays/%s/v4/frames-%06d.gz.b64", m.MatchID, page))
	if err != nil {
		writeError(w, 404, "replay page not found")
		return
	}
	frames, err := unpackFrames(raw)
	if err != nil {
		writeError(w, 500, "invalid stored replay")
		return
	}
	writeJSON(w, 200, map[string]any{"frames": frames, "page": page, "ticksPerPage": 100})
}
