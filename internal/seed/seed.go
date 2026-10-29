// Package seed creates an idempotent demo dataset so the mobile app can be
// explored immediately after `docker compose up`.
package seed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/loveservice/loveservice/internal/domain"
	"github.com/loveservice/loveservice/internal/security"
	"github.com/loveservice/loveservice/internal/storage"
	"github.com/loveservice/loveservice/internal/store"
)

const DemoPassword = "LoveDemo123!"

// Technical administrator demo account. It is used to exercise the admin and
// lawful-access endpoints in Swagger/Postman. Change it before any deployment.
const (
	AdminEmail    = "admin@loveservice.app"
	AdminPassword = "AdminDemo123!"
	ModEmail      = "moderator@loveservice.app"
	ModPassword   = "ModeratorDemo123!"
)

type demoUser struct {
	Email      string
	Name       string
	Gender     string
	BirthDate  string
	Bio        string
	Job        string
	Education  string
	HeightCM   int
	Lat        float64
	Lng        float64
	Interests  []string
	City       string
	Country    string
	PhotoColor string
}

var users = []demoUser{
	{"demo@loveservice.app", "Демо", domain.GenderMale, "1995-05-14", "Люблю кофе, велопрогулки и хорошие разговоры. Ищу ту, с кем можно смеяться над всем подряд.", "Product designer", "МГУ", 183, 55.7558, 37.6173, []string{"кофе", "велосипед", "кино", "путешествия"}, "Москва", "Россия", "#ff4d8d"},
	{"anna@loveservice.app", "Анна", domain.GenderFemale, "1998-03-21", "Фотограф, любительница закатов и собак. Покажу лучшие кофейни города.", "Фотограф", "МГИМО", 168, 55.7601, 37.6200, []string{"фотография", "кофе", "собаки", "искусство"}, "Москва", "Россия", "#ff7aa8"},
	{"maria@loveservice.app", "Мария", domain.GenderFemale, "1996-11-02", "Маркетолог днём, танцор сальсы вечером. Давай проверим, совпадают ли наши плейлисты.", "Маркетолог", "ВШЭ", 172, 55.7440, 37.6050, []string{"сальса", "музыка", "вино", "йога"}, "Москва", "Россия", "#c86dd7"},
	{"sofia@loveservice.app", "София", domain.GenderFemale, "1999-07-09", "Обожаю горы, пасты и спонтанные поездки. Ищу человека, с которым не страшно молчать.", "Frontend-разработчик", "МФТИ", 165, 55.7680, 37.6350, []string{"горы", "паста", "код", "путешествия"}, "Москва", "Россия", "#7b61ff"},
	{"elena@loveservice.app", "Елена", domain.GenderFemale, "1994-01-27", "Врач, книголюб и просто хороший человек. Расскажи мне свою любимую книгу.", "Врач", "Сеченовский университет", 170, 55.7300, 37.5800, []string{"книги", "медитация", "плавание"}, "Москва", "Россия", "#ff9f43"},
	{"kate@loveservice.app", "Катя", domain.GenderFemale, "2000-04-15", "Студентка, бариста, художница. Рисую людей и котов.", "Бариста", "РГГУ", 163, 55.7900, 37.6600, []string{"рисование", "кофе", "коты", "музыка"}, "Москва", "Россия", "#ff6b6b"},
	{"polina@loveservice.app", "Полина", domain.GenderFemale, "1997-09-30", "Люблю театр, вино и длинные прогулки без цели.", "Продюсер", "ГИТИС", 175, 55.7100, 37.5500, []string{"театр", "вино", "прогулки"}, "Москва", "Россия", "#4ecdc4"},
	{"dmitry@loveservice.app", "Дмитрий", domain.GenderMale, "1993-12-05", "Бегаю по утрам, готовлю пасту по вечерам. Ищу компанию для всего этого.", "Инженер", "МГТУ", 186, 55.7500, 37.6400, []string{"бег", "готовка", "стартапы"}, "Москва", "Россия", "#576574"},
	{"alex@loveservice.app", "Алексей", domain.GenderMale, "1991-06-18", "Гитарист и путешественник. Сыграю тебе что-нибудь у костра.", "Музыкант", "МГК", 180, 55.7700, 37.5900, []string{"гитара", "походы", "винил"}, "Москва", "Россия", "#2e86de"},
	{"olga@loveservice.app", "Ольга", domain.GenderFemale, "1995-08-22", "Архитектор, люблю свет и пространство. Ищу того, кто разделит воскресные завтраки.", "Архитектор", "МАРХИ", 169, 55.7350, 37.6500, []string{"архитектура", "завтраки", "фотография"}, "Москва", "Россия", "#f368e0"},
}

