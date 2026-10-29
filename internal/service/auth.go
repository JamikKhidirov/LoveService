package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/mailer"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/store"
)

type AuthService struct {
	store  *store.Store
	tokens *security.TokenManager
	mailer mailer.Mailer
	log    *slog.Logger
	env    string
}

func NewAuthService(st *store.Store, tokens *security.TokenManager, m mailer.Mailer, env string, log *slog.Logger) *AuthService {
	return &AuthService{store: st, tokens: tokens, mailer: m, log: log, env: env}
}

type RequestMeta struct {
	UserAgent string
	IP        string
}

type RegisterInput struct {
	Email            string
	Password         string
	DisplayName      string
	BirthDate        string
	Gender           string
	Phone            string
	PreferredGenders []string
}

type TokenPair struct {
	AccessToken   string    `json:"access_token"`
	RefreshToken  string    `json:"refresh_token"`
	TokenType     string    `json:"token_type"`
	ExpiresIn     int64     `json:"expires_in"`
	AccessExpiry  time.Time `json:"access_expires_at"`
	RefreshExpiry time.Time `json:"refresh_expires_at"`
}

type AuthResult struct {
	User    *domain.User    `json:"user"`
	Profile *domain.Profile `json:"profile"`
	Tokens  TokenPair       `json:"tokens"`
	DevCode string          `json:"dev_code,omitempty"`
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput, meta RequestMeta) (*AuthResult, error) {
	email := security.NormalizeEmail(in.Email)
	if err := security.ValidateEmail(email); err != nil {
		return nil, err
	}
	if err := security.ValidatePassword(in.Password); err != nil {
		return nil, err
	}
	if err := security.ValidateDisplayName(in.DisplayName); err != nil {
		return nil, err
	}
	birth, err := security.ValidateDateLayout(in.BirthDate)
	if err != nil {
		return nil, err
	}
	if err := security.ValidateBirthDate(birth); err != nil {
		return nil, err
	}
	if err := security.ValidateGender(in.Gender); err != nil {
		return nil, err
	}
	if err := security.ValidatePhone(in.Phone); err != nil {
		return nil, err
	}
	preferred, err := normalizeGenders(in.PreferredGenders, in.Gender)
	if err != nil {
		return nil, err
	}
	hash, err := security.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	var phone *string
	if in.Phone != "" {
		p := in.Phone
		phone = &p
	}

	user, err := s.store.CreateUserWithProfile(ctx, store.CreateUserInput{
		Email:            email,
		Phone:            phone,
		PasswordHash:     hash,
		DisplayName:      in.DisplayName,
		BirthDate:        birth,
		Gender:           in.Gender,
		PreferredGenders: preferred,
	})
	if err != nil {
		return nil, err
	}

	code, sendErr := s.sendCode(ctx, email, "verify_email")
	if sendErr != nil {
		s.log.Warn("could not send verification email", "error", sendErr)
	}

	pair, err := s.issueTokens(ctx, user, meta)
	if err != nil {
		return nil, err
	}
	profile, _ := s.store.GetProfile(ctx, user.ID)
	enrichProfile(ctx, nil, profile)
	if err := s.store.Enqueue(ctx, "love.events", "user.registered", user.ID.String(), map[string]any{
		"user_id": user.ID,
		"email":   user.Email,
		"name":    in.DisplayName,
	}); err != nil {
		s.log.Warn("could not enqueue user.registered", "error", err)
	}

	result := &AuthResult{User: user, Profile: profile, Tokens: pair}
	if s.env != "production" {
		result.DevCode = code
	}
	return result, nil
}

func (s *AuthService) Login(ctx context.Context, email, password string, meta RequestMeta) (*AuthResult, error) {
	email = security.NormalizeEmail(email)
	if err := security.ValidateEmail(email); err != nil {
		return nil, domain.ErrInvalidCredentials
	}
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrInvalidCredentials
		}
		return nil, err
	}
	if !security.CheckPassword(user.PasswordHash, password) {
		return nil, domain.ErrInvalidCredentials
	}
	if user.Status == domain.StatusBanned {
		return nil, domain.ErrForbidden
	}
	if user.Status != domain.StatusActive {
		return nil, domain.ErrForbidden
	}
	pair, err := s.issueTokens(ctx, user, meta)
	if err != nil {
		return nil, err
	}
	profile, _ := s.store.GetProfile(ctx, user.ID)
	enrichProfile(ctx, nil, profile)
	return &AuthResult{User: user, Profile: profile, Tokens: pair}, nil
}

