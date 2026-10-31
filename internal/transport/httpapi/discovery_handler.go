package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/service"
)

func (a *API) discover(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, hasLat, err := floatQuery(q.Get("lat"))
	if err != nil {
		writeError(w, r, a.log(), domain.Invalid("lat", "lat must be a number"))
		return
	}
	lng, hasLng, err := floatQuery(q.Get("lng"))
	if err != nil {
		writeError(w, r, a.log(), domain.Invalid("lng", "lng must be a number"))
		return
	}
	if hasLat != hasLng {
		writeError(w, r, a.log(), domain.Invalid("location", "lat and lng must be provided together"))
		return
	}
	radius, hasRadius, err := floatQuery(q.Get("radius_km"))
	if err != nil {
		writeError(w, r, a.log(), domain.Invalid("radius_km", "radius_km must be a number"))
		return
	}
	minAge, _, err := intQuery(q.Get("min_age"))
	if err != nil {
		writeError(w, r, a.log(), domain.Invalid("min_age", "min_age must be an integer"))
		return
	}
	maxAge, _, err := intQuery(q.Get("max_age"))
	if err != nil {
		writeError(w, r, a.log(), domain.Invalid("max_age", "max_age must be an integer"))
		return
	}
	limit, _, err := intQuery(q.Get("limit"))
	if err != nil {
		writeError(w, r, a.log(), domain.Invalid("limit", "limit must be an integer"))
		return
	}
	offset, _, err := intQuery(q.Get("offset"))
	if err != nil {
		writeError(w, r, a.log(), domain.Invalid("offset", "offset must be an integer"))
		return
	}
	expand := true
	if raw := q.Get("expand"); raw != "" {
		expand, err = strconv.ParseBool(raw)
		if err != nil {
			writeError(w, r, a.log(), domain.Invalid("expand", "expand must be true or false"))
			return
		}
	}
	genders := append([]string{}, q["gender"]...)
	if raw := q.Get("genders"); raw != "" {
		for _, g := range strings.Split(raw, ",") {
			if g = strings.TrimSpace(g); g != "" {
				genders = append(genders, g)
			}
		}
	}

	params := service.DiscoverParams{
		RadiusKM: floatPtr(radius, hasRadius),
		MinAge:   intPtr(minAge, q.Get("min_age") != ""),
		MaxAge:   intPtr(maxAge, q.Get("max_age") != ""),
		Genders:  genders,
		Limit:    limit,
		Offset:   offset,
		Expand:   expand,
		Sort:     q.Get("sort"),
	}
	if hasLat && hasLng {
		params.Lat = &lat
		params.Lng = &lng
	}
	result, err := a.discovery.Discover(r.Context(), userIDFrom(r.Context()), params)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) incomingLikes(w http.ResponseWriter, r *http.Request) {
	limit, _, _ := intQuery(r.URL.Query().Get("limit"))
	result, err := a.discovery.IncomingLikes(r.Context(), userIDFrom(r.Context()), limit)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *API) topPicks(w http.ResponseWriter, r *http.Request) {
	picks, err := a.discovery.TopPicks(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": picks, "count": len(picks)})
}

func (a *API) profileStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.discovery.ProfileStats(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (a *API) sentLikes(w http.ResponseWriter, r *http.Request) {
	limit, _, _ := intQuery(r.URL.Query().Get("limit"))
	items, err := a.discovery.SentLikes(r.Context(), userIDFrom(r.Context()), limit)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	st, err := a.discovery.Stats(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) nearbyCount(w http.ResponseWriter, r *http.Request) {
	radius, ok, err := floatQuery(r.URL.Query().Get("radius_km"))
	if err != nil {
		writeError(w, r, a.log(), domain.Invalid("radius_km", "radius_km must be a number"))
		return
	}
	if !ok {
		radius = 50
	}
	n, err := a.discovery.NearbyCount(r.Context(), userIDFrom(r.Context()), radius)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"radius_km": radius, "count": n})
}

func floatQuery(raw string) (float64, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	return v, true, err
}

func intQuery(raw string) (int, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.Atoi(raw)
	return v, true, err
}

func hasLocation(v float64) bool { return v != 0 }
func floatPtr(v float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return &v
}
func intPtr(v int, ok bool) *int {
	if !ok {
		return nil
	}
	return &v
}
