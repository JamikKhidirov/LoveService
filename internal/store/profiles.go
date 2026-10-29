package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

const profileFields = `p.user_id, p.display_name, p.birth_date, p.gender, p.bio, p.city, p.country,
       p.lat, p.lng, p.location_updated_at, p.height_cm, p.job, p.education,
       p.interests, p.looking_for, p.is_verified, p.last_seen_at, p.created_at, p.updated_at,
       COALESCE((
           SELECT jsonb_agg(to_jsonb(ph) ORDER BY ph.position, ph.created_at)
           FROM photos ph
           WHERE ph.user_id = p.user_id AND ph.moderation_status <> 'rejected'
       ), '[]'::jsonb) AS photos`

const profileSelect = `SELECT ` + profileFields + ` FROM profiles p`

func scanProfile(row pgx.Row) (*domain.Profile, error) {
	return scanProfileAnd(row)
}

// scanProfileAnd scans the standard profileFields list plus optional trailing
// columns (distance, total, swipe metadata, ...).
func scanProfileAnd(row pgx.Row, extra ...any) (*domain.Profile, error) {
	var p domain.Profile
	var photosJSON []byte
	dest := []any{
		&p.UserID, &p.DisplayName, &p.BirthDate, &p.Gender, &p.Bio, &p.City, &p.Country,
		&p.Lat, &p.Lng, &p.LocationUpdatedAt, &p.HeightCM, &p.Job, &p.Education,
		&p.Interests, &p.LookingFor, &p.IsVerified, &p.LastSeenAt, &p.CreatedAt, &p.UpdatedAt,
		&photosJSON,
	}
	dest = append(dest, extra...)
	err := row.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(photosJSON) > 0 {
		if err := json.Unmarshal(photosJSON, &p.Photos); err != nil {
			return nil, fmt.Errorf("decode photos: %w", err)
		}
	}
	if p.Photos == nil {
		p.Photos = []domain.Photo{}
	}
	if p.Interests == nil {
		p.Interests = []string{}
	}
	if p.LookingFor == nil {
		p.LookingFor = []string{}
	}
	return &p, nil
}

func (s *Store) GetProfile(ctx context.Context, userID uuid.UUID) (*domain.Profile, error) {
	return scanProfile(s.pool.QueryRow(ctx, profileSelect+` WHERE p.user_id = $1`, userID))
}

func (s *Store) GetProfileWithUser(ctx context.Context, userID uuid.UUID) (*domain.Profile, *domain.User, error) {
	profile, err := s.GetProfile(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return profile, user, nil
}

type ProfilePatch struct {
	DisplayName *string
	BirthDate   *time.Time
	Gender      *string
	Bio         *string
	City        *string
	Country     *string
	HeightCM    *int
	Job         *string
	Education   *string
	Interests   *[]string
	LookingFor  *[]string
	Verified    *bool
}

func (s *Store) UpdateProfile(ctx context.Context, userID uuid.UUID, patch ProfilePatch) (*domain.Profile, error) {
	sets := make([]string, 0, 12)
	args := make([]any, 0, 12)
	args = append(args, userID)
	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if patch.DisplayName != nil {
		add("display_name", *patch.DisplayName)
	}
	if patch.BirthDate != nil {
		add("birth_date", *patch.BirthDate)
	}
	if patch.Gender != nil {
		add("gender", *patch.Gender)
	}
	if patch.Bio != nil {
		add("bio", *patch.Bio)
	}
	if patch.City != nil {
		add("city", *patch.City)
	}
	if patch.Country != nil {
		add("country", *patch.Country)
	}
	if patch.HeightCM != nil {
		if *patch.HeightCM == 0 {
			sets = append(sets, "height_cm = NULL")
		} else {
			add("height_cm", *patch.HeightCM)
		}
	}
	if patch.Job != nil {
		add("job", *patch.Job)
	}
	if patch.Education != nil {
		add("education", *patch.Education)
	}
	if patch.Interests != nil {
		add("interests", *patch.Interests)
	}
	if patch.LookingFor != nil {
		add("looking_for", *patch.LookingFor)
	}
	if patch.Verified != nil {
		add("is_verified", *patch.Verified)
	}
	if len(sets) == 0 {
		return s.GetProfile(ctx, userID)
	}
	sets = append(sets, "updated_at = now()")
	query := `UPDATE profiles SET ` + strings.Join(sets, ", ") + ` WHERE user_id = $1`
	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrNotFound
	}
	return s.GetProfile(ctx, userID)
}

