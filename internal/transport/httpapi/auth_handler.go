package httpapi

import (
	"net/http"

	"github.com/loveservice/loveservice/internal/service"
)

type registerRequest struct {
	Email            string   `json:"email"`
	Password         string   `json:"password"`
	DisplayName      string   `json:"display_name"`
	BirthDate        string   `json:"birth_date"`
	Gender           string   `json:"gender"`
	Phone            string   `json:"phone"`
	PreferredGenders []string `json:"preferred_genders"`
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	res, err := a.auth.Register(r.Context(), service.RegisterInput{
		Email: req.Email, Password: req.Password, DisplayName: req.DisplayName,
		BirthDate: req.BirthDate, Gender: req.Gender, Phone: req.Phone,
		PreferredGenders: req.PreferredGenders,
	}, meta(r))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	res, err := a.auth.Login(r.Context(), req.Email, req.Password, meta(r))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	res, err := a.auth.Refresh(r.Context(), req.RefreshToken, meta(r))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	_ = decodeJSON(w, r, &req, 1<<20)
	if err := a.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"logged_out": true})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	user, profile, err := a.auth.Me(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": user, "profile": profile})
}

type verifyEmailRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func (a *API) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.auth.VerifyEmail(r.Context(), req.Email, req.Code); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"verified": true})
}

type emailOnlyRequest struct {
	Email string `json:"email"`
}

func (a *API) resendVerification(w http.ResponseWriter, r *http.Request) {
	var req emailOnlyRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	code, err := a.auth.ResendVerification(r.Context(), req.Email)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	payload := map[string]any{"sent": true}
	if code != "" {
		payload["dev_code"] = code
	}
	writeJSON(w, http.StatusOK, payload)
}

func (a *API) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req emailOnlyRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	code, err := a.auth.ForgotPassword(r.Context(), req.Email)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	payload := map[string]any{"sent": true}
	if code != "" {
		payload["dev_code"] = code
	}
	writeJSON(w, http.StatusOK, payload)
}

type resetPasswordRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

func (a *API) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.auth.ResetPassword(r.Context(), req.Email, req.Code, req.NewPassword); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"reset": true})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	pair, err := a.auth.ChangePassword(r.Context(), userIDFrom(r.Context()), req.CurrentPassword, req.NewPassword, meta(r))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": pair})
}

func meta(r *http.Request) service.RequestMeta {
	return service.RequestMeta{UserAgent: r.UserAgent(), IP: clientIP(r)}
}
