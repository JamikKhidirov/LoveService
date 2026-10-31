package httpapi

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/loveservice/loveservice/internal/domain"
)

// ---------------------------------------------------------------------------
// Device / session management
// ---------------------------------------------------------------------------

func (a *API) listSessions(w http.ResponseWriter, r *http.Request) {
	items, err := a.auth.Sessions(r.Context(), userIDFrom(r.Context()), sessionIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (a *API) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "sessionID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if err := a.auth.RevokeSession(r.Context(), userIDFrom(r.Context()), id); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Premium, coins and who-liked-me
// ---------------------------------------------------------------------------

func (a *API) premiumStatus(w http.ResponseWriter, r *http.Request) {
	status, err := a.premium.Status(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) subscribePremium(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Plan string `json:"plan"`
	}
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	status, err := a.premium.Subscribe(r.Context(), userIDFrom(r.Context()), req.Plan)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) unlockLikes(w http.ResponseWriter, r *http.Request) {
	status, err := a.premium.UnlockLikes(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *API) wallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := a.premium.Wallet(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, wallet)
}

func (a *API) topUpWallet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Coins     int    `json:"coins"`
		Reference string `json:"reference"`
	}
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	wallet, err := a.premium.TopUp(r.Context(), userIDFrom(r.Context()), req.Coins, req.Reference)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusOK, wallet)
}

// ---------------------------------------------------------------------------
// Face verification
// ---------------------------------------------------------------------------

func (a *API) faceVerificationStatus(w http.ResponseWriter, r *http.Request) {
	fv, err := a.verification.Status(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	if fv == nil {
		profile, perr := a.profiles.GetMe(r.Context(), userIDFrom(r.Context()))
		verified := false
		if perr == nil {
			verified = profile.IsVerified
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "unverified", "is_verified": verified})
		return
	}
	writeJSON(w, http.StatusOK, fv)
}

func (a *API) submitFaceVerification(w http.ResponseWriter, r *http.Request) {
	up, ok := a.readUpload(w, r, "file", "selfie")
	if !ok {
		return
	}
	defer up.Close()
	fv, err := a.verification.Submit(r.Context(), userIDFrom(r.Context()), up.Filename, up.ContentType, up.Size, up.Body)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, fv)
}

// ---------------------------------------------------------------------------
// Chat media and gifts
// ---------------------------------------------------------------------------

func (a *API) sendChatMedia(w http.ResponseWriter, r *http.Request) {
	conversationID, err := parseUUIDParam(r, "conversationID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	up, ok := a.readUpload(w, r, "file", "media")
	if !ok {
		return
	}
	defer up.Close()
	msg, err := a.chat.SendMedia(r.Context(), userIDFrom(r.Context()), conversationID, up.Filename, up.ContentType, up.Size, up.Body)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

func (a *API) giftCatalog(w http.ResponseWriter, r *http.Request) {
	items := a.chat.GiftCatalog()
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (a *API) sendGift(w http.ResponseWriter, r *http.Request) {
	conversationID, err := parseUUIDParam(r, "conversationID")
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	var req struct {
		GiftCode string `json:"gift_code"`
		Code     string `json:"code"`
	}
	if err := decodeJSON(w, r, &req, 1<<20); err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	code := req.GiftCode
	if strings.TrimSpace(code) == "" {
		code = req.Code
	}
	res, err := a.chat.SendGift(r.Context(), userIDFrom(r.Context()), conversationID, code)
	if err != nil {
		writeError(w, r, a.log(), err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// ---------------------------------------------------------------------------
// Multipart helper
// ---------------------------------------------------------------------------

type uploadedFile struct {
	Filename    string
	ContentType string
	Size        int64
	Body        io.Reader
	closer      io.Closer
}

func (u *uploadedFile) Close() {
	if u != nil && u.closer != nil {
		_ = u.closer.Close()
	}
}

// readUpload parses a multipart request with the given field name and sniffs the
// content type when clients send application/octet-stream.
func (a *API) readUpload(w http.ResponseWriter, r *http.Request, fields ...string) (*uploadedFile, bool) {
	maxUpload := int64(50)
	if a.cfg != nil && a.cfg.MaxUploadMB > 0 {
		maxUpload = a.cfg.MaxUploadMB
	}
	r.Body = http.MaxBytesReader(w, r.Body, (maxUpload<<20)+(1<<20))
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, r, a.log(), domain.Invalid("file", "uploaded file is too large"))
			return nil, false
		}
		writeError(w, r, a.log(), domain.Invalid("file", "multipart form with a file field is required"))
		return nil, false
	}
	var file multipart.File
	var header *multipart.FileHeader
	for _, field := range fields {
		f, h, err := r.FormFile(field)
		if err == nil {
			file, header = f, h
			break
		}
	}
	if file == nil || header == nil {
		writeError(w, r, a.log(), domain.Invalid("file", "field 'file' is required"))
		return nil, false
	}

	up := &uploadedFile{
		Filename:    header.Filename,
		ContentType: header.Header.Get("Content-Type"),
		Size:        header.Size,
		Body:        file,
		closer:      file,
	}
	if up.ContentType == "" || strings.HasPrefix(strings.ToLower(up.ContentType), "application/octet-stream") {
		head := make([]byte, 512)
		n, _ := io.ReadFull(file, head)
		head = head[:n]
		up.ContentType = http.DetectContentType(head)
		up.Body = io.MultiReader(bytes.NewReader(head), file)
	}
	return up, true
}
