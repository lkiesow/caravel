// Package auth implements local username/password authentication and cookie
// sessions. Credentials live in the auth_identities table (provider="local"),
// not on the users table itself, so that OpenID Connect can be added later
// as a second provider without migrating existing user rows.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"

	"caravel/internal/db"
)

const (
	ProviderLocal = "local"

	// SessionCookieName is the name of the cookie carrying the raw session
	// token. Only its SHA-256 hash is ever stored server-side.
	SessionCookieName = "caravel_session"

	sessionIdleTTL = 30 * 24 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUsernameTaken      = errors.New("username already taken")
)

type Service struct {
	store db.Store
	now   func() time.Time
}

func NewService(store db.Store) *Service {
	return &Service{store: store, now: time.Now}
}

// Register creates a new user with a local password identity.
func (s *Service) Register(ctx context.Context, username, password, displayName string) (db.User, error) {
	if _, err := s.store.GetUserByUsername(ctx, username); err == nil {
		return db.User{}, ErrUsernameTaken
	} else if !errors.Is(err, db.ErrNotFound) {
		return db.User{}, err
	}

	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return db.User{}, err
	}

	now := s.now().UTC()
	userID := uuid.NewString()

	var user db.User
	err = s.store.WithTx(ctx, func(tx db.Store) error {
		var txErr error
		user, txErr = tx.CreateUser(ctx, db.CreateUserParams{
			ID:          userID,
			Username:    username,
			DisplayName: displayName,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		if txErr != nil {
			return txErr
		}
		_, txErr = tx.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
			ID:             uuid.NewString(),
			UserID:         userID,
			Provider:       ProviderLocal,
			ProviderUserID: username,
			PasswordHash:   &hash,
			CreatedAt:      now,
		})
		return txErr
	})
	if err != nil {
		return db.User{}, err
	}
	return user, nil
}

// Authenticate verifies a username/password pair against the local
// auth_identity and returns the matching user on success.
func (s *Service) Authenticate(ctx context.Context, username, password string) (db.User, error) {
	identity, err := s.store.GetAuthIdentityByProvider(ctx, ProviderLocal, username)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return db.User{}, ErrInvalidCredentials
		}
		return db.User{}, err
	}
	if identity.PasswordHash == nil {
		return db.User{}, ErrInvalidCredentials
	}

	match, err := argon2id.ComparePasswordAndHash(password, *identity.PasswordHash)
	if err != nil {
		return db.User{}, err
	}
	if !match {
		return db.User{}, ErrInvalidCredentials
	}

	return s.store.GetUserByID(ctx, identity.UserID)
}

// StartSession creates a new session and returns the raw token to set as a
// cookie. Only its hash is persisted.
func (s *Service) StartSession(ctx context.Context, userID, userAgent, ip string) (rawToken string, session db.Session, err error) {
	rawToken, err = generateToken()
	if err != nil {
		return "", db.Session{}, err
	}

	now := s.now().UTC()
	var uaPtr, ipPtr *string
	if userAgent != "" {
		uaPtr = &userAgent
	}
	if ip != "" {
		ipPtr = &ip
	}

	session, err = s.store.CreateSession(ctx, db.CreateSessionParams{
		ID:         hashToken(rawToken),
		UserID:     userID,
		CreatedAt:  now,
		ExpiresAt:  now.Add(sessionIdleTTL),
		LastSeenAt: now,
		UserAgent:  uaPtr,
		IP:         ipPtr,
	})
	return rawToken, session, err
}

// ValidateSession looks up the session for a raw cookie token, extending its
// idle expiration on success (sliding expiration).
func (s *Service) ValidateSession(ctx context.Context, rawToken string) (db.User, error) {
	session, err := s.store.GetSessionByID(ctx, hashToken(rawToken))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return db.User{}, ErrInvalidCredentials
		}
		return db.User{}, err
	}

	now := s.now().UTC()
	if now.After(session.ExpiresAt) {
		_ = s.store.DeleteSession(ctx, session.ID)
		return db.User{}, ErrInvalidCredentials
	}

	if err := s.store.TouchSession(ctx, session.ID, now, now.Add(sessionIdleTTL)); err != nil {
		return db.User{}, err
	}

	return s.store.GetUserByID(ctx, session.UserID)
}

// Logout deletes the session backing the given raw cookie token.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	return s.store.DeleteSession(ctx, hashToken(rawToken))
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
