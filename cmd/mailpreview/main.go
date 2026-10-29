// Command mailpreview renders the transactional email templates into a static
// HTML gallery so they can be reviewed in a browser without sending anything.
//
//	go run ./cmd/mailpreview
//
// The output is docs/email-preview.html.
package main

import (
	"fmt"
	"html"
	"log"
	"os"
	"path/filepath"

	"github.com/loveservice/loveservice/internal/mailer"
)

func main() {
	verification := mailer.VerificationEmail("482913")
	reset := mailer.ResetPasswordEmail("902144")

	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<title>LoveService — transactional email preview</title>
<style>
  body { margin:0; background:#0f1020; color:#f5f5fa; font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif; }
  header { padding:40px 24px 8px; max-width:1000px; margin:0 auto; }
  h1 { margin:0 0 8px; font-size:30px; }
  p.lead { color:#b9b9cc; margin:0 0 28px; line-height:1.6; }
  main { max-width:1000px; margin:0 auto; padding:0 24px 48px; display:grid; gap:32px; }
  section { background:#191a2e; border:1px solid #2a2b45; border-radius:16px; overflow:hidden; }
  section h2 { font-size:14px; letter-spacing:1.6px; text-transform:uppercase; color:#ff9ab8; margin:0; padding:16px 20px; border-bottom:1px solid #2a2b45; }
  iframe { width:100%%; height:820px; border:0; background:#f4f5f7; display:block; }
  code { background:#242540; padding:2px 6px; border-radius:6px; }
</style>
</head>
<body>
<header>
  <h1>LoveService — письма</h1>
  <p class="lead">Так выглядят письма, которые сервис генерирует и отправляет сам через SMTP.
  Файл собран командой <code>go run ./cmd/mailpreview</code>; шаблоны лежат в
  <code>internal/mailer/templates.go</code>.</p>
</header>
<main>
  <section>
    <h2>Подтверждение почты — %s</h2>
    <iframe title="Verification email" srcdoc="%s"></iframe>
  </section>
  <section>
    <h2>Восстановление пароля — %s</h2>
    <iframe title="Password reset email" srcdoc="%s"></iframe>
  </section>
</main>
</body>
</html>
`, html.EscapeString(verification.Subject), html.EscapeString(verification.HTML),
		html.EscapeString(reset.Subject), html.EscapeString(reset.HTML))

	path := filepath.Join("docs", "email-preview.html")
	if err := os.WriteFile(path, []byte(page), 0o644); err != nil {
		log.Fatalf("write %s: %v", path, err)
	}
	fmt.Println("wrote", path)
}