func Run(ctx context.Context, st *store.Store, media storage.Storage, log *slog.Logger) error {
	hash, err := security.HashPassword(DemoPassword)
	if err != nil {
		return err
	}
	ids := map[string]uuid.UUID{}
	for i, du := range users {
		id, err := ensureUser(ctx, st, media, du, hash)
		if err != nil {
			return fmt.Errorf("seed %s: %w", du.Email, err)
		}
		ids[du.Email] = id
		_ = i
	}
	demo := ids["demo@loveservice.app"]

	// Three mutual likes -> matches with conversations.
	for _, email := range []string{"anna@loveservice.app", "maria@loveservice.app", "sofia@loveservice.app"} {
		peer := ids[email]
		if err := ensureSwipe(ctx, st, demo, peer, domain.ActionLike); err != nil {
			log.Warn("seed swipe failed", "email", email, "error", err)
		}
		if err := ensureSwipe(ctx, st, peer, demo, domain.ActionLike); err != nil {
			log.Warn("seed reciprocal swipe failed", "email", email, "error", err)
		}
	}
	// Waiting likes for the "likes" tab.
	for _, email := range []string{"kate@loveservice.app", "polina@loveservice.app"} {
		if err := ensureSwipe(ctx, st, ids[email], demo, domain.ActionLike); err != nil {
			log.Warn("seed incoming like failed", "email", email, "error", err)
		}
	}

	if err := ensureGreetings(ctx, st, demo); err != nil {
		log.Warn("seed greetings failed", "error", err)
	}
	if err := ensureStaff(ctx, st, log); err != nil {
		log.Warn("seed staff accounts failed", "error", err)
	}
	log.Info("demo seed ready", "account", "demo@loveservice.app", "password", DemoPassword,
		"admin", AdminEmail, "admin_password", AdminPassword)
	return nil
}

// ensureStaff creates the technical administrator and moderator accounts used
// for the administrative / lawful-access APIs.
func ensureStaff(ctx context.Context, st *store.Store, log *slog.Logger) error {
	staff := []struct {
		email, password, name, role string
	}{
		{AdminEmail, AdminPassword, "Technical Admin", domain.RoleAdmin},
		{ModEmail, ModPassword, "Moderator", domain.RoleModerator},
	}
	for _, s := range staff {
		hash, err := security.HashPassword(s.password)
		if err != nil {
			return err
		}
		u, err := st.GetUserByEmail(ctx, s.email)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if u == nil {
			birth, _ := time.Parse("2006-01-02", "1990-01-01")
			u, err = st.CreateUserWithProfile(ctx, store.CreateUserInput{
				Email: s.email, PasswordHash: hash, DisplayName: s.name,
				BirthDate: birth, Gender: domain.GenderOther,
				PreferredGenders: []string{domain.GenderMale, domain.GenderFemale, domain.GenderOther},
			})
			if err != nil {
				return err
			}
			if err := st.SetEmailVerified(ctx, u.ID); err != nil {
				return err
			}
		}
		if u.Role != s.role {
			if err := st.AdminSetUserRole(ctx, u.ID, s.role); err != nil {
				return err
			}
		}
	}
	log.Info("staff accounts ready", "admin", AdminEmail, "moderator", ModEmail)
	return nil
}

func ensureUser(ctx context.Context, st *store.Store, media storage.Storage, du demoUser, hash string) (uuid.UUID, error) {
	u, err := st.GetUserByEmail(ctx, du.Email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return uuid.Nil, err
	}
	if u == nil {
		birth, _ := time.Parse("2006-01-02", du.BirthDate)
		preferred := []string{domain.GenderFemale}
		if du.Gender == domain.GenderFemale {
			preferred = []string{domain.GenderMale}
		}
		u, err = st.CreateUserWithProfile(ctx, store.CreateUserInput{
			Email: du.Email, PasswordHash: hash, DisplayName: du.Name,
			BirthDate: birth, Gender: du.Gender, PreferredGenders: preferred,
		})
		if err != nil {
			return uuid.Nil, err
		}
	}

	if _, err := st.UpdateLocation(ctx, u.ID, du.Lat, du.Lng); err != nil {
		return uuid.Nil, err
	}
	bio, city, country, job, education := du.Bio, du.City, du.Country, du.Job, du.Education
	height := du.HeightCM
	if _, err := st.UpdateProfile(ctx, u.ID, store.ProfilePatch{
		Bio: &bio, City: &city, Country: &country, Job: &job, Education: &education,
		HeightCM: &height, Interests: &du.Interests,
	}); err != nil {
		return uuid.Nil, err
	}
	if err := ensurePhotos(ctx, st, media, u.ID, du); err != nil {
		return uuid.Nil, err
	}
	return u.ID, nil
}

