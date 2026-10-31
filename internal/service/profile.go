package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/cache"
	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/storage"
	"github.com/loveservice/loveservice/internal/store"
)

type ProfileService struct {
	store       *store.Store
	cache       *cache.Cache
	storage     storage.Storage
	log         *slog.Logger
	maxUpload   int64
	autoApprove bool
}

func NewProfileService(st *store.Store, c *cache.Cache, media storage.Storage, maxUploadMB int64, autoApprove bool, log *slog.Logger) *ProfileService {
	return &ProfileService{
		store: st, cache: c, storage: media,
		log: log, maxUpload: maxUploadMB << 20, autoApprove: autoApprove,
	}
}

type UpdateProfileInput struct {
	DisplayName *string
	BirthDate   *string
	Gender      *string
	Bio         *string
	City        *string
	Country     *string
	HeightCM    *int
	Job         *string
	Education   *string
	Interests   *[]string
	LookingFor  *[]string
	Phone       *string
}

func (s *ProfileService) GetMe(ctx context.Context, userID uuid.UUID) (*domain.Profile, error) {
	p, err := s.store.GetProfile(ctx, userID)
	if err != nil {
		return nil, err
	}
	enrichProfile(ctx, s.cache, p)
	if s.cache != nil {
		p.Online = s.cache.IsOnline(ctx, userID)
	}
	return p, nil
}

func (s *ProfileService) GetUser(ctx context.Context, viewerID, targetID uuid.UUID) (*domain.Profile, error) {
	p, err := s.store.TargetProfileVisible(ctx, viewerID, targetID)
	if err != nil {
		return nil, err
	}
	// Profile statistics: one view per viewer/target/day. Failures must never
	// break the profile response.
	_ = s.store.RecordProfileView(ctx, viewerID, targetID)
	enrichProfile(ctx, s.cache, p)
	if s.cache != nil {
		p.Online = s.cache.IsOnline(ctx, targetID)
	}
	settings, err := s.store.GetSettings(ctx, targetID)
	if viewer, verr := s.store.GetProfile(ctx, viewerID); verr == nil && viewer.Lat != nil && viewer.Lng != nil && p.Lat != nil && p.Lng != nil {
		d := domain.Haversine(*viewer.Lat, *viewer.Lng, *p.Lat, *p.Lng)
		p.DistanceKM = &d
	}
	if err == nil {
		applyViewerVisibility(p, settings)
	}
	return p, nil
}

func (s *ProfileService) UpdateMe(ctx context.Context, userID uuid.UUID, in UpdateProfileInput) (*domain.Profile, error) {
	patch := store.ProfilePatch{}
	if in.DisplayName != nil {
		if err := security.ValidateDisplayName(*in.DisplayName); err != nil {
			return nil, err
		}
		patch.DisplayName = in.DisplayName
	}
	if in.BirthDate != nil {
		birth, err := security.ValidateDateLayout(*in.BirthDate)
		if err != nil {
			return nil, err
		}
		if err := security.ValidateBirthDate(birth); err != nil {
			return nil, err
		}
		patch.BirthDate = &birth
	}
	if in.Gender != nil {
		g := normalizeGender(*in.Gender)
		if err := security.ValidateGender(g); err != nil {
			return nil, err
		}
		patch.Gender = &g
	}
	if in.Bio != nil {
		if err := security.ValidateBio(*in.Bio); err != nil {
			return nil, err
		}
		bio := strings.TrimSpace(*in.Bio)
		patch.Bio = &bio
	}
	if in.City != nil {
		v := strings.TrimSpace(*in.City)
		if len([]rune(v)) > 80 {
			return nil, domain.Invalid("city", "city must be at most 80 characters")
		}
		patch.City = &v
	}
	if in.Country != nil {
		v := strings.TrimSpace(*in.Country)
		if len([]rune(v)) > 80 {
			return nil, domain.Invalid("country", "country must be at most 80 characters")
		}
		patch.Country = &v
	}
	if in.HeightCM != nil {
		if *in.HeightCM != 0 && (*in.HeightCM < 100 || *in.HeightCM > 250) {
			return nil, domain.Invalid("height_cm", "height must be between 100 and 250 cm")
		}
		patch.HeightCM = in.HeightCM
	}
	if in.Job != nil {
		v := strings.TrimSpace(*in.Job)
		if len([]rune(v)) > 100 {
			return nil, domain.Invalid("job", "job must be at most 100 characters")
		}
		patch.Job = &v
	}
	if in.Education != nil {
		v := strings.TrimSpace(*in.Education)
		if len([]rune(v)) > 100 {
			return nil, domain.Invalid("education", "education must be at most 100 characters")
		}
		patch.Education = &v
	}
	if in.Interests != nil {
		items, err := security.ValidateInterests(*in.Interests)
		if err != nil {
			return nil, err
		}
		patch.Interests = &items
	}
	if in.LookingFor != nil {
		items := make([]string, 0, len(*in.LookingFor))
		for _, g := range *in.LookingFor {
			g = normalizeGender(g)
			if err := security.ValidateGender(g); err != nil {
				return nil, domain.Invalid("looking_for", "looking_for must contain male, female or other")
			}
			items = append(items, g)
		}
		patch.LookingFor = &items
	}
	if in.Phone != nil {
		phone := strings.TrimSpace(*in.Phone)
		if err := security.ValidatePhone(phone); err != nil {
			return nil, err
		}
		var phonePtr *string
		if phone != "" {
			phonePtr = &phone
		}
		if err := s.store.UpdatePhone(ctx, userID, phonePtr); err != nil {
			return nil, err
		}
	}
	profile, err := s.store.UpdateProfile(ctx, userID, patch)
	if err != nil {
		return nil, err
	}
	enrichProfile(ctx, s.cache, profile)
	return profile, nil
}

