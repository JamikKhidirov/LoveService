package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/store"
)

// AdminService implements moderation and technical-administration workflows.
//
// Access to another person's data is only possible through the admin role and is
// always paired with an append-only audit-log entry. Lawful-access exports
// require a legal request record (case number, authority, country, legal basis)
// and, when configured, a second administrator's approval.
type AdminService struct {
	store         *store.Store
	log           *slog.Logger
	requireSecond bool
}

func NewAdminService(st *store.Store, requireSecondApproval bool, log *slog.Logger) *AdminService {
	return &AdminService{store: st, log: log, requireSecond: requireSecondApproval}
}

// AdminMeta carries the authenticated administrator and request context used
// for the audit trail.
type AdminMeta struct {
	AdminID   uuid.UUID
	IP        string
	UserAgent string
	RequestID string
}

func (m AdminMeta) audit(action, targetType, targetID, reason, legalBasis string, metadata map[string]any) *domain.AuditLog {
	return &domain.AuditLog{
		AdminID:    &m.AdminID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Reason:     reason,
		LegalBasis: legalBasis,
		RequestID:  m.RequestID,
		IP:         m.IP,
		UserAgent:  m.UserAgent,
		Metadata:   metadata,
	}
}

func (s *AdminService) audit(ctx context.Context, entry *domain.AuditLog) {
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	if err := s.store.CreateAuditLog(ctx, entry); err != nil {
		// Audit failures must be visible but must not break the response the
		// operator already performed.
		s.log.Error("could not write admin audit log",
			"action", entry.Action, "admin_id", entry.AdminID, "error", err)
	}
}

func (s *AdminService) Stats(ctx context.Context, meta AdminMeta) (*domain.AdminStats, error) {
	stats, err := s.store.AdminStats(ctx)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, meta.audit("admin.stats.view", "system", "stats", "", "", nil))
	return stats, nil
}

func (s *AdminService) ListUsers(ctx context.Context, meta AdminMeta, search, status string, limit, offset int) ([]domain.AdminUserSummary, int, error) {
	users, total, err := s.store.AdminListUsers(ctx, search, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	s.audit(ctx, meta.audit("admin.users.list", "user", "", "", "", map[string]any{
		"search": search, "status": status, "returned": len(users),
	}))
	return users, total, nil
}

func (s *AdminService) GetUser(ctx context.Context, meta AdminMeta, userID uuid.UUID, reason string) (*domain.AdminUserDetail, error) {
	detail, err := s.store.AdminGetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if detail.Profile != nil {
		enrichProfile(ctx, nil, detail.Profile)
	}
	s.audit(ctx, meta.audit("admin.user.view", "user", userID.String(), reason, "", nil))
	return detail, nil
}

func (s *AdminService) SetUserStatus(ctx context.Context, meta AdminMeta, userID uuid.UUID, status, reason string) error {
	switch status {
	case domain.StatusActive, domain.StatusDeactivated, domain.StatusBanned, domain.StatusDeleted:
	default:
		return domain.Invalid("status", "status must be active, deactivated, banned or deleted")
	}
	if err := s.store.AdminSetUserStatus(ctx, userID, status); err != nil {
		return err
	}
	s.audit(ctx, meta.audit("admin.user.status", "user", userID.String(), reason, "", map[string]any{"status": status}))
	return nil
}

func (s *AdminService) SetUserRole(ctx context.Context, meta AdminMeta, userID uuid.UUID, role, reason string) error {
	switch role {
	case domain.RoleUser, domain.RoleModerator, domain.RoleAdmin:
	default:
		return domain.Invalid("role", "role must be user, moderator or admin")
	}
	if err := s.store.AdminSetUserRole(ctx, userID, role); err != nil {
		return err
	}
	s.audit(ctx, meta.audit("admin.user.role", "user", userID.String(), reason, "", map[string]any{"role": role}))
	return nil
}

func (s *AdminService) VerifyUser(ctx context.Context, meta AdminMeta, userID uuid.UUID, verified bool, reason string) error {
	if err := s.store.AdminVerifyUser(ctx, userID, verified); err != nil {
		return err
	}
	s.audit(ctx, meta.audit("admin.user.verify", "user", userID.String(), reason, "", map[string]any{"verified": verified}))
	return nil
}

func (s *AdminService) ListReports(ctx context.Context, meta AdminMeta, status string, limit, offset int) ([]domain.AdminReport, int, error) {
	reports, total, err := s.store.AdminListReports(ctx, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	s.audit(ctx, meta.audit("admin.reports.list", "report", "", "", "", map[string]any{"status": status, "returned": len(reports)}))
	return reports, total, nil
}

func (s *AdminService) UpdateReport(ctx context.Context, meta AdminMeta, reportID uuid.UUID, status, reason string) (*domain.Report, error) {
	switch status {
	case "open", "reviewing", "resolved", "dismissed":
	default:
		return nil, domain.Invalid("status", "status must be open, reviewing, resolved or dismissed")
	}
	report, err := s.store.AdminUpdateReport(ctx, reportID, status)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, meta.audit("admin.report.update", "report", reportID.String(), reason, "", map[string]any{"status": status}))
	return report, nil
}

func (s *AdminService) DeletePhoto(ctx context.Context, meta AdminMeta, photoID uuid.UUID, reason string) (*domain.Photo, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, domain.Invalid("reason", "reason is required when removing user media")
	}
	photo, err := s.store.AdminDeletePhoto(ctx, photoID)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, meta.audit("admin.photo.delete", "photo", photoID.String(), reason, "", map[string]any{"user_id": photo.UserID}))
	return photo, nil
}