func ensurePhotos(ctx context.Context, st *store.Store, media storage.Storage, userID uuid.UUID, du demoUser) error {
	photos, err := st.ListPhotos(ctx, userID)
	if err != nil {
		return err
	}
	want := 2
	if len(photos) >= want {
		return nil
	}
	slug := strings.Split(strings.Split(du.Email, "@")[0], ".")[0]
	for i := len(photos); i < want; i++ {
		svg := avatarSVG(du.Name, du.PhotoColor, i, du.Interests)
		key := fmt.Sprintf("seed/%s-%d.svg", slug, i)
		url, err := media.Put(ctx, key, bytes.NewReader([]byte(svg)), int64(len(svg)), "image/svg+xml")
		if err != nil {
			return err
		}
		isPrimary := i == 0
		if _, err := st.AddPhoto(ctx, &domain.Photo{
			UserID: userID, URL: url, StorageKey: key, MediaType: domain.MediaPhoto,
			IsPrimary: isPrimary, ModerationStatus: domain.ModerationApproved,
		}); err != nil {
			return err
		}
	}
	return nil
}

func ensureSwipe(ctx context.Context, st *store.Store, swiper, target uuid.UUID, action string) error {
	if _, err := st.GetSwipe(ctx, swiper, target); err == nil {
		return nil
	}
	_, err := st.CreateSwipe(ctx, swiper, target, action)
	if errors.Is(err, domain.ErrAlreadySwiped) {
		return nil
	}
	return err
}

func ensureGreetings(ctx context.Context, st *store.Store, demoID uuid.UUID) error {
	conversations, err := st.ListConversations(ctx, demoID)
	if err != nil {
		return err
	}
	greetings := map[string][]string{
		"Анна":  {"Привет! Твой профиль улыбнул 😊 Как проходит день?", "Привет! Отлично, только что вернулась с прогулки. А ты чем занят?"},
		"Мария": {"Привет! Какие планы на выходные?", "Привет! Думаю про выставку. Составишь компанию?"},
		"София": {"Привет! Ты тоже любишь горы?", "Обожаю! Последний раз была в Дагестане — это было невероятно."},
	}
	for _, conv := range conversations {
		if conv.Peer == nil {
			continue
		}
		msgs, err := st.ListMessages(ctx, conv.ID, nil, 1)
		if err != nil {
			return err
		}
		if len(msgs) > 0 {
			continue
		}
		texts, ok := greetings[conv.Peer.DisplayName]
		if !ok {
			continue
		}
		for _, text := range texts {
			sender := demoID
			if strings.HasPrefix(text, "Привет! Отлично") || strings.HasPrefix(text, "Привет! Думаю") || strings.HasPrefix(text, "Обожаю!") {
				sender = conv.Peer.UserID
			}
			if _, err := st.CreateMessage(ctx, store.CreateMessageInput{
				ConversationID: conv.ID, SenderID: sender, Body: text, Type: domain.MessageText,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func avatarSVG(name, color string, variant int, interests []string) string {
	initials := []rune(name)
	if len(initials) > 1 {
		initials = initials[:1]
	}
	subtitle := ""
	if len(interests) > variant {
		subtitle = html.EscapeString(interests[variant])
	}
	accent := "#2b2d42"
	if variant%2 == 1 {
		accent = "#ffffff"
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="800" height="1000" viewBox="0 0 800 1000">
<defs>
  <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
    <stop offset="0%%" stop-color="%s"/>
    <stop offset="100%%" stop-color="#1b1b2f"/>
  </linearGradient>
  <radialGradient id="glow" cx="50%%" cy="35%%" r="55%%">
    <stop offset="0%%" stop-color="#ffffff" stop-opacity="0.35"/>
    <stop offset="100%%" stop-color="#ffffff" stop-opacity="0"/>
  </radialGradient>
</defs>
<rect width="800" height="1000" fill="url(#g)"/>
<circle cx="400" cy="360" r="260" fill="url(#glow)"/>
<circle cx="400" cy="360" r="150" fill="#ffffff" fill-opacity="0.16"/>
<text x="400" y="330" font-family="Arial, sans-serif" font-size="150" font-weight="700" fill="%s" text-anchor="middle">%s</text>
<text x="400" y="470" font-family="Arial, sans-serif" font-size="56" font-weight="700" fill="#ffffff" text-anchor="middle">%s</text>
<text x="400" y="540" font-family="Arial, sans-serif" font-size="28" fill="#ffffff" fill-opacity="0.75" text-anchor="middle">%s</text>
<path d="M400 700 C340 640 280 700 320 760 C350 805 400 850 400 850 C400 850 450 805 480 760 C520 700 460 640 400 700 Z" fill="#ffffff" fill-opacity="0.9"/>
<text x="400" y="930" font-family="Arial, sans-serif" font-size="24" fill="#ffffff" fill-opacity="0.55" text-anchor="middle">LoveService • demo profile</text>
</svg>`, color, accent, html.EscapeString(string(initials)), html.EscapeString(name), subtitle)
}