func (s *ProfileService) UpdateLocation(ctx context.Context, userID uuid.UUID, lat, lng float64) (*domain.Profile, error) {
	if err := security.ValidateCoordinates(lat, lng); err != nil {
		return nil, err
	}
	p, err := s.store.UpdateLocation(ctx, userID, lat, lng)
	if err != nil {
		return nil, err
	}
	enrichProfile(ctx, s.cache, p)
	return p, nil
}

type UpdatePreferencesInput struct {
	MinAge           *int
	MaxAge           *int
	MaxDistanceKM    *int
	PreferredGenders *[]string
	GlobalMode       *bool
}

func (s *ProfileService) GetPreferences(ctx context.Context, userID uuid.UUID) (*domain.Preferences, error) {
	return s.store.GetPreferences(ctx, userID)
}

func (s *ProfileService) UpdatePreferences(ctx context.Context, userID uuid.UUID, in UpdatePreferencesInput) (*domain.Preferences, error) {
	prefs, err := s.store.GetPreferences(ctx, userID)
	if err != nil {
		return nil, err
	}
	if in.MinAge != nil {
		prefs.MinAge = *in.MinAge
	}
	if in.MaxAge != nil {
		prefs.MaxAge = *in.MaxAge
	}
	if in.MaxDistanceKM != nil {
		prefs.MaxDistanceKM = *in.MaxDistanceKM
	}
	if in.GlobalMode != nil {
		prefs.GlobalMode = *in.GlobalMode
	}
	if in.PreferredGenders != nil {
		genders, err := normalizeGenders(*in.PreferredGenders, "")
		if err != nil {
			return nil, err
		}
		prefs.PreferredGenders = genders
	}
	if prefs.MinAge < 18 {
		return nil, domain.Invalid("min_age", "min_age must be at least 18")
	}
	if prefs.MaxAge > 120 || prefs.MaxAge < prefs.MinAge {
		return nil, domain.Invalid("max_age", "max_age must be between min_age and 120")
	}
	if prefs.MaxDistanceKM < 1 || prefs.MaxDistanceKM > 20000 {
		return nil, domain.Invalid("max_distance_km", "max_distance_km must be between 1 and 20000")
	}
	return s.store.UpsertPreferences(ctx, prefs)
}

type UpdateSettingsInput struct {
	NotificationsEnabled *bool
	Discoverable         *bool
	ShowDistance         *bool
	ShowOnline           *bool
	Language             *string
}

func (s *ProfileService) GetSettings(ctx context.Context, userID uuid.UUID) (*domain.UserSettings, error) {
	return s.store.GetSettings(ctx, userID)
}

func (s *ProfileService) UpdateSettings(ctx context.Context, userID uuid.UUID, in UpdateSettingsInput) (*domain.UserSettings, error) {
	st, err := s.store.GetSettings(ctx, userID)
	if err != nil {
		return nil, err
	}
	if in.NotificationsEnabled != nil {
		st.NotificationsEnabled = *in.NotificationsEnabled
	}
	if in.Discoverable != nil {
		st.Discoverable = *in.Discoverable
	}
	if in.ShowDistance != nil {
		st.ShowDistance = *in.ShowDistance
	}
	if in.ShowOnline != nil {
		st.ShowOnline = *in.ShowOnline
	}
	if in.Language != nil {
		if err := security.ValidateLanguage(*in.Language); err != nil {
			return nil, err
		}
		st.Language = *in.Language
	}
	return s.store.UpsertSettings(ctx, st)
}

