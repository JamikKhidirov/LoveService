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

func (s *Store) AdminStats(ctx context.Context) (*domain.AdminStats, error) {
	var st domain.AdminStats
	err := s.pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM users),
  (SELECT count(*) FROM users WHERE status = 'active'),
  (SELECT count(*) FROM users WHERE created_at >= now() - interval '24 hours'),
  (SELECT count(*) FROM matches WHERE unmatched_at IS NULL),
  (SELECT count(*) FROM messages),
  (SELECT count(*) FROM swipes),
  (SELECT count(*) FROM reports WHERE status = 'open'),
  (SELECT count(*) FROM notifications),
  (SELECT count(*) FROM legal_requests)`,
	).Scan(&st.Users, &st.ActiveUsers, &st.NewUsers24h, &st.Matches, &st.Messages, &st.Swipes, &st.OpenReports, &st.Notifications, &st.LegalRequests)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *Store) AdminListUsers(ctx context.Context, search, status string, limit, offset int) ([]domain.AdminUserSummary, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where := []string{"1 = 1"}
	args := []any{}
	if strings.TrimSpace(search) != "" {
		args = append(args, "%"+strings.TrimSpace(search)+"%")
		where = append(where, fmt.Sprintf("(u.email ILIKE $%d OR COALESCE(p.display_name, '') ILIKE $%d OR COALESCE(u.phone, '') ILIKE $%d)", len(args), len(args), len(args)))
	}
	if strings.TrimSpace(status) != "" {
		args = append(args, strings.TrimSpace(status))
		where = append(where, fmt.Sprintf("u.status = $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users u LEFT JOIN profiles p ON p.user_id = u.id WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	query := fmt.Sprintf(`
SELECT u.id, u.email, u.phone, u.role, u.status, (u.email_verified_at IS NOT NULL),
       COALESCE(p.display_name, ''), p.birth_date, COALESCE(p.gender, ''), COALESCE(p.city, ''),
       (SELECT url FROM photos ph WHERE ph.user_id = u.id AND ph.is_primary LIMIT 1),
       (SELECT count(*) FROM photos ph WHERE ph.user_id = u.id),
       (SELECT count(*) FROM matches m WHERE m.unmatched_at IS NULL AND (m.user_a = u.id OR m.user_b = u.id)),
       (SELECT count(*) FROM messages msg WHERE msg.sender_id = u.id),
       p.last_seen_at, u.created_at
FROM users u
LEFT JOIN profiles p ON p.user_id = u.id
WHERE %s
ORDER BY u.created_at DESC
LIMIT $%d OFFSET $%d`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.AdminUserSummary, 0, limit)
	for rows.Next() {
		var u domain.AdminUserSummary
		if err := rows.Scan(&u.ID, &u.Email, &u.Phone, &u.Role, &u.Status, &u.EmailVerified,
			&u.DisplayName, &u.BirthDate, &u.Gender, &u.City, &u.PrimaryPhoto,
			&u.Photos, &u.Matches, &u.Messages, &u.LastSeenAt, &u.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

func (s *Store) AdminGetUser(ctx context.Context, userID uuid.UUID) (*domain.AdminUserDetail, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	profile, err := s.GetProfile(ctx, userID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	prefs, _ := s.GetPreferences(ctx, userID)
	settings, _ := s.GetSettings(ctx, userID)
	var swipesSent, swipesReceived, matches, messagesSent, photos, reportsAgainst, notifications int
	err = s.pool.QueryRow(ctx, `