func (s *AuthService) Refresh(ctx context.Context, rawRefresh string, meta RequestMeta) (*AuthResult, error) {
	hash := security.HashToken(rawRefresh)
	stored, err := s.store.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		return nil, domain.ErrInvalidToken
	}
	if stored.RevokedAt != nil || stored.ExpiresAt.Before(time.Now().UTC()) {
		return nil, domain.ErrInvalidToken
	}
	user, err := s.store.GetUserByID(ctx, stored.UserID)
	if err != nil || user.Status != domain.StatusActive {
		return nil, domain.ErrInvalidToken
	}
	if err := s.store.RevokeRefreshToken(ctx, stored.ID); err != nil {
		return nil, err
	}
	pair, err := s.issueTokens(ctx, user, meta)
	if err != nil {
		return nil, err
	}
	profile, _ := s.store.GetProfile(ctx, user.ID)
	enrichProfile(ctx, nil, profile)
	return &AuthResult{User: user, Profile: profile, Tokens: pair}, nil
}

func (s *AuthService) Logout(ctx context.Context, rawRefresh string) error {
	if rawRefresh == "" {
		return nil
	}
	stored, err := s.store.GetRefreshTokenByHash(ctx, security.HashToken(rawRefresh))
	if err != nil {
		if errors.Is(err, domain.ErrInvalidToken) || errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	return s.store.RevokeRefreshToken(ctx, stored.ID)
}

func (s *AuthService) Me(ctx context.Context, userID uuid.UUID) (*domain.User, *domain.Profile, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	profile, err := s.store.GetProfile(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	enrichProfile(ctx, nil, profile)
	return user, profile, nil
}

func (s *AuthService) VerifyEmail(ctx context.Context, email, code string) error {
	email = security.NormalizeEmail(email)
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return domain.ErrInvalidToken
	}
	if user.EmailVerified() {
		return nil
	}
	if err := s.consumeCode(ctx, email, "verify_email", code); err != nil {
		return err
	}
	if err := s.store.SetEmailVerified(ctx, user.ID); err != nil {
		return err
	}
	_ = s.store.Enqueue(ctx, "love.events", "user.email_verified", user.ID.String(), map[string]any{"user_id": user.ID})
	return nil
}

func (s *AuthService) ResendVerification(ctx context.Context, email string) (string, error) {
	email = security.NormalizeEmail(email)
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		// Do not leak account existence.
		return "", nil
	}
	if user.EmailVerified() {
		return "", nil
	}
	code, err := s.sendCode(ctx, email, "verify_email")
	if s.env == "production" {
		return "", err
	}
	return code, err
}

func (s *AuthService) ForgotPassword(ctx context.Context, email string) (string, error) {
	email = security.NormalizeEmail(email)
	if err := security.ValidateEmail(email); err != nil {
		return "", nil
	}
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil || user.Status != domain.StatusActive {
		return "", nil
	}
	code, err := s.sendCode(ctx, email, "reset_password")
	if s.env == "production" {
		return "", err
	}
	return code, err
}

func (s *AuthService) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	email = security.NormalizeEmail(email)
	if err := security.ValidatePassword(newPassword); err != nil {
		return err
	}
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return domain.ErrInvalidToken
	}
	if err := s.consumeCode(ctx, email, "reset_password", code); err != nil {
		return err
	}
	hash, err := security.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.UpdatePassword(ctx, user.ID, hash); err != nil {
		return err
	}
	if err := s.store.RevokeUserRefreshTokens(ctx, user.ID); err != nil {
		return err
	}
	_ = s.store.Enqueue(ctx, "love.events", "user.password_reset", user.ID.String(), map[string]any{"user_id": user.ID})
	return nil
}

