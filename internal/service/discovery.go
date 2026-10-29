package service

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/store"
)

type DiscoveryService struct {
	store *store.Store
	cache *cache.Cache
}

func NewDiscoveryService(st *store.Store, c *cache.Cache) *DiscoveryService {
	return &DiscoveryService{store: st, cache: c}
}

type DiscoverParams struct {
	Lat      *float64
	Lng      *float64
	RadiusKM *float64
	MinAge   *int
	MaxAge   *int
	Genders  []string
	Limit    int
	Offset   int
	Expand   bool
}

func (s *DiscoveryService) Discover(ctx context.Context, viewerID uuid.UUID, in DiscoverParams) (*domain.DiscoveryResult, error) {
	viewer, err := s.store.GetProfile(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	prefs, err := s.store.GetPreferences(ctx, viewerID)
	if err != nil {
		return nil, err
	}

	lat, lng, hasLocation := 0.0, 0.0, false
	if in.Lat != nil && in.Lng != nil {
		if err := security.ValidateCoordinates(*in.Lat, *in.Lng); err != nil {
			return nil, err
		}
		lat, lng, hasLocation = *in.Lat, *in.Lng, true
	} else if viewer.Lat != nil && viewer.Lng != nil {
		lat, lng, hasLocation = *viewer.Lat, *viewer.Lng, true
	}
	if !hasLocation {
		return nil, domain.Invalid("location", "send lat and lng or update your profile location first")
	}

	minAge := prefs.MinAge
	if in.MinAge != nil {
		minAge = *in.MinAge
	}
	maxAge := prefs.MaxAge
	if in.MaxAge != nil {
		maxAge = *in.MaxAge
	}
	if minAge < 18 {
		return nil, domain.Invalid("min_age", "min_age must be at least 18")
	}
	if maxAge > 120 || maxAge < minAge {
		return nil, domain.Invalid("max_age", "max_age must be between min_age and 120")
	}

	genders := prefs.PreferredGenders
	if len(in.Genders) > 0 {
		genders, err = normalizeGenders(in.Genders, "")
		if err != nil {
			return nil, err
		}
	}

	radius := float64(prefs.MaxDistanceKM)
	if in.RadiusKM != nil {
		radius = *in.RadiusKM
	}
	if prefs.GlobalMode {
		radius = 20000
	}
	radius = domain.Clamp(radius, 1, 20000)
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}

	query := domain.DiscoveryQuery{
		ViewerID: viewerID, Lat: lat, Lng: lng,
		RadiusKM: radius, MinAge: minAge, MaxAge: maxAge,
		Genders: genders, Limit: limit, Offset: offset,
	}

	var profiles []domain.Profile
	var total int
	expandedTo := radius

	if in.Expand && offset == 0 && radius < 20000 {
		seen := map[uuid.UUID]struct{}{}
		for _, tier := range domain.RadiusTiers(radius) {
			tierProfiles, tierTotal, err := s.store.Discover(ctx, domain.DiscoveryQuery{
				ViewerID: viewerID, Lat: lat, Lng: lng,
				RadiusKM: tier, MinAge: minAge, MaxAge: maxAge,
				Genders: genders, Limit: limit, Offset: 0,
			})
			if err != nil {
				return nil, err
			}
			for _, p := range tierProfiles {
				if _, ok := seen[p.UserID]; ok {
					continue
				}
				seen[p.UserID] = struct{}{}
				profiles = append(profiles, p)
			}
			total = tierTotal
			expandedTo = tier
			if len(profiles) >= limit {
				break
			}
		}
	} else {
		profiles, total, err = s.store.Discover(ctx, query)
		if err != nil {
			return nil, err
		}
	}

	if len(profiles) > limit {
		profiles = profiles[:limit]
	}
	if in.Expand && offset == 0 {
		if n, err := s.store.CountCandidates(ctx, domain.DiscoveryQuery{
			ViewerID: viewerID, Lat: lat, Lng: lng,
			RadiusKM: expandedTo, MinAge: minAge, MaxAge: maxAge, Genders: genders,
		}); err == nil {
			total = n
		}
	}

	enrichProfiles(ctx, s.cache, profiles)
	s.applyPrivacy(ctx, profiles)

	hasMore := total > offset+len(profiles)
	return &domain.DiscoveryResult{
		Profiles:         profiles,
		RadiusKM:         radius,
		ExpandedToRadius: expandedTo,
		NextOffset:       offset + len(profiles),
		HasMore:          hasMore,
		TotalCandidates:  total,
	}, nil
}

func (s *DiscoveryService) IncomingLikes(ctx context.Context, viewerID uuid.UUID, limit int) ([]domain.IncomingLike, error) {
	items, err := s.store.ListIncomingLikes(ctx, viewerID, limit)
	if err != nil {
		return nil, err
	}
	profiles := make([]domain.Profile, 0, len(items))
	for i := range items {
		profiles = append(profiles, items[i].Profile)
	}
	enrichProfiles(ctx, s.cache, profiles)
	s.applyPrivacy(ctx, profiles)
	for i := range items {
		items[i].Profile = profiles[i]
	}
	return items, nil
}

func (s *DiscoveryService) SentLikes(ctx context.Context, viewerID uuid.UUID, limit int) ([]domain.IncomingLike, error) {
	items, err := s.store.ListSentLikes(ctx, viewerID, limit)
	if err != nil {
		return nil, err
	}
	profiles := make([]domain.Profile, 0, len(items))
	for i := range items {
		profiles = append(profiles, items[i].Profile)
	}
	enrichProfiles(ctx, s.cache, profiles)
	s.applyPrivacy(ctx, profiles)
	for i := range items {
		items[i].Profile = profiles[i]
	}
	return items, nil
}

func (s *DiscoveryService) Stats(ctx context.Context, userID uuid.UUID) (*domain.Stats, error) {
	return s.store.GetStats(ctx, userID)
}

func (s *DiscoveryService) applyPrivacy(ctx context.Context, profiles []domain.Profile) {
	if len(profiles) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(profiles))
	for _, p := range profiles {
		ids = append(ids, p.UserID)
	}
	settings, err := s.store.SettingsForUsers(ctx, ids)
	if err != nil {
		return
	}
	for i := range profiles {
		if st, ok := settings[profiles[i].UserID]; ok {
			applyViewerVisibility(&profiles[i], &st)
		}
	}
}

func (s *DiscoveryService) NearbyCount(ctx context.Context, viewerID uuid.UUID, radius float64) (int, error) {
	viewer, err := s.store.GetProfile(ctx, viewerID)
	if err != nil {
		return 0, err
	}
	if viewer.Lat == nil || viewer.Lng == nil {
		return 0, domain.Invalid("location", "update your location first")
	}
	prefs, err := s.store.GetPreferences(ctx, viewerID)
	if err != nil {
		return 0, err
	}
	return s.store.CountCandidates(ctx, domain.DiscoveryQuery{
		ViewerID: viewerID, Lat: *viewer.Lat, Lng: *viewer.Lng,
		RadiusKM: math.Max(radius, 1), MinAge: prefs.MinAge, MaxAge: prefs.MaxAge,
		Genders: prefs.PreferredGenders,
	})
}

func (s *DiscoveryService) String() string { return fmt.Sprintf("discovery(%v)", s.store != nil) }