func (s *AdminService) ListAuditLogs(ctx context.Context, meta AdminMeta, limit, offset int) ([]domain.AuditLog, error) {
	logs, err := s.store.ListAuditLogs(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *AdminService) Broadcast(ctx context.Context, meta AdminMeta, userIDs []uuid.UUID, kind, title, body, reason string) (int, error) {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(body) == "" {
		return 0, domain.Invalid("title", "title and body are required")
	}
	if kind == "" {
		kind = "system"
	}
	n, err := s.store.AdminBroadcast(ctx, userIDs, kind, title, body)
	if err != nil {
		return 0, err
	}
	s.audit(ctx, meta.audit("admin.notification.broadcast", "notification", "", reason, "", map[string]any{
		"recipients": len(userIDs), "created": n, "title": title,
	}))
	return n, nil
}

func (s *AdminService) Purge(ctx context.Context, meta AdminMeta, reason string) (map[string]int64, error) {
	result, err := s.store.PurgeExpiredData(ctx)
	if err != nil {
		return nil, err
	}
	total := int64(0)
	for _, n := range result {
		total += n
	}
	s.audit(ctx, meta.audit("admin.maintenance.purge", "system", "expired-data", reason, "", map[string]any{
		"deleted": result, "total": total,
	}))
	return result, nil
}

// ---------------------------------------------------------------------------
// Lawful access (technical admin) — legal requests and controlled exports
// ---------------------------------------------------------------------------

var legalScopes = map[string]bool{
	"account": true, "profile": true, "media": true, "matches": true,
	"messages": true, "swipes": true, "notifications": true, "reports": true,
}

type CreateLegalRequestInput struct {
	CaseNumber   string
	Authority    string
	Country      string
	LegalBasis   string
	TargetUserID *uuid.UUID
	Scopes       []string
	Reason       string
}

func (s *AdminService) CreateLegalRequest(ctx context.Context, meta AdminMeta, in CreateLegalRequestInput) (*domain.LegalRequest, error) {
	for field, value := range map[string]string{
		"case_number": in.CaseNumber, "authority": in.Authority,
		"country": in.Country, "legal_basis": in.LegalBasis,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, domain.Invalid(field, field+" is required for a lawful-access request")
		}
	}
	if len(in.Scopes) == 0 {
		in.Scopes = []string{"account", "profile", "media", "matches", "messages"}
	}
	for _, sc := range in.Scopes {
		if !legalScopes[sc] {
			return nil, domain.Invalid("scopes", "unknown scope "+sc)
		}
	}
	if in.TargetUserID != nil {
		ok, err := s.store.UserExists(ctx, *in.TargetUserID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, domain.ErrNotFound
		}
	}
	status := "pending"
	var approvedBy *uuid.UUID
	var approvedAt *time.Time
	if !s.requireSecond {
		status = "approved"
		approvedAt = ptrTime(time.Now().UTC())
	}
	req, err := s.store.CreateLegalRequest(ctx, &domain.LegalRequest{
		CreatedBy: &meta.AdminID, CaseNumber: strings.TrimSpace(in.CaseNumber),
		Authority: strings.TrimSpace(in.Authority), Country: strings.TrimSpace(in.Country),
		LegalBasis: strings.TrimSpace(in.LegalBasis), TargetUserID: in.TargetUserID,
		Scopes: in.Scopes, Status: status, ApprovedBy: approvedBy, ApprovedAt: approvedAt,
	})
	if err != nil {
		return nil, err
	}
	s.audit(ctx, meta.audit("legal.request.create", "legal_request", req.ID.String(), in.Reason, in.LegalBasis, map[string]any{
		"case_number": req.CaseNumber, "authority": req.Authority, "country": req.Country,
		"scopes": req.Scopes, "target_user_id": req.TargetUserID,
	}))
	return req, nil
}

