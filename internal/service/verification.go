package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/storage"
	"github.com/loveservice/loveservice/internal/store"
	"github.com/loveservice/loveservice/internal/verification"
)

// VerificationService checks a selfie against the profile's primary photo and
// updates the public verification badge when the provider approves it.
type VerificationService struct {
	store     *store.Store
	storage   storage.Storage
	verifier  verification.Verifier
	log       *slog.Logger
	maxUpload int64
	threshold float64
}

func NewVerificationService(st *store.Store, media storage.Storage, v verification.Verifier, threshold float64, maxUploadMB int64, log *slog.Logger) *VerificationService {
	if threshold <= 0 || threshold > 1 {
		threshold = 0.75
	}
	if maxUploadMB <= 0 {
		maxUploadMB = 8
	}
	if v == nil {
		v = verification.AutoVerifier{}
	}
	return &VerificationService{
		store: st, storage: media, verifier: v, log: log,
		maxUpload: maxUploadMB << 20, threshold: threshold,
	}
}

// Status returns the latest attempt, or nil when the user never tried.
func (s *VerificationService) Status(ctx context.Context, userID uuid.UUID) (*domain.FaceVerification, error) {
	fv, err := s.store.LatestFaceVerification(ctx, userID)
	if err != nil {
		if err == domain.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return fv, nil
}

// Submit stores the selfie, compares it with the primary photo and records the
// outcome. The profile badge is only set when the provider approves.
func (s *VerificationService) Submit(ctx context.Context, userID uuid.UUID, filename, contentType string, size int64, r io.Reader) (*domain.FaceVerification, error) {
	if size <= 0 {
		return nil, domain.Invalid("file", "file is empty")
	}
	if size > s.maxUpload {
		return nil, domain.Invalid("file", fmt.Sprintf("file must be smaller than %d MB", s.maxUpload>>20))
	}
	mediaType, ext, err := detectMedia(filename, contentType)
	if err != nil {
		return nil, err
	}
	if mediaType != domain.MediaPhoto {
		return nil, domain.Invalid("file", "the selfie must be an image")
	}

	photos, err := s.store.ListPhotos(ctx, userID)
	if err != nil {
		return nil, err
	}
	var reference *domain.Photo
	for i := range photos {
		if photos[i].MediaType != domain.MediaPhoto || photos[i].ModerationStatus == domain.ModerationRejected {
			continue
		}
		if reference == nil || photos[i].IsPrimary {
			reference = &photos[i]
		}
	}
	if reference == nil {
		return nil, domain.ErrPhotoRequired
	}

	selfie, err := io.ReadAll(io.LimitReader(r, s.maxUpload+1))
	if err != nil {
		return nil, domain.Invalid("file", "could not read the uploaded file")
	}
	if int64(len(selfie)) == 0 {
		return nil, domain.Invalid("file", "file is empty")
	}
	if int64(len(selfie)) > s.maxUpload {
		return nil, domain.Invalid("file", fmt.Sprintf("file must be smaller than %d MB", s.maxUpload>>20))
	}

	referenceReader, err := s.storage.Get(ctx, reference.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("load reference photo: %w", err)
	}
	referenceBytes, readErr := io.ReadAll(io.LimitReader(referenceReader, s.maxUpload+1))
	_ = referenceReader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("load reference photo: %w", readErr)
	}

	key := fmt.Sprintf("verifications/%s/%s%s", userID.String(), uuid.NewString(), ext)
	selfieURL, err := s.storage.Put(ctx, key, bytes.NewReader(selfie), int64(len(selfie)), contentType)
	if err != nil {
		return nil, fmt.Errorf("store selfie: %w", err)
	}

	status, reason := domain.VerificationPending, ""
	score, verifyErr := s.verifier.Compare(ctx, selfie, referenceBytes, contentType)
	reviewedAt := time.Now().UTC()
	switch {
	case verifyErr != nil:
		status = domain.VerificationError
		reason = verifyErr.Error()
		if s.log != nil {
			s.log.Warn("face verification provider failed", "user_id", userID, "error", verifyErr)
		}
	case score >= s.threshold:
		status = domain.VerificationApproved
		reason = "selfie matches the profile photo"
	default:
		status = domain.VerificationRejected
		reason = "the selfie does not match the profile photo closely enough"
	}

	refID := reference.ID
	record := &domain.FaceVerification{
		UserID:           userID,
		SelfieURL:        selfieURL,
		SelfieKey:        key,
		ReferencePhotoID: &refID,
		Score:            score,
		Provider:         s.verifier.Name(),
		Status:           status,
		Reason:           reason,
		ReviewedAt:       &reviewedAt,
	}
	if status == domain.VerificationPending {
		record.ReviewedAt = nil
	}
	saved, err := s.store.CreateFaceVerification(ctx, record)
	if err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, err
	}
	if status == domain.VerificationApproved {
		if err := s.store.SetProfileVerified(ctx, userID, true); err != nil && s.log != nil {
			s.log.Warn("could not set verification badge", "user_id", userID, "error", err)
		}
		_ = s.store.Enqueue(ctx, "love.events", "user.verified", userID.String(), map[string]any{
			"user_id": userID,
			"score":   score,
			"method":  "face",
		})
	}
	if verifyErr != nil {
		return saved, fmt.Errorf("%w: %s", domain.ErrVerificationFailed, strings.TrimSpace(verifyErr.Error()))
	}
	return saved, nil
}