SELECT
 (SELECT count(*) FROM swipes WHERE swiper_id = $1),
 (SELECT count(*) FROM swipes WHERE target_id = $1),
 (SELECT count(*) FROM matches WHERE (user_a = $1 OR user_b = $1) AND unmatched_at IS NULL),
 (SELECT count(*) FROM messages WHERE sender_id = $1),
 (SELECT count(*) FROM photos WHERE user_id = $1),
 (SELECT count(*) FROM reports WHERE target_id = $1),
 (SELECT count(*) FROM notifications WHERE user_id = $1)`, userID,
	).Scan(&swipesSent, &swipesReceived, &matches, &messagesSent, &photos, &reportsAgainst, &notifications)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{
		"swipes_sent": swipesSent, "swipes_received": swipesReceived, "matches": matches,
		"messages_sent": messagesSent, "photos": photos, "reports_against": reportsAgainst,
		"notifications": notifications,
	}
	return &domain.AdminUserDetail{User: *user, Profile: profile, Preferences: prefs, Settings: settings, Counts: counts}, nil
}

func (s *Store) AdminSetUserStatus(ctx context.Context, userID uuid.UUID, status string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET status = $2, updated_at = now() WHERE id = $1`, userID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	if status != domain.StatusActive {
		if err := s.RevokeUserRefreshTokens(ctx, userID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AdminSetUserRole(ctx context.Context, userID uuid.UUID, role string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET role = $2, updated_at = now() WHERE id = $1`, userID, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) AdminVerifyUser(ctx context.Context, userID uuid.UUID, verified bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE profiles SET is_verified = $2, updated_at = now() WHERE user_id = $1`, userID, verified)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) AdminListReports(ctx context.Context, status string, limit, offset int) ([]domain.AdminReport, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where := "1 = 1"
	args := []any{}
	if strings.TrimSpace(status) != "" {
		args = append(args, strings.TrimSpace(status))
		where = "r.status = $1"
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM reports r WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	query := fmt.Sprintf(`
SELECT r.id, r.reporter_id, r.target_id, r.reason, r.details, r.status, r.created_at,
       ru.email, COALESCE(rp.display_name, ''), tu.email, COALESCE(tp.display_name, '')
FROM reports r
JOIN users ru ON ru.id = r.reporter_id
JOIN users tu ON tu.id = r.target_id
LEFT JOIN profiles rp ON rp.user_id = r.reporter_id
LEFT JOIN profiles tp ON tp.user_id = r.target_id
WHERE %s
ORDER BY r.created_at DESC
LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.AdminReport, 0, limit)
	for rows.Next() {
		var r domain.AdminReport
		if err := rows.Scan(&r.ID, &r.ReporterID, &r.TargetID, &r.Reason, &r.Details, &r.Status, &r.CreatedAt,
			&r.ReporterEmail, &r.ReporterName, &r.TargetEmail, &r.TargetName); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (s *Store) AdminUpdateReport(ctx context.Context, reportID uuid.UUID, status string) (*domain.Report, error) {
	var r domain.Report
	err := s.pool.QueryRow(ctx, `
		UPDATE reports SET status = $2 WHERE id = $1
		RETURNING id, reporter_id, target_id, reason, details, status, created_at`, reportID, status,
	).Scan(&r.ID, &r.ReporterID, &r.TargetID, &r.Reason, &r.Details, &r.Status, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) AdminDeletePhoto(ctx context.Context, photoID uuid.UUID) (*domain.Photo, error) {
	var p domain.Photo
	err := s.pool.QueryRow(ctx, `
		DELETE FROM photos WHERE id = $1
		RETURNING id, user_id, url, storage_key, media_type, position, is_primary, moderation_status, created_at`, photoID,
	).Scan(&p.ID, &p.UserID, &p.URL, &p.StorageKey, &p.MediaType, &p.Position, &p.IsPrimary, &p.ModerationStatus, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreateAuditLog(ctx context.Context, entry *domain.AuditLog) error {
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	meta, err := json.Marshal(entry.Metadata)
	if err != nil {
		return err
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO admin_audit_logs (admin_id, action, target_type, target_id, reason, legal_basis, request_id, ip, user_agent, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`,
		entry.AdminID, entry.Action, entry.TargetType, entry.TargetID, entry.Reason, entry.LegalBasis,
		entry.RequestID, entry.IP, entry.UserAgent, meta,
	).Scan(&entry.ID, &entry.CreatedAt)
}

func (s *Store) ListAuditLogs(ctx context.Context, limit, offset int) ([]domain.AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, admin_id, action, target_type, target_id, reason, legal_basis, request_id, ip, user_agent, metadata, created_at
		FROM admin_audit_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AuditLog, 0, limit)
	for rows.Next() {
		var l domain.AuditLog
		var meta []byte
		if err := rows.Scan(&l.ID, &l.AdminID, &l.Action, &l.TargetType, &l.TargetID, &l.Reason, &l.LegalBasis,
			&l.RequestID, &l.IP, &l.UserAgent, &meta, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Metadata = map[string]any{}
		_ = json.Unmarshal(meta, &l.Metadata)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) CreateLegalRequest(ctx context.Context, r *domain.LegalRequest) (*domain.LegalRequest, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO legal_requests (created_by, case_number, authority, country, legal_basis, target_user_id, scopes, status, approved_by, approved_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, NULL)
		RETURNING id, created_by, approved_by, case_number, authority, country, legal_basis, target_user_id,
		          scopes, status, export_sha256, created_at, approved_at, completed_at`,
		r.CreatedBy, r.CaseNumber, r.Authority, r.Country, r.LegalBasis, r.TargetUserID, r.Scopes, r.Status,
	).Scan(&r.ID, &r.CreatedBy, &r.ApprovedBy, &r.CaseNumber, &r.Authority, &r.Country, &r.LegalBasis, &r.TargetUserID,
		&r.Scopes, &r.Status, &r.ExportSHA256, &r.CreatedAt, &r.ApprovedAt, &r.CompletedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) GetLegalRequest(ctx context.Context, id uuid.UUID) (*domain.LegalRequest, error) {
	var r domain.LegalRequest
	err := s.pool.QueryRow(ctx, `
		SELECT id, created_by, approved_by, case_number, authority, country, legal_basis, target_user_id,
		       scopes, status, export_sha256, created_at, approved_at, completed_at
		FROM legal_requests WHERE id = $1`, id,
	).Scan(&r.ID, &r.CreatedBy, &r.ApprovedBy, &r.CaseNumber, &r.Authority, &r.Country, &r.LegalBasis, &r.TargetUserID,
		&r.Scopes, &r.Status, &r.ExportSHA256, &r.CreatedAt, &r.ApprovedAt, &r.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) ListLegalRequests(ctx context.Context, limit, offset int) ([]domain.LegalRequest, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, created_by, approved_by, case_number, authority, country, legal_basis, target_user_id,
		       scopes, status, export_sha256, created_at, approved_at, completed_at
		FROM legal_requests ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.LegalRequest, 0, limit)
	for rows.Next() {
		var r domain.LegalRequest
		if err := rows.Scan(&r.ID, &r.CreatedBy, &r.ApprovedBy, &r.CaseNumber, &r.Authority, &r.Country, &r.LegalBasis, &r.TargetUserID,
			&r.Scopes, &r.Status, &r.ExportSHA256, &r.CreatedAt, &r.ApprovedAt, &r.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ApproveLegalRequest(ctx context.Context, id, adminID uuid.UUID) (*domain.LegalRequest, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE legal_requests SET approved_by = $2, approved_at = now(), status = 'approved'
		WHERE id = $1 AND status = 'pending' AND (created_by IS NULL OR created_by <> $2)`, id, adminID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrNotFound
	}
	return s.GetLegalRequest(ctx, id)
}

func (s *Store) CompleteLegalRequest(ctx context.Context, id uuid.UUID, sha string) (*domain.LegalRequest, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE legal_requests SET status = 'exported', export_sha256 = $2, completed_at = now()
		WHERE id = $1 AND status IN ('approved', 'exported')`, id, sha)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrNotFound
	}
	return s.GetLegalRequest(ctx, id)
}

// ExportUserData assembles a lawful-access export. Passwords, password hashes,
// refresh tokens and verification codes are never included.
func (s *Store) ExportUserData(ctx context.Context, userID uuid.UUID, scopes []string, meta map[string]any) (map[string]any, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, sc := range scopes {
		want[sc] = true
	}
	out := map[string]any{
		"export_metadata": meta,
		"account": map[string]any{
			"id": user.ID, "email": user.Email, "phone": user.Phone, "role": user.Role,
			"status": user.Status, "email_verified_at": user.EmailVerifiedAt,
			"created_at": user.CreatedAt, "updated_at": user.UpdatedAt,
		},
	}
	if want["profile"] {
		if profile, err := s.GetProfile(ctx, userID); err == nil {
			out["profile"] = map[string]any{
				"user_id": profile.UserID, "display_name": profile.DisplayName,
				"birth_date": profile.BirthDate.Format("2006-01-02"), "gender": profile.Gender,
				"bio": profile.Bio, "city": profile.City, "country": profile.Country,
				"lat": profile.Lat, "lng": profile.Lng, "location_updated_at": profile.LocationUpdatedAt,
				"height_cm": profile.HeightCM, "job": profile.Job, "education": profile.Education,
				"interests": profile.Interests, "looking_for": profile.LookingFor,
				"is_verified": profile.IsVerified, "last_seen_at": profile.LastSeenAt,
				"created_at": profile.CreatedAt, "updated_at": profile.UpdatedAt,
			}
			if prefs, err := s.GetPreferences(ctx, userID); err == nil {
				out["preferences"] = prefs
			}
			if settings, err := s.GetSettings(ctx, userID); err == nil {
				out["settings"] = settings
			}
		}
	}
	if want["media"] {
		photos, err := s.ListPhotos(ctx, userID)
		if err != nil {
			return nil, err
		}
		out["media"] = photos
	}
	if want["matches"] {
		matches, err := s.ListMatches(ctx, userID)
		if err != nil {
			return nil, err
		}
		out["matches"] = matches
	}
	if want["messages"] {
		rows, err := s.pool.Query(ctx, `
			SELECT msg.id, msg.conversation_id, msg.sender_id, msg.body, msg.type, msg.media_url, msg.read_at, msg.created_at
			FROM messages msg
			JOIN conversations c ON c.id = msg.conversation_id
			JOIN matches m ON m.id = c.match_id
			WHERE m.user_a = $1 OR m.user_b = $1
			ORDER BY msg.created_at ASC
			LIMIT 10000`, userID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		messages := make([]domain.Message, 0)
		for rows.Next() {
			var m domain.Message
			if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.Body, &m.Type, &m.MediaURL, &m.ReadAt, &m.CreatedAt); err != nil {
				return nil, err
			}
			messages = append(messages, m)
		}
		out["messages"] = messages
	}
	if want["swipes"] {
		sent, _ := s.ListSentLikes(ctx, userID, 100)
		incoming, _ := s.ListIncomingLikes(ctx, userID, 100)
		out["swipes"] = map[string]any{"sent_likes": sent, "incoming_likes": incoming}
	}
	if want["notifications"] {
		notifications, err := s.ListNotifications(ctx, userID, nil, 100)
		if err == nil {
			out["notifications"] = notifications
		}
	}
	if want["reports"] {
		rows, err := s.pool.Query(ctx, `
			SELECT id, reporter_id, target_id, reason, details, status, created_at
			FROM reports WHERE reporter_id = $1 OR target_id = $1 ORDER BY created_at DESC LIMIT 200`, userID)
		if err == nil {
			defer rows.Close()
			reports := []domain.Report{}
			for rows.Next() {
				var r domain.Report
				if err := rows.Scan(&r.ID, &r.ReporterID, &r.TargetID, &r.Reason, &r.Details, &r.Status, &r.CreatedAt); err != nil {
					break
				}
				reports = append(reports, r)
			}
			out["reports"] = reports
		}
	}
	return out, nil
}

func (s *Store) AdminBroadcast(ctx context.Context, userIDs []uuid.UUID, kind, title, body string) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	data, _ := json.Marshal(map[string]any{"screen": "notifications", "broadcast": true})
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO notifications (user_id, type, title, body, data)
		SELECT unnest($1::uuid[]), $2, $3, $4, $5`, userIDs, kind, title, body, data)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *Store) PurgeExpiredData(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	if tag, err := s.pool.Exec(ctx, `DELETE FROM refresh_tokens WHERE expires_at < now() - interval '7 days'`); err == nil {
		out["refresh_tokens"] = tag.RowsAffected()
	}
	if tag, err := s.pool.Exec(ctx, `DELETE FROM email_codes WHERE expires_at < now() - interval '1 day'`); err == nil {
		out["email_codes"] = tag.RowsAffected()
	}
	if tag, err := s.pool.Exec(ctx, `DELETE FROM outbox_events WHERE published_at IS NOT NULL AND published_at < now() - interval '7 days'`); err == nil {
		out["outbox_events"] = tag.RowsAffected()
	}
	return out, nil
}

func (s *Store) AuditLogCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM admin_audit_logs`).Scan(&n)
	return n, err
}

var _ = time.Now
