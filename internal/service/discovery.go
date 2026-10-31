package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

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
	// Sort is one of balanced (default), distance or rating. "rating" orders by
	// the internal attractiveness score, "balanced" blends distance and rating.
	Sort string
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
	sortMode := strings.ToLower(strings.TrimSpace(in.Sort))
	switch sortMode {
	case domain.DiscoverySortDistance, domain.DiscoverySortRating, domain.DiscoverySortBalanced:
	case "":
		sortMode = domain.DiscoverySortBalanced
	default:
		return nil, domain.Invalid("sort", "sort must be one of balanced, distance, rating")
	}

	query := domain.DiscoveryQuery{
		ViewerID: viewerID, Lat: lat, Lng: lng,
		RadiusKM: radius, MinAge: minAge, MaxAge: maxAge,
		Genders: genders, Limit: limit, Offset: offset, Sort: sortMode,
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
				Genders: genders, Limit: limit, Offset: 0, Sort: sortMode,
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

// IncomingLikes implements "who liked me". Free users only receive the counter
// and a paywall marker; premium members and users who bought a 24h unlock get
// the actual profiles.
func (s *DiscoveryService) IncomingLikes(ctx context.Context, viewerID uuid.UUID, limit int) (*domain.IncomingLikesResult, error) {
	user, err := s.store.GetUserByID(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	count, err := s.store.CountIncomingLikes(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	result := &domain.IncomingLikesResult{
		Items:              []domain.IncomingLike{},
		Count:              count,
		PremiumActive:      user.PremiumActive(),
		LikesUnlockedUntil: user.LikesUnlockedUntil,
		UnlockCostCoins:    UnlockLikesCostCoins,
	}
	if !user.PremiumActive() && !user.LikesUnlocked() {
		result.Locked = true
		return result, nil
	}

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
	result.Items = items
	result.Count = len(items)
	return result, nil
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

// ProfileStats returns the "impressions" panel: profile views and likes.
func (s *DiscoveryService) ProfileStats(ctx context.Context, userID uuid.UUID) (*domain.ProfileStats, error) {
	return s.store.GetProfileStats(ctx, userID)
}

// TopPicks returns 4–5 daily recommendations. The set is persisted per user and
// day, so repeated calls within a day are stable and cheap.
func (s *DiscoveryService) TopPicks(ctx context.Context, viewerID uuid.UUID) ([]domain.TopPick, error) {
	picks, err := s.store.ListTopPicks(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	if len(picks) > 0 {
		profiles := make([]domain.Profile, 0, len(picks))
		for i := range picks {
			profiles = append(profiles, picks[i].Profile)
		}
		enrichProfiles(ctx, s.cache, profiles)
		s.applyPrivacy(ctx, profiles)
		for i := range picks {
			picks[i].Profile = profiles[i]
		}
		return picks, nil
	}

	viewer, err := s.store.GetProfile(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	if viewer.Lat == nil || viewer.Lng == nil {
		return nil, domain.Invalid("location", "update your location first")
	}
	prefs, err := s.store.GetPreferences(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	candidates, _, err := s.store.Discover(ctx, domain.DiscoveryQuery{
		ViewerID: viewerID, Lat: *viewer.Lat, Lng: *viewer.Lng,
		RadiusKM: float64(prefs.MaxDistanceKM), MinAge: prefs.MinAge, MaxAge: prefs.MaxAge,
		Genders: prefs.PreferredGenders, Limit: 30, Sort: domain.DiscoverySortBalanced,
	})
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []domain.TopPick{}, nil
	}

	scored := make([]domain.TopPick, 0, len(candidates))
	for i := range candidates {
		scored = append(scored, domain.TopPick{
			Profile:            candidates[i],
			CompatibilityScore: compatibilityScore(viewer, &candidates[i]),
			ForDate:            time.Now().UTC().Format("2006-01-02"),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].CompatibilityScore > scored[j].CompatibilityScore
	})
	limit := 5
	if len(scored) < limit {
		limit = len(scored)
	}
	if limit < 4 {
		// Still return what exists; small pools simply have fewer picks.
		limit = len(scored)
	}
	picks = scored[:limit]
	for i := range picks {
		picks[i].Rank = i + 1
	}
	if err := s.store.ReplaceTopPicks(ctx, viewerID, picks); err != nil {
		return nil, err
	}

	profiles := make([]domain.Profile, 0, len(picks))
	for i := range picks {
		profiles = append(profiles, picks[i].Profile)
	}
	enrichProfiles(ctx, s.cache, profiles)
	s.applyPrivacy(ctx, profiles)
	for i := range picks {
		picks[i].Profile = profiles[i]
	}
	return picks, nil
}

// compatibilityScore is a transparent 0..1 blend of shared interests, distance,
// verification and profile completeness. It intentionally uses no hidden data.
func compatibilityScore(viewer, candidate *domain.Profile) float64 {
	score := 0.0
	if len(viewer.Interests) > 0 && len(candidate.Interests) > 0 {
		set := make(map[string]struct{}, len(viewer.Interests))
		for _, v := range viewer.Interests {
			set[strings.ToLower(strings.TrimSpace(v))] = struct{}{}
		}
		shared := 0
		for _, v := range candidate.Interests {
			if _, ok := set[strings.ToLower(strings.TrimSpace(v))]; ok {
				shared++
			}
		}
		union := len(set) + len(candidate.Interests) - shared
		if union > 0 {
			score += 0.5 * float64(shared) / float64(union)
		}
	}
	if candidate.DistanceKM != nil {
		proximity := 1 - *candidate.DistanceKM/50
		if proximity < 0 {
			proximity = 0
		}
		score += 0.3 * proximity
	}
	if candidate.IsVerified {
		score += 0.1
	}
	if len(candidate.Photos) > 0 {
		score += 0.05
	}
	if candidate.Bio != "" {
		score += 0.05
	}
	if score > 1 {
		score = 1
	}
	return math.Round(score*1000) / 1000
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
