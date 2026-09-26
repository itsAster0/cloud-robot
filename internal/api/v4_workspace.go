package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	robotauth "github.com/kryxen/cloud-robot/internal/auth"
	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/enginev4"
	"net/http"
	"strings"
)

func revision(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}
func (s *Server) v4Script(w http.ResponseWriter, r *http.Request) {
	boxID := boxes.IDForUser(robotauth.UserID(r.Context()))
	if r.Method == http.MethodGet {
		source, err := s.boxes.ReadMain(r.Context(), boxID)
		if err != nil {
			writeError(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"source": source, "revision": revision(source)})
		return
	}
	var input struct {
		Source   string `json:"source"`
		Revision string `json:"revision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(input.Source) == "" || len(input.Source) > 16*1024 || len(input.Revision) != 64 {
		writeError(w, 400, "source must be 1..16384 bytes and a loaded revision is required")
		return
	}
	writer, ok := s.boxes.(interface {
		WriteMainRevision(context.Context, string, string, string) (string, error)
	})
	if !ok {
		writeError(w, 503, "provisioner does not support revision-checked saves")
		return
	}
	written, err := writer.WriteMainRevision(r.Context(), boxID, input.Source, input.Revision)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if err = s.store.PutScript(r.Context(), fmt.Sprintf("versions/%s/%s/main.lua", boxID, uuid.NewString()), written); err != nil {
		writeError(w, 502, "file saved but version storage failed: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"source": written, "revision": revision(written)})
}
func (s *Server) v4ValidateScript(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Source string `json:"source"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if len(input.Source) == 0 || len(input.Source) > 16*1024 {
		writeError(w, 400, "source must be 1..16384 bytes")
		return
	}
	validator, ok := s.boxes.(interface {
		ValidateMain(context.Context, string, string) error
	})
	if !ok {
		writeError(w, 503, "provisioner does not support Lua validation")
		return
	}
	if err := validator.ValidateMain(r.Context(), boxes.IDForUser(robotauth.UserID(r.Context())), input.Source); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"valid": true})
}
func (s *Server) v4Loadout(w http.ResponseWriter, r *http.Request) {
	key := "loadouts/" + boxes.IDForUser(robotauth.UserID(r.Context())) + "/default.json"
	if r.Method == http.MethodGet {
		raw, err := s.store.GetScript(r.Context(), key)
		if err != nil {
			var l enginev4.Loadout
			l.Defaults()
			writeJSON(w, 200, l)
			return
		}
		writeJSON(w, 200, json.RawMessage(raw))
		return
	}
	var l enginev4.Loadout
	if err := decodeJSON(w, r, &l); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	l.Defaults()
	if err := l.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	raw, _ := json.Marshal(l)
	if err := s.store.PutScript(r.Context(), key, string(raw)); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, l)
}