func (s *Store) UpdateLocation(ctx context.Context, userID uuid.UUID, lat, lng float64) (*domain.Profile, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE profiles
		SET lat = $2, lng = $3, location_updated_at = now(), updated_at = now()
		WHERE user_id = $1`, userID, lat, lng)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrNotFound
	}
	return s.GetProfile(ctx, userID)
}

func (s *Store) GetPreferences(ctx context.Context, userID uuid.UUID) (*domain.Preferences, error) {
	var p domain.Preferences
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, min_age, max_age, max_distance_km, preferred_genders, global_mode, updated_at
		FROM preferences WHERE user_id = $1`, userID,
	).Scan(&p.UserID, &p.MinAge, &p.MaxAge, &p.MaxDistanceKM, &p.PreferredGenders, &p.GlobalMode, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) UpsertPreferences(ctx context.Context, p *domain.Preferences) (*domain.Preferences, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO preferences (user_id, min_age, max_age, max_distance_km, preferred_genders, global_mode, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (user_id) DO UPDATE SET
			min_age = EXCLUDED.min_age,
			max_age = EXCLUDED.max_age,
			max_distance_km = EXCLUDED.max_distance_km,
			preferred_genders = EXCLUDED.preferred_genders,
			global_mode = EXCLUDED.global_mode,
			updated_at = now()
		RETURNING user_id, min_age, max_age, max_distance_km, preferred_genders, global_mode, updated_at`,
		p.UserID, p.MinAge, p.MaxAge, p.MaxDistanceKM, p.PreferredGenders, p.GlobalMode,
	).Scan(&p.UserID, &p.MinAge, &p.MaxAge, &p.MaxDistanceKM, &p.PreferredGenders, &p.GlobalMode, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) GetSettings(ctx context.Context, userID uuid.UUID) (*domain.UserSettings, error) {
	var st domain.UserSettings
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, notifications_enabled, discoverable, show_distance, show_online, language, updated_at
		FROM user_settings WHERE user_id = $1`, userID,
	).Scan(&st.UserID, &st.NotificationsEnabled, &st.Discoverable, &st.ShowDistance, &st.ShowOnline, &st.Language, &st.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *Store) UpsertSettings(ctx context.Context, st *domain.UserSettings) (*domain.UserSettings, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO user_settings (user_id, notifications_enabled, discoverable, show_distance, show_online, language, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (user_id) DO UPDATE SET
			notifications_enabled = EXCLUDED.notifications_enabled,
			discoverable = EXCLUDED.discoverable,
			show_distance = EXCLUDED.show_distance,
			show_online = EXCLUDED.show_online,
			language = EXCLUDED.language,
			updated_at = now()
		RETURNING user_id, notifications_enabled, discoverable, show_distance, show_online, language, updated_at`,
		st.UserID, st.NotificationsEnabled, st.Discoverable, st.ShowDistance, st.ShowOnline, st.Language,
	).Scan(&st.UserID, &st.NotificationsEnabled, &st.Discoverable, &st.ShowDistance, &st.ShowOnline, &st.Language, &st.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) CountMedia(ctx context.Context, userID uuid.UUID, mediaType string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM photos WHERE user_id = $1 AND media_type = $2`, userID, mediaType).Scan(&n)
	return n, err
}

func (s *Store) ListPhotos(ctx context.Context, userID uuid.UUID) ([]domain.Photo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, url, storage_key, media_type, position, is_primary, moderation_status, created_at
		FROM photos WHERE user_id = $1 ORDER BY position, created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Photo, 0, 9)
	for rows.Next() {
		var p domain.Photo
		if err := rows.Scan(&p.ID, &p.UserID, &p.URL, &p.StorageKey, &p.MediaType, &p.Position, &p.IsPrimary, &p.ModerationStatus, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) AddPhoto(ctx context.Context, p *domain.Photo) (*domain.Photo, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var maxPos *int
	if err := tx.QueryRow(ctx, `SELECT max(position) FROM photos WHERE user_id = $1`, p.UserID).Scan(&maxPos); err != nil {
		return nil, err
	}
	p.Position = 0
	if maxPos != nil {
		p.Position = *maxPos + 1
	}
	if p.IsPrimary {
		if _, err := tx.Exec(ctx, `UPDATE photos SET is_primary = false WHERE user_id = $1`, p.UserID); err != nil {
			return nil, err
		}
	} else if maxPos == nil && p.MediaType == domain.MediaPhoto {
		p.IsPrimary = true
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO photos (user_id, url, storage_key, media_type, position, is_primary, moderation_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, position, is_primary, created_at`,
		p.UserID, p.URL, p.StorageKey, p.MediaType, p.Position, p.IsPrimary, p.ModerationStatus,
	).Scan(&p.ID, &p.Position, &p.IsPrimary, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) GetPhoto(ctx context.Context, userID, photoID uuid.UUID) (*domain.Photo, error) {
	var p domain.Photo
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, url, storage_key, media_type, position, is_primary, moderation_status, created_at
		FROM photos WHERE id = $1 AND user_id = $2`, photoID, userID,
	).Scan(&p.ID, &p.UserID, &p.URL, &p.StorageKey, &p.MediaType, &p.Position, &p.IsPrimary, &p.ModerationStatus, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) DeletePhoto(ctx context.Context, userID, photoID uuid.UUID) (*domain.Photo, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	photo, err := s.getPhotoTx(ctx, tx, userID, photoID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM photos WHERE id = $1`, photoID); err != nil {
		return nil, err
	}
	if photo.IsPrimary {
		if _, err := tx.Exec(ctx, `
			UPDATE photos SET is_primary = true
			WHERE id = (SELECT id FROM photos WHERE user_id = $1 AND media_type = 'photo' ORDER BY position LIMIT 1)`,
			userID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return photo, nil
}

func (s *Store) getPhotoTx(ctx context.Context, tx pgx.Tx, userID, photoID uuid.UUID) (*domain.Photo, error) {
	var p domain.Photo
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, url, storage_key, media_type, position, is_primary, moderation_status, created_at
		FROM photos WHERE id = $1 AND user_id = $2`, photoID, userID,
	).Scan(&p.ID, &p.UserID, &p.URL, &p.StorageKey, &p.MediaType, &p.Position, &p.IsPrimary, &p.ModerationStatus, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return &p, err
}

func (s *Store) SetPrimaryPhoto(ctx context.Context, userID, photoID uuid.UUID) (*domain.Photo, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	photo, err := s.getPhotoTx(ctx, tx, userID, photoID)
	if err != nil {
		return nil, err
	}
	if photo.MediaType != domain.MediaPhoto {
		return nil, domain.Invalid("photo_id", "only photos can be primary")
	}
	if _, err := tx.Exec(ctx, `UPDATE photos SET is_primary = false WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE photos SET is_primary = true WHERE id = $1`, photoID); err != nil {
		return nil, err
	}
	photo.IsPrimary = true
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return photo, nil
}

func (s *Store) GetPhotoOwners(ctx context.Context, userID uuid.UUID) ([]domain.Photo, error) {
	return s.ListPhotos(ctx, userID)
}

func (s *Store) SettingsForUsers(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.UserSettings, error) {
	out := make(map[uuid.UUID]domain.UserSettings, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, notifications_enabled, discoverable, show_distance, show_online, language, updated_at
		FROM user_settings WHERE user_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st domain.UserSettings
		if err := rows.Scan(&st.UserID, &st.NotificationsEnabled, &st.Discoverable, &st.ShowDistance, &st.ShowOnline, &st.Language, &st.UpdatedAt); err != nil {
			return nil, err
		}
		out[st.UserID] = st
	}
	return out, rows.Err()
}
