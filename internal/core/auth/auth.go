package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/hellboundg/nexus/internal/core/store"
)

var ErrUnauthorized = errors.New("unauthorized")

const (
	CookieName   = "nexus_session"
	APIKeyHeader = "X-Api-Key"

	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

// HashPassword returns an argon2id encoded hash of plain.
func HashPassword(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether plain matches the argon2id encoded hash.
func VerifyPassword(encoded, plain string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid hash format")
	}
	var mem, tme, par uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &tme, &par); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(plain), salt, tme, mem, uint8(par), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

type Service struct {
	store  *store.Store
	apiKey string
}

func NewService(s *store.Store, apiKey string) *Service {
	return &Service{store: s, apiKey: apiKey}
}

func (a *Service) Login(ctx context.Context, username, password string) (string, error) {
	u, err := a.store.GetUserByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", err
	}
	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrUnauthorized
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := a.store.CreateSession(ctx, token, u.ID, time.Now().Add(30*24*time.Hour)); err != nil {
		return "", err
	}
	return token, nil
}

func (a *Service) Logout(ctx context.Context, token string) error {
	return a.store.DeleteSession(ctx, token)
}

func (a *Service) Authenticate(ctx context.Context, token string) (*store.User, error) {
	sess, err := a.store.GetSession(ctx, token)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = a.store.DeleteSession(ctx, token)
		return nil, ErrUnauthorized
	}
	return a.store.GetUserByID(ctx, sess.UserID)
}

// Middleware allows the request if the API key matches or a session cookie authenticates.
func (a *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if k := r.Header.Get(APIKeyHeader); k != "" &&
			subtle.ConstantTimeCompare([]byte(k), []byte(a.apiKey)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(CookieName); err == nil {
			if _, err := a.Authenticate(r.Context(), c.Value); err == nil {
				next.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"authentication required"}}`))
	})
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
