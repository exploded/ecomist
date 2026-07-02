package auth

import (
	"fmt"
	"log/slog"
	"net/smtp"
	"os"
	"strings"
)

// SMTPConfigured reports whether outbound email is set up.
func SMTPConfigured() bool { return os.Getenv("SMTP_HOST") != "" }

// SendVerificationEmail emails the confirm-your-address link. When SMTP is not
// configured the link is logged instead, so local/dev setups still work - the
// register handler additionally surfaces the link on-screen in DevMode.
func SendVerificationEmail(toEmail, toName, link string) error {
	subject := "Confirm your email for Ecomist"
	body := fmt.Sprintf(`G'day%s,

Someone (hopefully you) signed up for the Ecomist run-sheet app with this
email address. To confirm it's yours, open this link:

%s

The link is valid for 48 hours. If this wasn't you, just ignore this email.
`, greetName(toName), link)

	if !SMTPConfigured() {
		slog.Info("SMTP not configured - verification link", "email", toEmail, "link", link)
		return nil
	}

	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("EMAIL_FROM")
	if from == "" {
		from = user
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "From: Ecomist <%s>\r\n", from)
	fmt.Fprintf(&msg, "To: %s\r\n", toEmail)
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))

	auth := smtp.PlainAuth("", user, pass, host)
	return smtp.SendMail(host+":"+port, auth, from, []string{toEmail}, []byte(msg.String()))
}

func greetName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	first, _, _ := strings.Cut(name, " ")
	return " " + first
}
