package httpapi

import (
	"github.com/go-chi/chi/v5"

	"github.com/loveservice/loveservice/internal/domain"
)

// mountRoutes keeps the route table in one place, separate from middleware and
// dependency wiring. Nesting mirrors the authorization model:
//
//	public  → auth endpoints
//	private → authenticated user endpoints
//	admin   → moderator/admin endpoints (audited)
func (a *API) mountRoutes(r chi.Router, d Deps) {
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/ws", d.WS.ServeHTTP)

		a.mountPublicAuthRoutes(r)
		a.mountUserRoutes(r, d)
	})

	// Native clients that cannot use /api/v1 can connect at /ws as well.
	r.Get("/ws", d.WS.ServeHTTP)
}

func (a *API) mountPublicAuthRoutes(r chi.Router) {
	r.Post("/auth/register", a.register)
	r.Post("/auth/login", a.login)
	r.Post("/auth/refresh", a.refresh)
	r.Post("/auth/logout", a.logout)
	r.Post("/auth/verify-email", a.verifyEmail)
	r.Post("/auth/resend-verification", a.resendVerification)
	r.Post("/auth/forgot-password", a.forgotPassword)
	r.Post("/auth/reset-password", a.resetPassword)
}

func (a *API) mountUserRoutes(r chi.Router, d Deps) {
	r.Group(func(r chi.Router) {
		r.Use(requireAuth(d.Tokens, d.Store))

		a.mountAccountRoutes(r)
		a.mountProfileRoutes(r)
		a.mountDiscoveryRoutes(r)
		a.mountSwipeRoutes(r)
		a.mountMatchRoutes(r)
		a.mountChatRoutes(r)
		a.mountNotificationRoutes(r)
		a.mountSafetyRoutes(r)

		// Personal data export (GDPR / privacy rights).
		r.Get("/account/export", a.exportMyData)

		a.mountAdminRoutes(r)
	})
}

func (a *API) mountAccountRoutes(r chi.Router) {
	r.Get("/auth/me", a.me)
	r.Post("/auth/change-password", a.changePassword)
	r.Post("/auth/logout-all", a.logoutAll)
	r.Get("/devices", a.listDevices)
	r.Post("/devices", a.saveDevice)
	r.Delete("/devices", a.deleteDevice)
	// Device/session management: IP, phone model and login date per device.
	r.Get("/sessions", a.listSessions)
	r.Delete("/sessions/{sessionID}", a.revokeSession)
}

func (a *API) mountProfileRoutes(r chi.Router) {
	r.Get("/profiles/me", a.getMyProfile)
	r.Patch("/profiles/me", a.updateMyProfile)
	r.Delete("/profiles/me", a.deleteAccount)
	r.Post("/profiles/me/location", a.updateLocation)
	r.Get("/profiles/me/preferences", a.getPreferences)
	r.Patch("/profiles/me/preferences", a.updatePreferences)
	r.Get("/profiles/me/settings", a.getSettings)
	r.Patch("/profiles/me/settings", a.updateSettings)
	r.Get("/profiles/me/photos", a.listMedia)
	r.Post("/profiles/me/photos", a.uploadMedia)
	r.Delete("/profiles/me/photos/{mediaID}", a.deleteMedia)
	r.Patch("/profiles/me/photos/{mediaID}/primary", a.setPrimaryMedia)
	r.Get("/profiles/{userID}", a.getProfile)
	r.Get("/profile/stats", a.profileStats)
	r.Get("/verification/face", a.faceVerificationStatus)
	r.Post("/verification/face", a.submitFaceVerification)
}

func (a *API) mountDiscoveryRoutes(r chi.Router) {
	r.Get("/discovery", a.discover)
	r.Get("/discovery/nearby", a.discover)
	r.Get("/discovery/nearby/count", a.nearbyCount)
	r.Get("/discovery/likes", a.incomingLikes)
	r.Get("/discovery/likes/sent", a.sentLikes)
	r.Get("/discovery/stats", a.stats)
	r.Get("/recommendations/top-picks", a.topPicks)
	r.Get("/discovery/top-picks", a.topPicks)
	r.Post("/discovery/likes/unlock", a.unlockLikes)
	r.Get("/premium", a.premiumStatus)
	r.Post("/premium/subscribe", a.subscribePremium)
	r.Get("/wallet", a.wallet)
	r.Post("/wallet/topup", a.topUpWallet)
	r.Get("/gifts/catalog", a.giftCatalog)
}

