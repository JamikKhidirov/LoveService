package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/store"
)

type SwipeService struct {
	store          *store.Store
	cache          *cache.Cache
	log            *slog.Logger
	likeLimit      int
	superlikeLimit int
	rewindWindow   time.Duration
}

func NewSwipeService(st *store.Store, c *cache.Cache, likeLimit, superlikeLimit int, rewindWindow time.Duration, log *slog.Logger) *SwipeService {
	if likeLimit <= 0 {
		likeLimit = 100
	}
	if superlikeLimit <= 0 {
		superlikeLimit = 5
	}
	if rewindWindow <= 0 {
		rewindWindow = 5 * time.Minute
	}
	return &SwipeService{store: st, cache: c, log: log, likeLimit: likeLimit, superlikeLimit: superlikeLimit, rewindWindow: rewindWindow}
}

type SwipeResponse struct {
	Matched             bool            `json:"matched"`
	Match               *domain.Match   `json:"match,omitempty"`
	ConversationID      *uuid.UUID      `json:"conversation_id,omitempty"`
	Peer                *domain.Profile `json:"peer,omitempty"`
	Action              string          `json:"action"`
	LikesRemaining      int             `json:"likes_remaining"`
	SuperlikesRemaining int             `json:"superlikes_remaining"`
}

type DailyLimit struct {
	Limit     int       `json:"limit"`
	Used      int       `json:"used"`
	Remaining int       `json:"remaining"`
	ResetsAt  time.Time `json:"resets_at"`
}

type SwipeLimits struct {
	Likes               DailyLimit `json:"likes"`
	Superlikes          DailyLimit `json:"superlikes"`
	RewindWindowSeconds int        `json:"rewind_window_seconds"`
}

func (s *SwipeService) Swipe(ctx context.Context, swiperID, targetID uuid.UUID, action string) (*SwipeResponse, error) {
	switch action {
	case domain.ActionLike, domain.ActionDislike, domain.ActionSuperlike:
	default:
		return nil, domain.Invalid("action", "action must be like, dislike or superlike")
	}
	if swiperID == targetID {
		return nil, domain.ErrSelfAction
	}
	if _, err := s.store.TargetProfileVisible(ctx, swiperID, targetID); err != nil {
		return nil, err
	}

	if action == domain.ActionLike || action == domain.ActionSuperlike {
		limits, err := s.Limits(ctx, swiperID)
		if err != nil {
			return nil, err
		}
		if action == domain.ActionLike && limits.Likes.Remaining <= 0 {
			return nil, domain.ErrDailyLimitReached
		}
		if action == domain.ActionSuperlike && limits.Superlikes.Remaining <= 0 {
			return nil, domain.ErrDailyLimitReached
		}
	}

	result, err := s.store.CreateSwipe(ctx, swiperID, targetID, action)
	if err != nil {
		return nil, err
	}
	if action == domain.ActionLike || action == domain.ActionSuperlike {
		s.incrementCounter(ctx, action, swiperID)
	}

	resp := &SwipeResponse{Action: action}
	if result.Match != nil {
		resp.Matched = true
		resp.Match = result.Match
		resp.ConversationID = result.ConversationID
		peer, err := s.store.GetProfile(ctx, targetID)
		if err == nil {
			enrichProfile(ctx, s.cache, peer)
			peer.Online = s.cache.IsOnline(ctx, peer.UserID)
			resp.Peer = peer
			resp.Match.Peer = peer
		}
	}
	limits, err := s.Limits(ctx, swiperID)
	if err == nil {
		resp.LikesRemaining = limits.Likes.Remaining
		resp.SuperlikesRemaining = limits.Superlikes.Remaining
	}
	return resp, nil
}

func (s *SwipeService) Limits(ctx context.Context, userID uuid.UUID) (*SwipeLimits, error) {
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
	likeUsed := int(s.cache.Count(ctx, cache.DailyKey("limit:like", userID, now)))
	superUsed := int(s.cache.Count(ctx, cache.DailyKey("limit:superlike", userID, now)))
	if !s.cache.Available() {
		var err error
		likeUsed, err = s.store.CountSwipesTodayByAction(ctx, userID, domain.ActionLike)
		if err != nil {
			return nil, err
		}
		superUsed, err = s.store.CountSwipesTodayByAction(ctx, userID, domain.ActionSuperlike)
		if err != nil {
			return nil, err
		}
	}
	return &SwipeLimits{
		Likes:               DailyLimit{Limit: s.likeLimit, Used: likeUsed, Remaining: maxInt(0, s.likeLimit-likeUsed), ResetsAt: midnight},
		Superlikes:          DailyLimit{Limit: s.superlikeLimit, Used: superUsed, Remaining: maxInt(0, s.superlikeLimit-superUsed), ResetsAt: midnight},
		RewindWindowSeconds: int(s.rewindWindow.Seconds()),
	}, nil
}

func (s *SwipeService) incrementCounter(ctx context.Context, action string, userID uuid.UUID) {
	if !s.cache.Available() {
		return
	}
	now := time.Now().UTC()
	ttl := cache.SecondsUntilMidnight(now) + time.Minute
	key := cache.DailyKey("limit:like", userID, now)
	if action == domain.ActionSuperlike {
		key = cache.DailyKey("limit:superlike", userID, now)
	}
	if _, err := s.cache.Incr(ctx, key, ttl); err != nil {
		s.log.Warn("could not increment swipe counter", "error", err)
	}
}

func (s *SwipeService) Rewind(ctx context.Context, userID uuid.UUID) (*domain.Swipe, error) {
	return s.store.Rewind(ctx, userID, s.rewindWindow)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
