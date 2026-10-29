package httpapi

import (
	"net/http"
	"strings"

	"github.com/loveservice/loveservice/internal/domain"
)

func (a *API) listNotifications(w http.ResponseWriter, r *http.Request) {
	before, err := parseTimeQuery(r, "before")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	limit, _, _ := intQuery(r.URL.Query().Get("limit"))
	items, err := a.notifications.List(r.Context(), userIDFrom(r.Context()), before, limit)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	unread, _ := a.notifications.UnreadCount(r.Context(), userIDFrom(r.Context()))
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items), "unread": unread})
}

func (a *API) unreadNotifications(w http.ResponseWriter, r *http.Request) {
	n, err := a.notifications.UnreadCount(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": n})
}

func (a *API) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "notificationID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.notifications.MarkRead(r.Context(), userIDFrom(r.Context()), id); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"read": true})
}

func (a *API) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	n, err := a.notifications.MarkAllRead(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": n})
}

type createBlockRequest struct {
	UserID string `json:"user_id"`
}

func (a *API) listBlocks(w http.ResponseWriter, r *http.Request) {
	items, err := a.account.Blocked(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (a *API) createBlock(w http.ResponseWriter, r *http.Request) {
	var req createBlockRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	targetID, err := parseUUIDParamValue(req.UserID, "user_id")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.account.Block(r.Context(), userIDFrom(r.Context()), targetID); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"blocked": true})
}

func (a *API) deleteBlock(w http.ResponseWriter, r *http.Request) {
	targetID, err := parseUUIDParam(r, "userID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.account.Unblock(r.Context(), userIDFrom(r.Context()), targetID); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type reportRequest struct {
	TargetUserID string `json:"target_user_id"`
	Reason       string `json:"reason"`
	Details      string `json:"details"`
}

func (a *API) createReport(w http.ResponseWriter, r *http.Request) {
	var req reportRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	targetID, err := parseUUIDParamValue(req.TargetUserID, "target_user_id")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	report, err := a.account.Report(r.Context(), userIDFrom(r.Context()), targetID, req.Reason, req.Details)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, report)
}

type deviceRequest struct {
	Platform string `json:"platform"`
	Token    string `json:"token"`
}

func (a *API) saveDevice(w http.ResponseWriter, r *http.Request) {
	var req deviceRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.account.SaveDevice(r.Context(), userIDFrom(r.Context()), req.Platform, req.Token); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"saved": true})
}

func (a *API) listDevices(w http.ResponseWriter, r *http.Request) {
	items, err := a.account.Devices(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (a *API) deleteDevice(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		writeError(w, r, a.log(), domain.Invalid("token", "token query parameter is required"))
		return
	}
	if err := a.account.DeleteDevice(r.Context(), userIDFrom(r.Context()), token); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
