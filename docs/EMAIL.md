# Письма и коды подтверждения

LoveService **сам** генерирует коды и **сам** отправляет письма по SMTP.
Никакие сторонние SMS-агрегаторы или email-API не нужны: код создаётся
криптографическим генератором внутри сервиса, письмо рендерится на сервере и
уходит напрямую в ваш почтовый ящик (Gmail, Яндекс, Mail.ru, корпоративный
Exchange, Resend/SendGrid SMTP и т.д.).

```
POST /auth/register ──► security.NewNumericCode(6)      // crypto/rand, 6 цифр
                        security.HashCode(code)          // в БД только SHA-256
                        mailer.VerificationEmail(code)   // HTML + text
                        SMTP (TLS) ──► почта пользователя
                        ответ 201 (+ dev_code вне production)
```

## Что уже реализовано

| Возможность | Детали |
|---|---|
| Генерация кода | `crypto/rand`, 6 цифр, `internal/security` |
| Хранение | только SHA-256-хеш в `email_codes`, TTL 15 минут |
| Повторная отправка | новый код инвалидирует предыдущий |
| Защита от перебора | не более 5 попыток на код, затем код гасится |
| Письма | HTML + plain-text (`multipart/alternative`), UTF-8 |
| Тема письма | RFC 2047 (кириллица не ломается в любом клиенте) |
| Транспорт | SMTP: STARTTLS (587), implicit TLS (465), local relay |
| Повторные попытки | 3 попытки с backoff, таймаут 15 с на попытку |
| Заголовки | `Date`, `Message-ID`, `X-Mailer`, защита от header injection |
| Dev-режим | письмо пишется в `mail-outbox/*.eml` и логируется |
| Безопасность | пароли/токены/коды никогда не попадают в логи аудита и экспорт |

Шаблоны: `internal/mailer/templates.go`.
Транспорт: `internal/mailer/mailer.go`.
Превью: [`docs/email-preview.html`](email-preview.html) — откройте в браузере.

## Быстрый старт

### 1. Без SMTP (разработка)

Оставьте `SMTP_HOST` пустым. Код вернётся в JSON (`dev_code`) и будет записан в
лог, а готовое письмо — в `mail-outbox/`:

```bash
docker compose up -d --build
curl -sX POST localhost:8080/api/v1/auth/forgot-password \
  -H 'Content-Type: application/json' -d '{"email":"demo@loveservice.app"}'
# {"data":{"dev_code":"123456"}}
ls mail-outbox/            # 20261029-...-demo@loveservice.app.eml
```

Файл `.eml` открывается в Thunderbird/Outlook, а HTML-часть можно посмотреть,
перейдя в [`docs/email-preview.html`](email-preview.html).

### 2. Gmail

1. Включите двухфакторную аутентификацию.
2. Создайте **пароль приложения** (App password) на
   <https://myaccount.google.com/apppasswords>.
3. Пропишите в `.env`:

```dotenv
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_TLS=starttls
SMTP_USER=you@gmail.com
SMTP_PASSWORD=abcd efgh ijkl mnop   # 16 символов пароля приложения
SMTP_FROM=LoveService <you@gmail.com>
```

> Gmail переписывает `From` на адрес аккаунта. Для брендового адреса
> используйте свой домен (см. раздел «Доставляемость»).

### 3. Яндекс 360 / Mail.ru

```dotenv
# Яндекс: порт 465 = implicit TLS
SMTP_HOST=smtp.yandex.ru
SMTP_PORT=465
SMTP_TLS=tls
SMTP_USER=no-reply@yourdomain.ru
SMTP_PASSWORD=пароль-приложения
SMTP_FROM=LoveService <no-reply@yourdomain.ru>
```

```dotenv
# Mail.ru
SMTP_HOST=smtp.mail.ru
SMTP_PORT=465
SMTP_TLS=tls
SMTP_USER=no-reply@yourdomain.ru
SMTP_PASSWORD=пароль-приложения
SMTP_FROM=LoveService <no-reply@yourdomain.ru>
```

### 4. Корпоративный SMTP / релей

```dotenv
SMTP_HOST=mail.internal.example
SMTP_PORT=25
SMTP_TLS=none          # только для доверенной внутренней сети
SMTP_USER=
SMTP_PASSWORD=
SMTP_FROM=LoveService <no-reply@example.com>
```

### 5. Resend / SendGrid / Postmark (SMTP-шлюз)

Любой из них даёт SMTP-креды — сервис работает с ними без изменений кода:

```dotenv
# Resend
SMTP_HOST=smtp.resend.com
SMTP_PORT=587
SMTP_TLS=starttls
SMTP_USER=resend
SMTP_PASSWORD=re_xxxxxxxx

# SendGrid
SMTP_HOST=smtp.sendgrid.net
SMTP_USER=apikey
SMTP_PASSWORD=SG.xxxxxxxx
```

## Переменные окружения