func (a *API) mountSwipeRoutes(r chi.Router) {
	r.Post("/swipes", a.swipe)
	r.Post("/swipes/rewind", a.rewind)
	r.Get("/swipes/limits", a.swipeLimits)
}

func (a *API) mountMatchRoutes(r chi.Router) {
	r.Get("/matches", a.listMatches)
	r.Get("/matches/{matchID}", a.getMatch)
	r.Delete("/matches/{matchID}", a.unmatch)
}

func (a *API) mountChatRoutes(r chi.Router) {
	r.Get("/conversations", a.listConversations)
	r.Get("/conversations/{conversationID}", a.getConversation)
	r.Get("/conversations/{conversationID}/messages", a.listMessages)
	r.Post("/conversations/{conversationID}/messages", a.sendMessageNested)
	r.Post("/conversations/{conversationID}/read", a.markRead)
	r.Post("/conversations/{conversationID}/typing", a.typing)
	r.Post("/conversations/{conversationID}/media", a.sendChatMedia)
	r.Post("/conversations/{conversationID}/gifts", a.sendGift)
	r.Post("/chat/{conversationID}/media", a.sendChatMedia)
	r.Post("/chat/{conversationID}/gifts", a.sendGift)
	r.Post("/messages", a.sendMessage)
}

func (a *API) mountNotificationRoutes(r chi.Router) {
	r.Get("/notifications", a.listNotifications)
	r.Get("/notifications/unread-count", a.unreadNotifications)
	r.Post("/notifications/{notificationID}/read", a.markNotificationRead)
	r.Post("/notifications/read-all", a.markAllNotificationsRead)
}

func (a *API) mountSafetyRoutes(r chi.Router) {
	r.Get("/blocks", a.listBlocks)
	r.Post("/blocks", a.createBlock)
	r.Delete("/blocks/{userID}", a.deleteBlock)
	r.Post("/reports", a.createReport)
}

// mountAdminRoutes exposes other users' data. The "admin" role is the technical
// administrator authorised to respond to lawful requests; every call is written
// to the append-only admin_audit_logs table.
func (a *API) mountAdminRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleModerator, domain.RoleAdmin))

		r.Get("/admin/stats", a.adminStats)
		r.Get("/admin/users", a.adminListUsers)
		r.Get("/admin/users/{userID}", a.adminGetUser)
		r.Get("/admin/reports", a.adminListReports)
		r.Patch("/admin/reports/{reportID}", a.adminUpdateReport)
	})

	r.Group(func(r chi.Router) {
		r.Use(requireRole(domain.RoleAdmin))

		r.Patch("/admin/users/{userID}/status", a.adminSetUserStatus)
		r.Patch("/admin/users/{userID}/role", a.adminSetUserRole)
		r.Patch("/admin/users/{userID}/verify", a.adminVerifyUser)
		r.Delete("/admin/photos/{photoID}", a.adminDeletePhoto)
		r.Post("/admin/notifications/broadcast", a.adminBroadcast)
		r.Post("/admin/maintenance/purge", a.adminPurge)
		r.Get("/admin/audit-logs", a.adminAuditLogs)

		// Lawful access for national security / law enforcement.
		r.Post("/admin/legal-requests", a.adminCreateLegalRequest)
		r.Get("/admin/legal-requests", a.adminListLegalRequests)
		r.Get("/admin/legal-requests/{requestID}", a.adminGetLegalRequest)
		r.Post("/admin/legal-requests/{requestID}/approve", a.adminApproveLegalRequest)
		r.Post("/admin/legal-requests/{requestID}/export", a.adminExportLegalRequest)
	})
}