func (s *ProfileService) DeleteAccount(ctx context.Context, userID uuid.UUID) error {
	photos, err := s.store.ListPhotos(ctx, userID)
	if err == nil {
		for _, p := range photos {
			_ = s.storage.Delete(ctx, p.StorageKey)
		}
	}
	if err := s.store.DeleteAccount(ctx, userID); err != nil {
		return err
	}
	_ = s.store.Enqueue(ctx, "love.events", "user.deleted", userID.String(), map[string]any{"user_id": userID})
	return nil
}

func (s *ProfileService) ListMedia(ctx context.Context, userID uuid.UUID) ([]domain.Photo, error) {
	return s.store.ListPhotos(ctx, userID)
}

func (s *ProfileService) AddMedia(ctx context.Context, userID uuid.UUID, filename, contentType string, size int64, r io.Reader) (*domain.Photo, error) {
	if size <= 0 {
		return nil, domain.Invalid("file", "file is empty")
	}
	if size > s.maxUpload {
		return nil, domain.Invalid("file", fmt.Sprintf("file must be smaller than %d MB", s.maxUpload>>20))
	}
	mediaType, ext, err := detectMedia(filename, contentType)
	if err != nil {
		return nil, err
	}
	if mediaType == domain.MediaPhoto {
		n, err := s.store.CountMedia(ctx, userID, domain.MediaPhoto)
		if err != nil {
			return nil, err
		}
		if n >= 9 {
			return nil, fmt.Errorf("%w: at most 9 photos are allowed", domain.ErrMediaLimitReached)
		}
	} else {
		n, err := s.store.CountMedia(ctx, userID, domain.MediaVideo)
		if err != nil {
			return nil, err
		}
		if n >= 2 {
			return nil, fmt.Errorf("%w: at most 2 videos are allowed", domain.ErrMediaLimitReached)
		}
	}

	key := fmt.Sprintf("%s/%s%s", userID.String(), uuid.NewString(), ext)
	url, err := s.storage.Put(ctx, key, r, size, contentType)
	if err != nil {
		return nil, fmt.Errorf("store media: %w", err)
	}
	status := domain.ModerationPending
	if s.autoApprove {
		status = domain.ModerationApproved
	}
	photo := &domain.Photo{
		UserID:           userID,
		URL:              url,
		StorageKey:       key,
		MediaType:        mediaType,
		ModerationStatus: status,
		IsPrimary:        mediaType == domain.MediaPhoto,
	}
	saved, err := s.store.AddPhoto(ctx, photo)
	if err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, err
	}
	if mediaType == domain.MediaVideo {
		saved.IsPrimary = false
	}
	return saved, nil
}

func (s *ProfileService) DeleteMedia(ctx context.Context, userID, mediaID uuid.UUID) error {
	photo, err := s.store.DeletePhoto(ctx, userID, mediaID)
	if err != nil {
		return err
	}
	if err := s.storage.Delete(ctx, photo.StorageKey); err != nil {
		s.log.Warn("could not delete media object", "key", photo.StorageKey, "error", err)
	}
	return nil
}

func (s *ProfileService) SetPrimaryMedia(ctx context.Context, userID, mediaID uuid.UUID) (*domain.Photo, error) {
	return s.store.SetPrimaryPhoto(ctx, userID, mediaID)
}

var imageTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
}

var videoTypes = map[string]string{
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".m4v":  "video/x-m4v",
}

func detectMedia(filename, contentType string) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if expected, ok := imageTypes[ext]; ok {
		if contentType != "" && !strings.HasPrefix(contentType, "image/") {
			return "", "", domain.Invalid("file", "file extension and content type do not match")
		}
		_ = expected
		return domain.MediaPhoto, ext, nil
	}
	if _, ok := videoTypes[ext]; ok {
		if contentType != "" && !strings.HasPrefix(contentType, "video/") {
			return "", "", domain.Invalid("file", "file extension and content type do not match")
		}
		return domain.MediaVideo, ext, nil
	}
	return "", "", domain.Invalid("file", "allowed formats: jpg, jpeg, png, webp, gif, mp4, webm, mov")
}

func (s *ProfileService) UpdateAvatarFromSeed(ctx context.Context, userID uuid.UUID, key string) (*domain.Photo, error) {
	return nil, nil
}