| Переменная | По умолчанию | Назначение |
|---|---|---|
| `SMTP_HOST` | — | пусто = dev-режим (лог + outbox) |
| `SMTP_PORT` | `587` | `587` STARTTLS, `465` implicit TLS, `25` relay |
| `SMTP_USER` | — | логин SMTP |
| `SMTP_PASSWORD` | — | пароль/пароль приложения/API-ключ |
| `SMTP_FROM` | `LoveService <no-reply@loveservice.local>` | отправитель |
| `SMTP_TLS` | `starttls` | `starttls` \| `tls` \| `none` |
| `SMTP_TIMEOUT` | `15s` | таймаут одной попытки |
| `MAIL_OUTBOX_DIR` | — | каталог `.eml` для dev-превью |

В `docker-compose.yml` эти значения пробрасываются из вашего `.env`, а
`MAIL_OUTBOX_DIR=/app/mail-outbox` смонтирован в `./mail-outbox`.

## Как это работает внутри

1. **Генерация** — `security.NewNumericCode(6)` использует `crypto/rand`.
2. **Хранение** — в таблицу `email_codes` пишется только `sha256(code)` и
   `expires_at = now() + 15m`. Старые активные коды помечаются использованными.
3. **Рендер** — `mailer.VerificationEmail(code)` / `ResetPasswordEmail(code)`
   собирают адаптивное письмо (табличная вёрстка, inline-стили, тёмная тема,
   без внешних картинок) и plain-text-версию.
4. **Отправка** — `SMTPMailer.SendMessage` кодирует тему в RFC 2047, собирает
   `multipart/alternative` в quoted-printable и отправляет по SMTP с TLS.
   Отправка асинхронная: регистрация не ждёт почтового сервера, ошибка
   логируется и повторяется (3 попытки).
5. **Проверка** — `POST /auth/verify-email` или `POST /auth/reset-password`
   сравнивает хеш, проверяет TTL и число попыток, гасит код.

### Почему код не хранится в открытом виде

Утечка базы не должна позволять подтверждать чужие адреса или сбрасывать
пароли. Поэтому в БД лежит только хеш, а сам код знает лишь пользователь и
письмо. То же правило применяется к refresh-токенам.

## Доставляемость (чтобы письма не попадали в спам)

1. **Свой домен** — отправляйте с `no-reply@yourdomain.ru`, а не с gmail.com.
2. **SPF** — TXT-запись: `v=spf1 include:_spf.google.com ~all` (или SPF вашего
   провайдера).
3. **DKIM** — включите подпись у почтового провайдера и опубликуйте
   `selector._domainkey.yourdomain.ru`.
4. **DMARC** — `_dmarc.yourdomain.ru`: `v=DMARC1; p=quarantine; rua=mailto:dmarc@yourdomain.ru`.
5. **PTR/A-запись** — если отправляете со своего сервера, обратная зона должна
   совпадать с HELO-именем.
6. Не отправляйте письма с `localhost` в production: `SMTP_FROM` и
   `PUBLIC_BASE_URL` должны указывать на реальный домен.

Проверить письмо: <https://www.mail-tester.com> (оценка 9–10/10 при
SPF+DKIM+DMARC).

## Ограничения и что можно добавить

- Сейчас письма на русском. Язык можно брать из `user_settings.language` и
  добавить шаблоны `en/es/de/fr` — шаблонизатор уже вынесен отдельно.
- Нет unsubscribe-центра: транзакционные письма (коды) в нём не нуждаются, но
  для маркетинговых рассылок его стоит добавить.
- Нет отдельного журнала доставки. Можно писать в таблицу
  `email_deliveries` (to, subject, status, smtp_response) для ретраев и
  разбора жалоб.
- Для масштаба имеет смысл вынести отправку в Kafka-consumer
  (`love.events` → `email.send`), чтобы API не запускал горутины.

## Тесты

```bash
go test ./tests/internal/mailer/... -v
```

Покрыто: рендер шаблонов (код, бренд, plain-text, `lang="ru"`), dev-outbox
`.eml` с `multipart/alternative`, RFC 2047-тема, защита от header injection и
реальная SMTP-доставка через поднятый в тесте SMTP-сервер (проверяются
заголовки, обе MIME-части и код).

## Траблшутинг

| Симптом | Причина и решение |
|---|---|
| `smtp auth: 535` | обычный пароль вместо пароля приложения / включён 2FA без App password |
| `starttls: server does not support STARTTLS` | порт 25/465 при `SMTP_TLS=starttls` — поставьте `tls` или `none` |
| `i/o timeout` | закрыт исходящий порт; проверьте firewall/провайдера, попробуйте 465 |
| Письмо в спаме | нет SPF/DKIM/DMARC или `From` не совпадает с доменом |
| Код не приходит, но ответ 200 | dev-режим: смотрите `mail-outbox/` и логи `love-api` |
| Кириллица в теме «кракозябрами» | обновите сервис: тема кодируется RFC 2047 |
