// Package verification compares a selfie with a profile photo. It ships with a
// provider-agnostic HTTP client for external ML services and a deterministic
// development verifier used when FACE_VERIFY_URL is empty.
package verification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Verifier returns a similarity score in the 0..1 range.
type Verifier interface {
	Name() string
	Compare(ctx context.Context, selfie, reference []byte, contentType string) (float64, error)
}

// New selects the HTTP provider when a URL is configured, otherwise the
// development auto-verifier.
func New(url, apiKey string, timeout time.Duration, log *slog.Logger) Verifier {
	if strings.TrimSpace(url) == "" {
		return AutoVerifier{}
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &HTTPVerifier{url: url, apiKey: apiKey, client: &http.Client{Timeout: timeout}, log: log}
}

// AutoVerifier approves any selfie when a real provider is not configured. It is
// intended for local development and CI only.
type AutoVerifier struct{}

func (AutoVerifier) Name() string { return "auto" }

func (AutoVerifier) Compare(_ context.Context, selfie, reference []byte, _ string) (float64, error) {
	if len(selfie) == 0 {
		return 0, fmt.Errorf("selfie is empty")
	}
	if len(reference) == 0 {
		return 0, fmt.Errorf("reference photo is empty")
	}
	return 0.99, nil
}

// HTTPVerifier calls an external face-comparison service.
type HTTPVerifier struct {
	url    string
	apiKey string
	client *http.Client
	log    *slog.Logger
}

func (v *HTTPVerifier) Name() string { return "http" }

func (v *HTTPVerifier) Compare(ctx context.Context, selfie, reference []byte, contentType string) (float64, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	selfiePart, err := writer.CreateFormFile("selfie", "selfie"+extensionFor(contentType))
	if err != nil {
		return 0, err
	}
	if _, err := selfiePart.Write(selfie); err != nil {
		return 0, err
	}
	referencePart, err := writer.CreateFormFile("reference", "reference"+extensionFor(contentType))
	if err != nil {
		return 0, err
	}
	if _, err := referencePart.Write(reference); err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.url, &body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if v.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.apiKey)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("face verification provider: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("face verification provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload struct {
		Score      *float64 `json:"score"`
		Confidence *float64 `json:"confidence"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, fmt.Errorf("decode face verification response: %w", err)
	}
	score := payload.Score
	if score == nil {
		score = payload.Confidence
	}
	if score == nil {
		return 0, fmt.Errorf("face verification response has no score")
	}
	if *score > 1 {
		*score = *score / 100
	}
	return *score, nil
}

func extensionFor(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}
