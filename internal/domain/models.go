// Package domain contains the business entities shared by all layers.
// Entities are intentionally free of transport and database concerns.
package domain

import (
	"time"

	"github.com/google/uuid"
)

const (
	RoleUser      = "user"
	RoleModerator = "moderator"
	RoleAdmin     = "admin"

	StatusActive      = "active"
	StatusDeactivated = "deactivated"
	StatusBanned      = "banned"
	StatusDeleted     = "deleted"

	GenderMale   = "male"
	GenderFemale = "female"
	GenderOther  = "other"

	ActionLike      = "like"
	ActionDislike   = "dislike"
	ActionSuperlike = "superlike"

	MediaPhoto = "photo"
	MediaVideo = "video"

	ModerationPending  = "pending"
	ModerationApproved = "approved"
	ModerationRejected = "rejected"

	MessageText  = "text"
	MessageImage = "image"
	MessageVideo = "video"
)

type User struct {
	ID              uuid.UUID  `json:"id"`
	Email           string     `json:"email"`
	Phone           *string    `json:"phone,omitempty"`
	PasswordHash    string     `json:"-"`
	Role            string     `json:"role"`
	Status          string     `json:"status"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (u *User) EmailVerified() bool { return u.EmailVerifiedAt != nil }

type Profile struct {
	UserID            uuid.UUID  `json:"user_id"`
	DisplayName       string     `json:"display_name"`
	BirthDate         time.Time  `json:"-"`
	Gender            string     `json:"gender"`
	Bio               string     `json:"bio"`
	City              string     `json:"city"`
	Country           string     `json:"country"`
	Lat               *float64   `json:"lat,omitempty"`
	Lng               *float64   `json:"lng,omitempty"`
	LocationUpdatedAt *time.Time `json:"location_updated_at,omitempty"`
	HeightCM          *int       `json:"height_cm,omitempty"`
	Job               string     `json:"job"`
	Education         string     `json:"education"`
	Interests         []string   `json:"interests"`
	LookingFor        []string   `json:"looking_for"`
	IsVerified        bool       `json:"is_verified"`
	LastSeenAt        *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`

	// Computed fields, populated by services.
	Age        int      `json:"age"`
	DistanceKM *float64 `json:"distance_km,omitempty"`
	Online     bool     `json:"online"`
	Photos     []Photo  `json:"photos"`
}

func (p *Profile) BirthDateString() string { return p.BirthDate.Format("2006-01-02") }

