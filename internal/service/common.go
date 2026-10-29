package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
)

// Broadcaster pushes realtime events to connected users.
type Broadcaster interface {
	SendToUser(userID uuid.UUID, eventType string, data any)
}

func enrichProfile(_ context.Context, _ *cache.Cache, p *domain.Profile) {
	if p == nil {
		return
	}
	p.Age = security.Age(p.BirthDate, time.Now().UTC())
}

func enrichProfiles(ctx context.Context, c *cache.Cache, profiles []domain.Profile) {
	if len(profiles) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(profiles))
	for i := range profiles {
		profiles[i].Age = security.Age(profiles[i].BirthDate, time.Now().UTC())
		ids = append(ids, profiles[i].UserID)
	}
	online := c.OnlineMany(ctx, ids)
	for i := range profiles {
		profiles[i].Online = online[profiles[i].UserID]
	}
}

func applyViewerVisibility(p *domain.Profile, settings *domain.UserSettings) {
	if p == nil || settings == nil {
		return
	}
	if !settings.ShowDistance {
		p.DistanceKM = nil
	}
	if !settings.ShowOnline {
		p.Online = false
	}
}
