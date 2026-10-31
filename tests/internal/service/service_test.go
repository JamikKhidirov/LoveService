// Package service_test contains black-box tests for internal/service. Only
// pure validation paths are exercised here; database-backed flows live in
// tests/integration.
package service_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/service"
)

func TestConstructors(t *testing.T) {
	log := slog.Default()
	tokens := security.NewTokenManager("01234567890123456789012345678901", time.Minute, time.Hour)

	if got := service.NewAccountService(nil, nil); got == nil {
		t.Fatal("NewAccountService returned nil")
	}
	if got := service.NewAdminService(nil, true, log); got == nil {
		t.Fatal("NewAdminService returned nil")
	}
	if got := service.NewAuthService(nil, tokens, nil, "development", log); got == nil {
		t.Fatal("NewAuthService returned nil")
	}
	if got := service.NewChatService(nil, nil, nil, log); got == nil {
		t.Fatal("NewChatService returned nil")
	}
	if got := service.NewDiscoveryService(nil, nil); got == nil {
		t.Fatal("NewDiscoveryService returned nil")
	}
	if got := service.NewNotificationService(nil, nil, log); got == nil {
		t.Fatal("NewNotificationService returned nil")
	}
	if got := service.NewProfileService(nil, nil, nil, 50, true, log); got == nil {
		t.Fatal("NewProfileService returned nil")
	}
	if got := service.NewSwipeService(nil, nil, 100, 5, time.Minute, log); got == nil {
		t.Fatal("NewSwipeService returned nil")
	}
}

func TestSwipeRejectsInvalidAction(t *testing.T) {
	svc := service.NewSwipeService(nil, nil, 100, 5, time.Minute, slog.Default())
	_, err := svc.Swipe(context.Background(), uuid.New(), uuid.New(), "hug")
	if err == nil {
		t.Fatal("expected invalid action error")
	}
	if _, ok := err.(*domain.ValidationError); !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
}

func TestSwipeRejectsSelfAction(t *testing.T) {
	svc := service.NewSwipeService(nil, nil, 100, 5, time.Minute, slog.Default())
	id := uuid.New()
	if _, err := svc.Swipe(context.Background(), id, id, domain.ActionLike); err != domain.ErrSelfAction {
		t.Fatalf("expected ErrSelfAction, got %v", err)
	}
}

func TestAdminValidatesStatusAndRole(t *testing.T) {
	svc := service.NewAdminService(nil, true, slog.Default())
	meta := service.AdminMeta{AdminID: uuid.New()}

	if err := svc.SetUserStatus(context.Background(), meta, uuid.New(), "sleeping", "test"); err == nil {
		t.Fatal("expected invalid status error")
	}
	if err := svc.SetUserRole(context.Background(), meta, uuid.New(), "superuser", "test"); err == nil {
		t.Fatal("expected invalid role error")
	}
}

func TestDeletePhotoRequiresReason(t *testing.T) {
	svc := service.NewAdminService(nil, true, slog.Default())
	meta := service.AdminMeta{AdminID: uuid.New()}
	if _, err := svc.DeletePhoto(context.Background(), meta, uuid.New(), "   "); err == nil {
		t.Fatal("expected reason to be required")
	}
}

func TestCreateLegalRequestValidatesInput(t *testing.T) {
	svc := service.NewAdminService(nil, true, slog.Default())
	meta := service.AdminMeta{AdminID: uuid.New()}
	ctx := context.Background()

	if _, err := svc.CreateLegalRequest(ctx, meta, service.CreateLegalRequestInput{}); err == nil {
		t.Fatal("expected required fields to be validated")
	}
	if _, err := svc.CreateLegalRequest(ctx, meta, service.CreateLegalRequestInput{
		CaseNumber: "CASE-1", Authority: "Police", Country: "RU", LegalBasis: "Court order",
		Scopes: []string{"bogus"},
	}); err == nil {
		t.Fatal("expected unknown scope to be rejected")
	}
}

func TestPremiumValidatesPlan(t *testing.T) {
	svc := service.NewPremiumService(nil)
	_, err := svc.Subscribe(context.Background(), uuid.New(), "lifetime")
	if err == nil {
		t.Fatal("expected an unknown plan to be rejected")
	}
	if _, ok := err.(*domain.ValidationError); !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
}

func TestChatRejectsUnknownGift(t *testing.T) {
	svc := service.NewChatService(nil, nil, nil, slog.Default())
	_, err := svc.SendGift(context.Background(), uuid.New(), uuid.New(), "unicorn")
	if err == nil {
		t.Fatal("expected an unknown gift code to be rejected")
	}
	if _, ok := err.(*domain.ValidationError); !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
}

func TestChatGiftCatalogIsACopy(t *testing.T) {
	svc := service.NewChatService(nil, nil, nil, slog.Default())
	catalog := svc.GiftCatalog()
	if len(catalog) == 0 {
		t.Fatal("expected a non-empty gift catalogue")
	}
	catalog[0].Coins = -1
	if svc.GiftCatalog()[0].Coins == -1 {
		t.Fatal("GiftCatalog must not expose the internal slice")
	}
}
