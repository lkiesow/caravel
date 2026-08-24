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
	// ErrNoLocalPassword means the account authenticates some other way, so
	// there is no password here to change.
	ErrNoLocalPassword = errors.New("account has no local password")
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

	// The first account on an instance becomes its administrator. Someone has
	// to be able to create the second account, and the alternative — a flag or
	// a CLI step to promote yourself — is a setup instruction people skip and
	// then cannot recover from.
	//
	// Counted inside the transaction so two simultaneous first registrations
	// cannot both see an empty table and both become admins. Under SQLite the
	// write lock settles it; the read has to be in the same transaction as the
	// insert for that to hold.
	var user db.User
	err = s.store.WithTx(ctx, func(tx db.Store) error {
		existing, txErr := tx.CountUsers(ctx)
		if txErr != nil {
			return txErr
		}
		user, txErr = tx.CreateUser(ctx, db.CreateUserParams{
			ID:          userID,
			Username:    username,
			DisplayName: displayName,
			IsAdmin:     existing == 0,
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

// HasPassword reports whether the user has a local password at all - which is
// what decides whether a "change my password" control is meaningful. An account
// that only ever authenticated through an external provider has no password
// stored here to change, and the settings screen hides the card rather than
// offering one that cannot work.
func (s *Service) HasPassword(ctx context.Context, user db.User) (bool, error) {
	identity, err := s.store.GetAuthIdentityByProvider(ctx, ProviderLocal, user.Username)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return identity.PasswordHash != nil, nil
}

// ChangePassword replaces a local account's password, requiring the current one.
//
// On success every session belonging to the user is deleted, this one included:
// the point of changing a password is that a leaked one stops working
// everywhere, and leaving other devices logged in would defeat it. The caller is
// expected to start a fresh session for the request it is serving (see
// httpapi.handleChangePassword), so the person doing the change is not logged
// out of the browser they are doing it in.
func (s *Service) ChangePassword(ctx context.Context, user db.User, currentPassword, newPassword string) error {
	identity, err := s.store.GetAuthIdentityByProvider(ctx, ProviderLocal, user.Username)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return ErrNoLocalPassword
		}
		return err
	}
	if identity.PasswordHash == nil {
		return ErrNoLocalPassword
	}
	// The identity is looked up by username, and usernames are unique, so this
	// should be the caller's own identity - checked rather than assumed, because
	// the consequence of being wrong is changing someone else's password.
	if identity.UserID != user.ID {
		return ErrInvalidCredentials
	}

	match, err := argon2id.ComparePasswordAndHash(currentPassword, *identity.PasswordHash)
	if err != nil {
		return err
	}
	if !match {
		return ErrInvalidCredentials
	}

	hash, err := argon2id.CreateHash(newPassword, argon2id.DefaultParams)
	if err != nil {
		return err
	}
	if err := s.store.UpdateAuthIdentityPassword(ctx, ProviderLocal, user.Username, hash); err != nil {
		return err
	}
	return s.store.DeleteSessionsByUserID(ctx, user.ID)
}

// SetPassword replaces a local account's password *without* asking for the
// current one, and without touching the user's sessions.
//
// Two callers, and the difference between them matters. ChangePassword is the
// one *users* go through: it requires the current password and logs every device
// out. This one does neither, which is why it is reached only by cmd/seed and by
// handleAdminResetPassword - an admin resetting a forgotten password should not
// sign the user out of the device they are holding, so a reset is deliberately
// not a way to evict somebody. Removing the account is.
//
// SetPassword exists for cmd/seed, whose whole contract is "these
// are the dev credentials": before this existed, ensureUser silently left an
// existing user's password alone, so once Stage 12 made passwords changeable, a
// changed dev password could not be reset by re-seeding and the documented
// credentials quietly stopped being true. Sessions are left alone on purpose -
// re-seeding should not log the developer out of the browser they are testing
// in.
func (s *Service) SetPassword(ctx context.Context, username, newPassword string) error {
	hash, err := argon2id.CreateHash(newPassword, argon2id.DefaultParams)
	if err != nil {
		return err
	}
	return s.store.UpdateAuthIdentityPassword(ctx, ProviderLocal, username, hash)
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