type Preferences struct {
	UserID           uuid.UUID `json:"user_id"`
	MinAge           int       `json:"min_age"`
	MaxAge           int       `json:"max_age"`
	MaxDistanceKM    int       `json:"max_distance_km"`
	PreferredGenders []string  `json:"preferred_genders"`
	GlobalMode       bool      `json:"global_mode"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type UserSettings struct {
	UserID               uuid.UUID `json:"user_id"`
	NotificationsEnabled bool      `json:"notifications_enabled"`
	Discoverable         bool      `json:"discoverable"`
	ShowDistance         bool      `json:"show_distance"`
	ShowOnline           bool      `json:"show_online"`
	Language             string    `json:"language"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type Photo struct {
	ID               uuid.UUID `json:"id"`
	UserID           uuid.UUID `json:"user_id"`
	URL              string    `json:"url"`
	StorageKey       string    `json:"-"`
	MediaType        string    `json:"media_type"`
	Position         int       `json:"position"`
	IsPrimary        bool      `json:"is_primary"`
	ModerationStatus string    `json:"moderation_status"`
	CreatedAt        time.Time `json:"created_at"`
}

type Swipe struct {
	ID        uuid.UUID `json:"id"`
	SwiperID  uuid.UUID `json:"swiper_id"`
	TargetID  uuid.UUID `json:"target_id"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
}

type Match struct {
	ID             uuid.UUID  `json:"id"`
	UserA          uuid.UUID  `json:"-"`
	UserB          uuid.UUID  `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UnmatchedAt    *time.Time `json:"-"`
	Peer           *Profile   `json:"peer,omitempty"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
}

type Conversation struct {
	ID            uuid.UUID  `json:"id"`
	MatchID       uuid.UUID  `json:"match_id"`
	CreatedAt     time.Time  `json:"created_at"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	Peer          *Profile   `json:"peer"`
	LastMessage   *Message   `json:"last_message,omitempty"`
	UnreadCount   int        `json:"unread_count"`
}

type Message struct {
	ID             uuid.UUID  `json:"id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	SenderID       uuid.UUID  `json:"sender_id"`
	Body           string     `json:"body"`
	Type           string     `json:"type"`
	MediaURL       *string    `json:"media_url,omitempty"`
	ReadAt         *time.Time `json:"read_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type Notification struct {
	ID        uuid.UUID      `json:"id"`
	UserID    uuid.UUID      `json:"-"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Data      map[string]any `json:"data"`
	ReadAt    *time.Time     `json:"read_at,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	UserAgent string
	IP        string
	CreatedAt time.Time
}

type EmailCode struct {
	ID         uuid.UUID
	Email      string
	Purpose    string
	CodeHash   string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	Attempts   int
	CreatedAt  time.Time
}

type Report struct {
	ID         uuid.UUID `json:"id"`
	ReporterID uuid.UUID `json:"reporter_id"`
	TargetID   uuid.UUID `json:"target_id"`
	Reason     string    `json:"reason"`
	Details    string    `json:"details"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

type Device struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"-"`
	Platform  string    `json:"platform"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

type OutboxEvent struct {
	ID          uuid.UUID
	Topic       string
	EventType   string
	AggregateID string
	Payload     []byte
	CreatedAt   time.Time
	PublishedAt *time.Time
	Attempts    int
}

// DiscoveryQuery describes a nearby-people search.
type DiscoveryQuery struct {
	ViewerID      uuid.UUID
	Lat           float64
	Lng           float64
	RadiusKM      float64
	MinAge        int
	MaxAge        int
	Genders       []string
	Limit         int
	Offset        int
	HasLocation   bool
	ExcludeViewed bool
}

type IncomingLike struct {
	Profile   Profile   `json:"profile"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
	Matched   bool      `json:"matched"`
}

type Stats struct {
	IncomingLikes       int `json:"incoming_likes"`
	Matches             int `json:"matches"`
	UnreadMessages      int `json:"unread_messages"`
	UnreadNotifications int `json:"unread_notifications"`
}

type DiscoveryResult struct {
	Profiles         []Profile `json:"profiles"`
	RadiusKM         float64   `json:"radius_km"`
	ExpandedToRadius float64   `json:"expanded_to_radius_km"`
	NextOffset       int       `json:"next_offset"`
	HasMore          bool      `json:"has_more"`
	TotalCandidates  int       `json:"total_candidates"`
}

type AdminStats struct {
	Users         int `json:"users"`
	ActiveUsers   int `json:"active_users"`
	NewUsers24h   int `json:"new_users_24h"`
	Matches       int `json:"matches"`
	Messages      int `json:"messages"`
	Swipes        int `json:"swipes"`
	OpenReports   int `json:"open_reports"`
	Notifications int `json:"notifications"`
	LegalRequests int `json:"legal_requests"`
}

type AdminUserSummary struct {
	ID            uuid.UUID  `json:"id"`
	Email         string     `json:"email"`
	Phone         *string    `json:"phone,omitempty"`
	Role          string     `json:"role"`
	Status        string     `json:"status"`
	EmailVerified bool       `json:"email_verified"`
	DisplayName   string     `json:"display_name"`
	BirthDate     *time.Time `json:"birth_date,omitempty"`
	Gender        string     `json:"gender"`
	City          string     `json:"city"`
	PrimaryPhoto  *string    `json:"primary_photo,omitempty"`
	Photos        int        `json:"photos"`
	Matches       int        `json:"matches"`
	Messages      int        `json:"messages"`
	LastSeenAt    *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type AdminUserDetail struct {
	User        User           `json:"user"`
	Profile     *Profile       `json:"profile"`
	Preferences *Preferences   `json:"preferences,omitempty"`
	Settings    *UserSettings  `json:"settings,omitempty"`
	Counts      map[string]int `json:"counts"`
}

type AdminReport struct {
	Report
	ReporterEmail string `json:"reporter_email"`
	ReporterName  string `json:"reporter_name"`
	TargetEmail   string `json:"target_email"`
	TargetName    string `json:"target_name"`
}

type AuditLog struct {
	ID         uuid.UUID      `json:"id"`
	AdminID    *uuid.UUID     `json:"admin_id,omitempty"`
	Action     string         `json:"action"`
	TargetType string         `json:"target_type"`
	TargetID   string         `json:"target_id"`
	Reason     string         `json:"reason"`
	LegalBasis string         `json:"legal_basis"`
	RequestID  string         `json:"request_id"`
	IP         string         `json:"ip"`
	UserAgent  string         `json:"user_agent"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type LegalRequest struct {
	ID           uuid.UUID  `json:"id"`
	CreatedBy    *uuid.UUID `json:"created_by,omitempty"`
	ApprovedBy   *uuid.UUID `json:"approved_by,omitempty"`
	CaseNumber   string     `json:"case_number"`
	Authority    string     `json:"authority"`
	Country      string     `json:"country"`
	LegalBasis   string     `json:"legal_basis"`
	TargetUserID *uuid.UUID `json:"target_user_id,omitempty"`
	Scopes       []string   `json:"scopes"`
	Status       string     `json:"status"`
	ExportSHA256 string     `json:"export_sha256,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ApprovedAt   *time.Time `json:"approved_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

type LegalExport struct {
	Request    LegalRequest   `json:"request"`
	Data       map[string]any `json:"data"`
	ExportedAt time.Time      `json:"exported_at"`
	SHA256     string         `json:"sha256"`
}
