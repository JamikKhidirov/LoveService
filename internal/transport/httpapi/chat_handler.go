package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
)

func (a *API) listConversations(w http.ResponseWriter, r *http.Request) {
	items, err := a.chat.ListConversations(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (a *API) getConversation(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "conversationID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	conv, err := a.chat.GetConversation(r.Context(), userIDFrom(r.Context()), id)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (a *API) listMessages(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "conversationID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	before, err := parseTimeQuery(r, "before")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	limit, _, _ := intQuery(r.URL.Query().Get("limit"))
	items, err := a.chat.ListMessages(r.Context(), userIDFrom(r.Context()), id, before, limit)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	nextBefore := ""
	if len(items) > 0 {
		nextBefore = items[0].CreatedAt.Format("2006-01-02T15:04:05.999999Z07:00")
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items), "next_before": nextBefore})
}

type sendMessageRequest struct {
	ConversationID string  `json:"conversation_id"`
	Body           string  `json:"body"`
	MediaURL       *string `json:"media_url"`
}

func (a *API) sendMessage(w http.ResponseWriter, r *http.Request) {
	var req sendMessageRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	conversationID, err := parseUUIDParamValue(req.ConversationID, "conversation_id")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	msg, err := a.chat.SendMessage(r.Context(), userIDFrom(r.Context()), conversationID, req.Body, req.MediaURL)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

func (a *API) sendMessageNested(w http.ResponseWriter, r *http.Request) {
	conversationID, err := parseUUIDParam(r, "conversationID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	var req struct {
		Body     string  `json:"body"`
		MediaURL *string `json:"media_url"`
	}
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	msg, err := a.chat.SendMessage(r.Context(), userIDFrom(r.Context()), conversationID, req.Body, req.MediaURL)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

func (a *API) markRead(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "conversationID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	senders, readAt, err := a.chat.MarkRead(r.Context(), userIDFrom(r.Context()), id)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"read_at": readAt, "notified_senders": senders})
}

func (a *API) typing(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "conversationID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.chat.Typing(r.Context(), userIDFrom(r.Context()), id); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listMatches(w http.ResponseWriter, r *http.Request) {
	items, err := a.account.Matches(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (a *API) getMatch(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "matchID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	m, err := a.account.Match(r.Context(), userIDFrom(r.Context()), id)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (a *API) unmatch(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "matchID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.account.Unmatch(r.Context(), userIDFrom(r.Context()), id); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"unmatched": true})
}

func parseUUIDParamValue(raw, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, domain.Invalid(field, field+" must be a valid UUID")
	}
	return id, nil
}
