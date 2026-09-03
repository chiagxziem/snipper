package mailer

import (
	"context"
	"embed"
	"fmt"
	"time"
)

//go:embed templates/*.html
var templates embed.FS

type Mailer interface {
	SendEmail(ctx context.Context, to []string, subject, html string) error
	SendVerificationEmail(ctx context.Context, to []string, name, token string) error
	SendPasswordResetEmail(ctx context.Context, to []string, name, token string) error
	SendWaitlistNotification(ctx context.Context, to []string, name, tierName, eventName string, expiresAt time.Time) error
}

func getFrom(domain string) string {
	return fmt.Sprintf("Gater <gater@%s>", domain)
}
