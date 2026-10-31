package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/loveservice/loveservice/internal/domain"
)

func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

type Claims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Type   string `json:"typ"`
	// SessionID identifies the auth_sessions row (one device login). Revoking
	// that row invalidates this access token immediately.
	SessionID string `json:"sid,omitempty"`
	// Version mirrors users.token_version. Bumping it invalidates all access
	// tokens issued before the bump (logout-all, password change, forced ban).
	Version int `json:"ver"`
	jwt.RegisteredClaims
}

type TokenManager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	issuer     string
}

func NewTokenManager(secret string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		issuer:     "loveservice",
	}
}

func (m *TokenManager) AccessTTL() time.Duration  { return m.accessTTL }
func (m *TokenManager) RefreshTTL() time.Duration { return m.refreshTTL }

func (m *TokenManager) IssueAccess(user *domain.User) (string, time.Time, error) {
	return m.IssueAccessForSession(user, uuid.Nil)
}

// IssueAccessForSession mints an access token bound to a device session. An
// empty session id is allowed for backwards compatibility and internal callers.
func (m *TokenManager) IssueAccessForSession(user *domain.User, sessionID uuid.UUID) (string, time.Time, error) {
	now := time.Now().UTC()
	expires := now.Add(m.accessTTL)
	claims := Claims{
		UserID:  user.ID.String(),
		Email:   user.Email,
		Role:    user.Role,
		Type:    "access",
		Version: user.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   user.ID.String(),
			Audience:  jwt.ClaimStrings{"loveservice-api"},
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	}
	if sessionID != uuid.Nil {
		claims.SessionID = sessionID.String()
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expires, nil
}

func (m *TokenManager) ParseAccess(raw string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method %q", t.Method.Alg())
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(m.issuer), jwt.WithAudience("loveservice-api"))
	if err != nil || !token.Valid {
		return nil, domain.ErrInvalidToken
	}
	if claims.Type != "access" {
		return nil, domain.ErrInvalidToken
	}
	if _, err := uuid.Parse(claims.UserID); err != nil {
		return nil, domain.ErrInvalidToken
	}
	return claims, nil
}

// NewOpaqueToken returns a URL-safe random token and its SHA-256 hash. Only the
// hash is persisted, so a database leak cannot be used to hijack sessions.
func NewOpaqueToken() (raw, hash string, err error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashToken(raw), nil
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func NewNumericCode(length int) (string, error) {
	if length <= 0 {
		length = 6
	}
	var b strings.Builder
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + n.Int64()))
	}
	return b.String(), nil
}

func HashCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

func VerifyCode(hash, code string) bool {
	return subtleEqual(hash, HashCode(code))
}

func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

var ErrWeakPassword = errors.New("password is too weak")