func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, current, next string, meta RequestMeta) (*TokenPair, error) {
	if err := security.ValidatePassword(next); err != nil {
		return nil, err
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !security.CheckPassword(user.PasswordHash, current) {
		return nil, domain.Invalid("current_password", "current password is incorrect")
	}
	hash, err := security.HashPassword(next)
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdatePassword(ctx, userID, hash); err != nil {
		return nil, err
	}
	// The password change bumped token_version in the database, so the access
	// token for this response must be minted with the new version.
	user.TokenVersion++
	if err := s.store.RevokeUserRefreshTokens(ctx, userID); err != nil {
		return nil, err
	}
	pair, err := s.issueTokens(ctx, user, meta)
	if err != nil {
		return nil, err
	}
	return &pair, nil
}

// LogoutAll revokes every refresh token of the user and invalidates all access
// tokens by bumping the account token version.
func (s *AuthService) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	return s.store.LogoutAll(ctx, userID)
}

func (s *AuthService) issueTokens(ctx context.Context, user *domain.User, meta RequestMeta) (TokenPair, error) {
	access, accessExp, err := s.tokens.IssueAccess(user)
	if err != nil {
		return TokenPair{}, err
	}
	raw, hash, err := security.NewOpaqueToken()
	if err != nil {
		return TokenPair{}, err
	}
	refreshExp := time.Now().UTC().Add(s.tokens.RefreshTTL())
	token := &domain.RefreshToken{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: refreshExp,
		UserAgent: meta.UserAgent,
		IP:        meta.IP,
	}
	if err := s.store.CreateRefreshToken(ctx, token); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:   access,
		RefreshToken:  raw,
		TokenType:     "Bearer",
		ExpiresIn:     int64(time.Until(accessExp).Seconds()),
		AccessExpiry:  accessExp,
		RefreshExpiry: refreshExp,
	}, nil
}

func (s *AuthService) sendCode(ctx context.Context, email, purpose string) (string, error) {
	code, err := security.NewNumericCode(6)
	if err != nil {
		return "", err
	}
	expires := time.Now().UTC().Add(15 * time.Minute)
	if err := s.store.CreateEmailCode(ctx, email, purpose, security.HashCode(code), expires); err != nil {
		return "", err
	}
	message := mailer.VerificationEmail(code)
	if purpose == "reset_password" {
		message = mailer.ResetPasswordEmail(code)
	}
	go func() {
		sendCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := s.mailer.SendMessage(sendCtx, email, message); err != nil {
			s.log.Warn("send email failed", "to", email, "subject", message.Subject, "error", err)
		}
	}()
	return code, nil
}

func (s *AuthService) consumeCode(ctx context.Context, email, purpose, code string) error {
	stored, err := s.store.GetActiveEmailCode(ctx, email, purpose)
	if err != nil {
		return domain.ErrInvalidToken
	}
	attempts, err := s.store.IncrementEmailCodeAttempts(ctx, stored.ID)
	if err != nil {
		return err
	}
	if attempts > 5 {
		return domain.ErrInvalidToken
	}
	if !security.VerifyCode(stored.CodeHash, code) {
		return domain.ErrInvalidToken
	}
	return s.store.ConsumeEmailCode(ctx, stored.ID)
}

func normalizeGenders(raw []string, ownGender string) ([]string, error) {
	if len(raw) == 0 {
		switch ownGender {
		case domain.GenderMale:
			return []string{domain.GenderFemale}, nil
		case domain.GenderFemale:
			return []string{domain.GenderMale}, nil
		default:
			return []string{domain.GenderMale, domain.GenderFemale, domain.GenderOther}, nil
		}
	}
	out := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, g := range raw {
		g = normalizeGender(g)
		if err := security.ValidateGender(g); err != nil {
			return nil, domain.Invalid("preferred_genders", "preferred_genders must contain male, female or other")
		}
		if _, ok := seen[g]; !ok {
			seen[g] = struct{}{}
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		return nil, domain.Invalid("preferred_genders", "at least one preferred gender is required")
	}
	return out, nil
}

func normalizeGender(g string) string {
	switch g {
	case "m", "man", "male", "мужчина", "м":
		return domain.GenderMale
	case "f", "woman", "female", "женщина", "ж":
		return domain.GenderFemale
	case "o", "other", "другое":
		return domain.GenderOther
	default:
		return g
	}
}
