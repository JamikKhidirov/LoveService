package mailer

import (
	"fmt"
	"html"
	"strings"
	"time"
)

// Message is a rendered transactional email. HTML is the rich version, Text is
// the plain-text fallback shown by clients that block HTML.
type Message struct {
	Subject string
	HTML    string
	Text    string
}

const (
	brandName    = "LoveService"
	supportEmail = "support@loveservice.app"
	codeTTL      = "15 минут"
)

// VerificationEmail renders the "confirm your email" letter.
func VerificationEmail(code string) Message {
	intro := "Спасибо за регистрацию в LoveService. Введите этот код, чтобы подтвердить адрес почты и открыть доступ ко всем функциям сервиса."
	return Message{
		Subject: "LoveService: код подтверждения почты",
		HTML:    renderLayout("Подтвердите почту", "Ваш код подтверждения", intro, code, "Код действует 15 минут. Если вы не регистрировались в LoveService, просто проигнорируйте это письмо."),
		Text:    renderText("Подтвердите почту", intro, code, "Код действует 15 минут. Если вы не регистрировались в LoveService, просто проигнорируйте это письмо."),
	}
}

// ResetPasswordEmail renders the "reset your password" letter.
func ResetPasswordEmail(code string) Message {
	intro := "Мы получили запрос на восстановление пароля. Введите код ниже, чтобы задать новый пароль."
	return Message{
		Subject: "LoveService: восстановление пароля",
		HTML:    renderLayout("Восстановление пароля", "Код для сброса пароля", intro, code, "Код действует 15 минут. Если вы не запрашивали сброс пароля, ничего не делайте — пароль останется прежним."),
		Text:    renderText("Восстановление пароля", intro, code, "Код действует 15 минут. Если вы не запрашивали сброс пароля, ничего не делайте — пароль останется прежним."),
	}
}

// renderLayout builds a responsive, table-based HTML email. Everything is
// inline-styled because most mail clients strip <style> blocks. The layout is
// 600px wide, works in light and dark mode and has no external images, so it
// renders even when remote content is blocked.
func renderLayout(title, preheader, intro, code, note string) string {
	year := time.Now().UTC().Year()
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="supported-color-schemes" content="light dark">
<title>%[1]s</title>
</head>
<body style="margin:0;padding:0;background:#f4f5f7;">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;">%[2]s</div>
<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="background:#f4f5f7;padding:32px 12px;">
  <tr>
    <td align="center">
      <table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" style="width:100%%;max-width:600px;background:#ffffff;border-radius:20px;overflow:hidden;box-shadow:0 12px 40px rgba(20,20,40,0.10);font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
        <tr>
          <td style="background:linear-gradient(135deg,#ff2d75 0%%,#ff6f61 55%%,#ffb347 100%%);padding:34px 40px;">
            <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0">
              <tr>
                <td style="font-size:22px;font-weight:700;letter-spacing:0.4px;color:#ffffff;">LoveService</td>
                <td align="right" style="font-size:12px;color:#ffe3ec;letter-spacing:1.6px;text-transform:uppercase;">dating&nbsp;backend</td>
              </tr>
            </table>
            <div style="margin-top:22px;font-size:26px;line-height:1.25;font-weight:700;color:#ffffff;">%[3]s</div>
          </td>
        </tr>
        <tr>
          <td style="padding:36px 40px 8px 40px;">
            <p style="margin:0 0 20px 0;font-size:16px;line-height:1.6;color:#3b3b4f;">%[4]s</p>
            <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0">
              <tr>
                <td align="center" style="background:#fff1f6;border:1px solid #ffd0e0;border-radius:16px;padding:22px 12px;">
                  <div style="font-size:12px;letter-spacing:2px;text-transform:uppercase;color:#c2185b;margin-bottom:8px;">Ваш код</div>
                  <div style="font-family:'SFMono-Regular',Consolas,'Liberation Mono',Menlo,monospace;font-size:38px;line-height:1.1;font-weight:700;letter-spacing:10px;color:#1d1d2b;padding-left:10px;">%[5]s</div>
                </td>
              </tr>
            </table>
            <p style="margin:22px 0 0 0;font-size:14px;line-height:1.6;color:#6b6b80;">%[6]s</p>
          </td>
        </tr>
        <tr>
          <td style="padding:28px 40px 36px 40px;">
            <div style="border-top:1px solid #ececf2;padding-top:22px;font-size:13px;line-height:1.7;color:#8a8a9e;">
              Нужна помощь? Напишите на <a href="mailto:%[7]s" style="color:#e91e63;text-decoration:none;">%[7]s</a>.<br>
              Это автоматическое письмо, отвечать на него не нужно.
            </div>
          </td>
        </tr>
        <tr>
          <td style="background:#fafafc;padding:20px 40px;border-top:1px solid #f0f0f5;">
            <div style="font-size:12px;line-height:1.7;color:#9a9aae;">
              © %[8]d LoveService. Сервис знакомств.<br>
              Вы получили это письмо, потому что указали этот адрес при регистрации или восстановлении пароля.
            </div>
          </td>
        </tr>
      </table>
      <div style="max-width:600px;margin:18px auto 0 auto;font-size:11px;line-height:1.6;color:#a6a6b8;text-align:center;">
        LoveService никогда не просит сообщать код по телефону или в чате.
      </div>
    </td>
  </tr>
</table>
</body>
</html>`,
		html.EscapeString(title), html.EscapeString(preheader), html.EscapeString(title),
		html.EscapeString(intro), html.EscapeString(code), html.EscapeString(note),
		supportEmail, year)
}

func renderText(title, intro, code, note string) string {
	var b strings.Builder
	b.WriteString(brandName + "\r\n")
	b.WriteString(strings.Repeat("=", len(brandName)) + "\r\n\r\n")
	b.WriteString(title + "\r\n\r\n")
	b.WriteString(intro + "\r\n\r\n")
	b.WriteString("Ваш код: " + code + "\r\n")
	b.WriteString("Действует: " + codeTTL + "\r\n\r\n")
	b.WriteString(note + "\r\n\r\n")
	b.WriteString("Поддержка: " + supportEmail + "\r\n")
	b.WriteString("Это автоматическое письмо, отвечать на него не нужно.\r\n")
	return b.String()
}

// htmlToText is a small fallback used when a caller sends raw HTML without a
// plain-text alternative. It keeps the message readable instead of shipping
// markup to text-only clients.
func htmlToText(body string) string {
	replacer := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n\n", "</div>", "\n", "</tr>", "\n", "</h1>", "\n\n", "</h2>", "\n\n",
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
	)
	text := replacer.Replace(body)
	var out strings.Builder
	inTag := false
	for _, r := range text {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	lines := strings.Split(out.String(), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	return strings.Join(cleaned, "\n")
}
