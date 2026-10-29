package httpapi

import (
	"net/http"

	"github.com/loveservice/loveservice/internal/domain"
)

type swipeRequest struct {
	TargetUserID string `json:"target_user_id"`
	Action       string `json:"action"`
}

func (a *API) swipe(w http.ResponseWriter, r *http.Request) {
	var req swipeRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	targetID, err := parseUUIDParamValue(req.TargetUserID, "target_user_id")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	action := req.Action
	if action == "" {
		action = domain.ActionLike
	}
	res, err := a.swipes.Swipe(r.Context(), userIDFrom(r.Context()), targetID, action)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) rewind(w http.ResponseWriter, r *http.Request) {
	swipe, err := a.swipes.Rewind(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rewound": true, "swipe": swipe})
}

func (a *API) swipeLimits(w http.ResponseWriter, r *http.Request) {
	limits, err := a.swipes.Limits(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, limits)
}
