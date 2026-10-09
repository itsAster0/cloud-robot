package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Local accounts let reviewers sign in with a username and password when
// WorkOS is unreachable or unconfigured. They are an opt-in demo path
// (LOCAL_AUTH_ENABLED=true), not a replacement for a hosted identity provider.
const (
	localIssuer     = "robot-arena-local"
	localUserPrefix = "local:"
	localSessionTTL = 30 * 24 * time.Hour
	// OWASP's 2023 floor for PBKDF2-HMAC-SHA256.
	pbkdf2Iterations = 600_000
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,23}$`)

type LocalAuth struct {
	enabled bool
	secret  []byte
}

func NewLocalAuth() *LocalAuth {
	l := &LocalAuth{enabled: strings.EqualFold(os.Getenv("LOCAL_AUTH_ENABLED"), "true")}
	if !l.enabled {
		return l
	}
	if secret := os.Getenv("LOCAL_AUTH_SECRET"); secret != "" {
		l.secret = []byte(secret)
	} else {
		// Without a configured secret, local sessions end when the API restarts.
		l.secret = make([]byte, 32)
		_, _ = rand.Read(l.secret)
		slog.Warn("LOCAL_AUTH_SECRET is unset; local sign-ins reset on API restart")
	}
	return l
}

func (l *LocalAuth) Enabled() bool { return l != nil && l.enabled }

// NormalizeUsername lowercases and validates a local account name.
func NormalizeUsername(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !usernamePattern.MatchString(name) {
		return "", errors.New("username must be 3-24 characters: letters, digits, - or _, starting with a letter or digit")
	}
	return name, nil
}

func ValidatePassword(password string) error {
	if len(password) < 8 || len(password) > 128 {
		return errors.New("password must be 8-128 characters")
	}
	return nil
}

// LocalUserID is the identity stored on boxes, matches, and stats.
func LocalUserID(username string) string { return localUserPrefix + username }

// Issue signs a session token for a local account.
func (l *LocalAuth) Issue(username string, now time.Time) (string, time.Time, error) {
	if !l.Enabled() {
		return "", time.Time{}, errors.New("local accounts are disabled")
	}
	expires := now.Add(localSessionTTL)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": localIssuer, "sub": LocalUserID(username), "name": username,
		"iat": now.Unix(), "exp": expires.Unix(),
	})
	signed, err := token.SignedString(l.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign local session: %w", err)
	}
	return signed, expires, nil
}

// parse returns the user ID of a valid local session token. ok is false for
// anything else, including WorkOS tokens, so the caller can fall through.
func (l *LocalAuth) parse(token string) (string, bool) {
	if !l.Enabled() {
		return "", false
	}
	parsed, err := jwt.Parse(token, func(*jwt.Token) (any, error) { return l.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(localIssuer), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		return "", false
	}
	sub, _ := parsed.Claims.GetSubject()
	if !strings.HasPrefix(sub, localUserPrefix) {
		return "", false
	}
	return sub, true
}

// HashPassword returns "pbkdf2-sha256$<iterations>$<salt>$<hash>".
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func CheckPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
