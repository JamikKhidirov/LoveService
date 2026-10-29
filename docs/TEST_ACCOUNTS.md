# Test accounts and seeded data

All seeded accounts use password `LoveDemo123!` unless noted.

| Name | Email | Gender | Role | Notes |
|---|---|---|---|---|
| Демо | `demo@loveservice.app` | male | user | Main demo account: matches, chats, incoming likes |
| Technical Admin | `admin@loveservice.app` | other | admin | Password `AdminDemo123!` |
| Moderator | `moderator@loveservice.app` | other | moderator | Password `ModeratorDemo123!` |
| Анна | `anna@loveservice.app` | female | user | Matched with demo, conversation with greeting |
| Мария | `maria@loveservice.app` | female | user | Matched with demo |
| София | `sofia@loveservice.app` | female | user | Matched with demo |
| Елена | `elena@loveservice.app` | female | user | Discoverable |
| Катя | `kate@loveservice.app` | female | user | Incoming like to demo |
| Полина | `polina@loveservice.app` | female | user | Incoming like to demo |
| Дмитрий | `dmitry@loveservice.app` | male | user | Discoverable |
| Алексей | `alex@loveservice.app` | male | user | Discoverable |
| Ольга | `olga@loveservice.app` | female | user | Discoverable |

All demo profiles are in Moscow with slightly different coordinates, so distance
ordering and radius expansion are visible immediately. Each has two generated SVG
photos uploaded through the storage layer (MinIO in Docker).

## What each account demonstrates

- **demo@** — discovery, likes, matches, chat, notifications, media, privacy export.
- **anna@ / maria@ / sofia@** — reciprocal matches and existing conversations.
- **kate@ / polina@** — pending incoming likes (the "likes" tab).
- **admin@** — every `/api/v1/admin/*` endpoint, including lawful-access requests
  and the audit log.
- **moderator@** — read-only admin surface: stats, user lookup, report triage.

## Resetting demo data

```bash
docker compose down -v
docker compose up -d --build
```

Seeding is idempotent: existing users, photos, swipes and greetings are reused.