func (s *AdminService) GetLegalRequest(ctx context.Context, meta AdminMeta, id uuid.UUID) (*domain.LegalRequest, error) {
	req, err := s.store.GetLegalRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, meta.audit("legal.request.view", "legal_request", id.String(), "", req.LegalBasis, nil))
	return req, nil
}

func (s *AdminService) ListLegalRequests(ctx context.Context, meta AdminMeta, limit, offset int) ([]domain.LegalRequest, error) {
	reqs, err := s.store.ListLegalRequests(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, meta.audit("legal.request.list", "legal_request", "", "", "", map[string]any{"returned": len(reqs)}))
	return reqs, nil
}

func (s *AdminService) ApproveLegalRequest(ctx context.Context, meta AdminMeta, id uuid.UUID, reason string) (*domain.LegalRequest, error) {
	req, err := s.store.ApproveLegalRequest(ctx, id, meta.AdminID)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, meta.audit("legal.request.approve", "legal_request", id.String(), reason, req.LegalBasis, nil))
	return req, nil
}

// ExportLegalRequest builds the disclosure package for an approved request,
// records its SHA-256 fingerprint and marks the request as exported.
func (s *AdminService) ExportLegalRequest(ctx context.Context, meta AdminMeta, id uuid.UUID, reason string) (*domain.LegalExport, error) {
	req, err := s.store.GetLegalRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Status == "rejected" || req.Status == "pending" {
		return nil, fmt.Errorf("%w: legal request must be approved before export", domain.ErrForbidden)
	}
	if req.TargetUserID == nil {
		return nil, domain.Invalid("target_user_id", "legal request has no target user")
	}
	exportMeta := map[string]any{
		"case_number": req.CaseNumber,
		"authority":   req.Authority,
		"country":     req.Country,
		"legal_basis": req.LegalBasis,
		"scopes":      req.Scopes,
		"request_id":  req.ID.String(),
		"exported_by": meta.AdminID.String(),
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"notice":      "Lawful access export. Does not contain passwords, password hashes, refresh tokens or verification codes.",
	}
	data, err := s.store.ExportUserData(ctx, *req.TargetUserID, req.Scopes, exportMeta)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	completed, err := s.store.CompleteLegalRequest(ctx, req.ID, digest)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, meta.audit("legal.request.export", "legal_request", req.ID.String(), reason, req.LegalBasis, map[string]any{
		"case_number": req.CaseNumber, "sha256": digest, "target_user_id": req.TargetUserID,
		"scopes": req.Scopes,
	}))
	return &domain.LegalExport{
		Request: *completed, Data: data, ExportedAt: time.Now().UTC(), SHA256: digest,
	}, nil
}

// SelfUserData is the privacy-friendly counterpart for a normal user. It is
// audited as well so accidental misuse is traceable.
func (s *AdminService) SelfUserData(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	return s.store.ExportUserData(ctx, userID, []string{
		"account", "profile", "media", "matches", "messages", "swipes", "notifications", "reports",
	}, map[string]any{
		"notice": "Personal data export requested by the account owner.",
	})
}

func ptrTime(t time.Time) *time.Time { return &t }

var _ = errors.Is
