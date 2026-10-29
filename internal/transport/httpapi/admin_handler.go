package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/service"
)

// adminMeta builds the audit context for the authenticated administrator.
// Every mutating/administrative call records who did what, from where, and why.
func (a *API) adminMeta(r *http.Request) service.AdminMeta {
	return service.AdminMeta{
		AdminID:   userIDFrom(r.Context()),
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: middleware.GetReqID(r.Context()),
	}
}

func (a *API) adminStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.admin.Stats(r.Context(), a.adminMeta(r))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (a *API) adminListUsers(w http.ResponseWriter, r *http.Request) {
	limit, _, _ := intQuery(r.URL.Query().Get("limit"))
	offset, _, _ := intQuery(r.URL.Query().Get("offset"))
	users, total, err := a.admin.ListUsers(r.Context(), a.adminMeta(r),
		r.URL.Query().Get("search"), r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": users, "count": len(users), "total": total})
}

func (a *API) adminGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "userID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	detail, err := a.admin.GetUser(r.Context(), a.adminMeta(r), id, r.URL.Query().Get("reason"))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

type adminStatusRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (a *API) adminSetUserStatus(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "userID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	var req adminStatusRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, r, a.log(), domain.Invalid("reason", "reason is required for an audited status change"))
		return
	}
	if err := a.admin.SetUserStatus(r.Context(), a.adminMeta(r), id, req.Status, req.Reason); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": true, "status": req.Status})
}

type adminRoleRequest struct {
	Role   string `json:"role"`
	Reason string `json:"reason"`
}

func (a *API) adminSetUserRole(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "userID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	var req adminRoleRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, r, a.log(), domain.Invalid("reason", "reason is required for an audited role change"))
		return
	}
	if err := a.admin.SetUserRole(r.Context(), a.adminMeta(r), id, req.Role, req.Reason); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": true, "role": req.Role})
}

type adminVerifyRequest struct {
	Verified bool   `json:"verified"`
	Reason   string `json:"reason"`
}

func (a *API) adminVerifyUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "userID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	var req adminVerifyRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.admin.VerifyUser(r.Context(), a.adminMeta(r), id, req.Verified, req.Reason); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": true, "verified": req.Verified})
}

func (a *API) adminListReports(w http.ResponseWriter, r *http.Request) {
	limit, _, _ := intQuery(r.URL.Query().Get("limit"))
	offset, _, _ := intQuery(r.URL.Query().Get("offset"))
	reports, total, err := a.admin.ListReports(r.Context(), a.adminMeta(r), r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": reports, "count": len(reports), "total": total})
}

type adminReportRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (a *API) adminUpdateReport(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "reportID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	var req adminReportRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	report, err := a.admin.UpdateReport(r.Context(), a.adminMeta(r), id, req.Status, req.Reason)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (a *API) adminDeletePhoto(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "photoID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	if reason == "" {
		writeError(w, r, a.log(), domain.Invalid("reason", "reason query parameter is required"))
		return
	}
	photo, err := a.admin.DeletePhoto(r.Context(), a.adminMeta(r), id, reason)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "photo": photo})
}

func (a *API) adminAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _, _ := intQuery(r.URL.Query().Get("limit"))
	offset, _, _ := intQuery(r.URL.Query().Get("offset"))
	logs, err := a.admin.ListAuditLogs(r.Context(), a.adminMeta(r), limit, offset)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": logs, "count": len(logs)})
}

type adminBroadcastRequest struct {
	UserIDs []string `json:"user_ids"`
	Type    string   `json:"type"`
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Reason  string   `json:"reason"`
}

func (a *API) adminBroadcast(w http.ResponseWriter, r *http.Request) {
	var req adminBroadcastRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	ids := make([]uuid.UUID, 0, len(req.UserIDs))
	for _, raw := range req.UserIDs {
		id, err := parseUUIDParamValue(raw, "user_ids")
		if err != nil {
			writeError(w, r, a.log(), err)
			return
		}
		ids = append(ids, id)
	}
	created, err := a.admin.Broadcast(r.Context(), a.adminMeta(r), ids, req.Type, req.Title, req.Body, req.Reason)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"created": created})
}

func (a *API) adminPurge(w http.ResponseWriter, r *http.Request) {
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	if reason == "" {
		writeError(w, r, a.log(), domain.Invalid("reason", "reason query parameter is required"))
		return
	}
	result, err := a.admin.Purge(r.Context(), a.adminMeta(r), reason)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": result})
}

// ---------------------------------------------------------------------------
// Lawful access endpoints — technical admin only
// ---------------------------------------------------------------------------

type legalRequestInput struct {
	CaseNumber   string   `json:"case_number"`
	Authority    string   `json:"authority"`
	Country      string   `json:"country"`
	LegalBasis   string   `json:"legal_basis"`
	TargetUserID *string  `json:"target_user_id"`
	Scopes       []string `json:"scopes"`
	Reason       string   `json:"reason"`
}

func (a *API) adminCreateLegalRequest(w http.ResponseWriter, r *http.Request) {
	var req legalRequestInput
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	in := service.CreateLegalRequestInput{
		CaseNumber: req.CaseNumber, Authority: req.Authority, Country: req.Country,
		LegalBasis: req.LegalBasis, Scopes: req.Scopes, Reason: req.Reason,
	}
	if req.TargetUserID != nil && strings.TrimSpace(*req.TargetUserID) != "" {
		id, err := parseUUIDParamValue(*req.TargetUserID, "target_user_id")
		if err != nil {
			writeError(w, r, a.log(), err)
			return
		}
		in.TargetUserID = &id
	}
	created, err := a.admin.CreateLegalRequest(r.Context(), a.adminMeta(r), in)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (a *API) adminListLegalRequests(w http.ResponseWriter, r *http.Request) {
	limit, _, _ := intQuery(r.URL.Query().Get("limit"))
	offset, _, _ := intQuery(r.URL.Query().Get("offset"))
	reqs, err := a.admin.ListLegalRequests(r.Context(), a.adminMeta(r), limit, offset)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": reqs, "count": len(reqs)})
}

func (a *API) adminGetLegalRequest(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "requestID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	req, err := a.admin.GetLegalRequest(r.Context(), a.adminMeta(r), id)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, req)
}

type legalApprovalInput struct {
	Reason string `json:"reason"`
}

func (a *API) adminApproveLegalRequest(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "requestID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	var req legalApprovalInput
	_ = decodeJSON(w, r, &req, 1<<20)
	updated, err := a.admin.ApproveLegalRequest(r.Context(), a.adminMeta(r), id, req.Reason)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (a *API) adminExportLegalRequest(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "requestID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	var req legalApprovalInput
	_ = decodeJSON(w, r, &req, 1<<20)
	export, err := a.admin.ExportLegalRequest(r.Context(), a.adminMeta(r), id, req.Reason)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="legal-export-`+id.String()+`.json"`)
	writeJSON(w, http.StatusOK, export)
}

// exportMyData lets any user download their own personal data (GDPR-style).
func (a *API) exportMyData(w http.ResponseWriter, r *http.Request) {
	data, err := a.admin.SelfUserData(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}
