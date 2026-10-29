package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/store"
)

type AccountService struct {
	store *store.Store
	cache *cache.Cache
}

func NewAccountService(st *store.Store, c *cache.Cache) *AccountService {
	return &AccountService{store: st, cache: c}
}

func (s *AccountService) Block(ctx context.Context, userID, targetID uuid.UUID) error {
	if userID == targetID {
		return domain.ErrSelfAction
	}
	if _, err := s.store.TargetProfileVisible(ctx, userID, targetID); err != nil {
		return err
	}
	if err := s.store.BlockUser(ctx, userID, targetID); err != nil {
		return err
	}
	_ = s.store.Enqueue(ctx, "love.events", "user.blocked", userID.String(), map[string]any{
		"blocker_id": userID,
		"blocked_id": targetID,
	})
	return nil
}

func (s *AccountService) Unblock(ctx context.Context, userID, targetID uuid.UUID) error {
	return s.store.UnblockUser(ctx, userID, targetID)
}

func (s *AccountService) Blocked(ctx context.Context, userID uuid.UUID) ([]domain.Profile, error) {
	profiles, err := s.store.ListBlocked(ctx, userID)
	if err != nil {
		return nil, err
	}
	enrichProfiles(ctx, s.cache, profiles)
	return profiles, nil
}

func (s *AccountService) Report(ctx context.Context, reporterID, targetID uuid.UUID, reason, details string) (*domain.Report, error) {
	if reporterID == targetID {
		return nil, domain.ErrSelfAction
	}
	if err := security.ValidateReportReason(reason); err != nil {
		return nil, err
	}
	details = strings.TrimSpace(details)
	if len([]rune(details)) > 2000 {
		return nil, domain.Invalid("details", "details must be at most 2000 characters")
	}
	if _, err := s.store.TargetProfileVisible(ctx, reporterID, targetID); err != nil {
		return nil, err
	}
	report, err := s.store.CreateReport(ctx, reporterID, targetID, reason, details)
	if err != nil {
		return nil, err
	}
	_ = s.store.Enqueue(ctx, "love.events", "user.reported", report.ID.String(), map[string]any{
		"report_id":   report.ID,
		"reporter_id": reporterID,
		"target_id":   targetID,
		"reason":      reason,
	})
	return report, nil
}

func (s *AccountService) SaveDevice(ctx context.Context, userID uuid.UUID, platform, token string) error {
	if err := security.ValidatePlatform(platform); err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 512 {
		return domain.Invalid("token", "device token is required and must be at most 512 characters")
	}
	return s.store.UpsertDevice(ctx, userID, platform, token)
}

func (s *AccountService) DeleteDevice(ctx context.Context, userID uuid.UUID, token string) error {
	return s.store.DeleteDevice(ctx, userID, token)
}

func (s *AccountService) Matches(ctx context.Context, userID uuid.UUID) ([]domain.Match, error) {
	matches, err := s.store.ListMatches(ctx, userID)
	if err != nil {
		return nil, err
	}
	peers := make([]domain.Profile, 0, len(matches))
	for i := range matches {
		if matches[i].Peer != nil {
			peers = append(peers, *matches[i].Peer)
		}
	}
	enrichProfiles(ctx, s.cache, peers)
	byID := make(map[uuid.UUID]domain.Profile, len(peers))
	for _, p := range peers {
		byID[p.UserID] = p
	}
	for i := range matches {
		if matches[i].Peer == nil {
			continue
		}
		if p, ok := byID[matches[i].Peer.UserID]; ok {
			peer := p
			matches[i].Peer = &peer
		}
	}
	return matches, nil
}

func (s *AccountService) Match(ctx context.Context, userID, matchID uuid.UUID) (*domain.Match, error) {
	m, err := s.store.GetMatch(ctx, userID, matchID)
	if err != nil {
		return nil, err
	}
	if m.Peer != nil {
		enrichProfile(ctx, s.cache, m.Peer)
		m.Peer.Online = s.cache.IsOnline(ctx, m.Peer.UserID)
	}
	return m, nil
}

func (s *AccountService) Unmatch(ctx context.Context, userID, matchID uuid.UUID) error {
	return s.store.Unmatch(ctx, userID, matchID)
}
