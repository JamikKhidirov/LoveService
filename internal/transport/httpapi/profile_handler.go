package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/service"
)

type updateProfileRequest struct {
	DisplayName *string   `json:"display_name"`
	BirthDate   *string   `json:"birth_date"`
	Gender      *string   `json:"gender"`
	Bio         *string   `json:"bio"`
	City        *string   `json:"city"`
	Country     *string   `json:"country"`
	HeightCM    *int      `json:"height_cm"`
	Job         *string   `json:"job"`
	Education   *string   `json:"education"`
	Interests   *[]string `json:"interests"`
	LookingFor  *[]string `json:"looking_for"`
	Phone       *string   `json:"phone"`
}

func (a *API) getMyProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := a.profiles.GetMe(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (a *API) updateMyProfile(w http.ResponseWriter, r *http.Request) {
	var req updateProfileRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	profile, err := a.profiles.UpdateMe(r.Context(), userIDFrom(r.Context()), service.UpdateProfileInput{
		DisplayName: req.DisplayName, BirthDate: req.BirthDate, Gender: req.Gender,
		Bio: req.Bio, City: req.City, Country: req.Country, HeightCM: req.HeightCM,
		Job: req.Job, Education: req.Education, Interests: req.Interests,
		LookingFor: req.LookingFor, Phone: req.Phone,
	})
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (a *API) getProfile(w http.ResponseWriter, r *http.Request) {
	targetID, err := parseUUIDParam(r, "userID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	profile, err := a.profiles.GetUser(r.Context(), userIDFrom(r.Context()), targetID)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

type locationRequest struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func (a *API) updateLocation(w http.ResponseWriter, r *http.Request) {
	var req locationRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	profile, err := a.profiles.UpdateLocation(r.Context(), userIDFrom(r.Context()), req.Lat, req.Lng)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

type preferencesRequest struct {
	MinAge           *int      `json:"min_age"`
	MaxAge           *int      `json:"max_age"`
	MaxDistanceKM    *int      `json:"max_distance_km"`
	PreferredGenders *[]string `json:"preferred_genders"`
	GlobalMode       *bool     `json:"global_mode"`
}

func (a *API) getPreferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := a.profiles.GetPreferences(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (a *API) updatePreferences(w http.ResponseWriter, r *http.Request) {
	var req preferencesRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	prefs, err := a.profiles.UpdatePreferences(r.Context(), userIDFrom(r.Context()), service.UpdatePreferencesInput{
		MinAge: req.MinAge, MaxAge: req.MaxAge, MaxDistanceKM: req.MaxDistanceKM,
		PreferredGenders: req.PreferredGenders, GlobalMode: req.GlobalMode,
	})
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

type settingsRequest struct {
	NotificationsEnabled *bool   `json:"notifications_enabled"`
	Discoverable         *bool   `json:"discoverable"`
	ShowDistance         *bool   `json:"show_distance"`
	ShowOnline           *bool   `json:"show_online"`
	Language             *string `json:"language"`
}

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := a.profiles.GetSettings(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) updateSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	st, err := a.profiles.UpdateSettings(r.Context(), userIDFrom(r.Context()), service.UpdateSettingsInput{
		NotificationsEnabled: req.NotificationsEnabled, Discoverable: req.Discoverable,
		ShowDistance: req.ShowDistance, ShowOnline: req.ShowOnline, Language: req.Language,
	})
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (a *API) listMedia(w http.ResponseWriter, r *http.Request) {
	photos, err := a.profiles.ListMedia(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, photos)
}

func (a *API) uploadMedia(w http.ResponseWriter, r *http.Request) {
	maxBody := (a.cfg.MaxUploadMB << 20) + (1 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, r, a.log(), domain.Invalid("file", "uploaded file is too large"))
			return
		}
		writeError(w, r, a.log(), domain.Invalid("file", "multipart form with a 'file' field is required"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, a.log(), domain.Invalid("file", "field 'file' is required"))
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	var body io.Reader = file
	if contentType == "" || strings.HasPrefix(strings.ToLower(contentType), "application/octet-stream") {
		// Browsers and some HTTP clients do not attach a MIME type to multipart
		// parts. Sniff the magic bytes instead of rejecting a valid photo.
		head := make([]byte, 512)
		n, _ := io.ReadFull(file, head)
		head = head[:n]
		contentType = http.DetectContentType(head)
		body = io.MultiReader(bytes.NewReader(head), file)
	}
	photo, err := a.profiles.AddMedia(r.Context(), userIDFrom(r.Context()), header.Filename, contentType, header.Size, body)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, photo)
}

func (a *API) deleteMedia(w http.ResponseWriter, r *http.Request) {
	mediaID, err := parseUUIDParam(r, "mediaID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.profiles.DeleteMedia(r.Context(), userIDFrom(r.Context()), mediaID); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) setPrimaryMedia(w http.ResponseWriter, r *http.Request) {
	mediaID, err := parseUUIDParam(r, "mediaID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	photo, err := a.profiles.SetPrimaryMedia(r.Context(), userIDFrom(r.Context()), mediaID)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, photo)
}

func (a *API) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if err := a.profiles.DeleteAccount(r.Context(), userIDFrom(r.Context())); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

var _ = strings.TrimSpace
