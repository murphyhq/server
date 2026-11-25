package email

import (
	"bytes"
	"context"
	"crypto/x509"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"sync/atomic"
	"time"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/internal/smtp"
	"github.com/murphyhq/server/internal/tracer"
	"github.com/murphyhq/server/services/notifications"
)

type Emails struct {
	roots  *x509.CertPool
	active atomic.Pointer[sender]
}

type sender struct {
	server smtp.Server
	from   *mail.Address
}

var _ notifications.Provider = (*Emails)(nil)

func New(roots *x509.CertPool) *Emails {
	return &Emails{roots: roots}
}

func (e *Emails) Use(ctx context.Context, provider *murphy.EmailProvider, password string) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	from, err := mail.ParseAddress(provider.GetFrom())
	if err != nil {
		return fmt.Errorf("the sender %q isn't an email address", provider.GetFrom())
	}
	next := &sender{from: from, server: smtp.Server{
		Host: provider.GetHost(), Port: int(provider.GetPort()), Username: provider.GetUsername(), Password: password, Roots: e.roots,
	}}
	if err := next.server.Check(ctx); err != nil {
		e.active.CompareAndSwap(nil, next)
		return err
	}
	e.active.Store(next)
	return nil
}

func (e *Emails) Stop() {
	e.active.Store(nil)
}

func (e *Emails) Send(ctx context.Context, notification *murphy.Notification) (err error) {
	ctx, span := tracer.Span(ctx)
	defer span.End(&err)

	current := e.active.Load()
	if current == nil {
		return &notifications.RefusedError{Reason: "Murphy has no email provider"}
	}
	to, err := mail.ParseAddress(notification.GetContactMethod().GetAddress())
	if err != nil {
		return &notifications.RefusedError{Reason: "the contact method isn't an email address"}
	}
	err = current.server.Send(ctx, current.from.Address, to.Address, Message(current.from, to, notification, time.Now()))
	if smtp.Permanent(err) {
		return &notifications.RefusedError{Reason: err.Error()}
	}
	return err
}

func Message(from, to *mail.Address, notification *murphy.Notification, at time.Time) []byte {
	incident := notification.GetIncident()
	title := strings.Join(strings.Fields(incident.GetTitle()), " ")
	headline := fmt.Sprintf("#%d %s", incident.GetNumber(), title)
	subject := "[Murphy] " + headline
	switch notification.GetReason() {
	case murphy.NotificationReason_NOTIFICATION_REASON_ACKNOWLEDGED:
		subject = "[Murphy] Acknowledged: " + headline
	case murphy.NotificationReason_NOTIFICATION_REASON_RESOLVED:
		subject = "[Murphy] Resolved: " + headline
	}
	_, domain, _ := strings.Cut(from.Address, "@")

	var out bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&out, "%s: %s\r\n", name, value) }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", at.Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s@%s>", notification.GetId(), domain))
	header("Auto-Submitted", "auto-generated")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	out.WriteString("\r\n")

	body := quotedprintable.NewWriter(&out)
	lines := []string{
		headline,
		"",
		"Status: " + incident.GetStatus().Text(),
		"Urgency: " + incident.GetUrgency().Text(),
	}
	if notification.GetReason() == murphy.NotificationReason_NOTIFICATION_REASON_PAGED {
		lines = append(lines, "", "Acknowledge it in the Murphy app to stop the escalation.")
	}
	_, _ = body.Write([]byte(strings.Join(lines, "\r\n") + "\r\n"))
	_ = body.Close()
	return out.Bytes()
}
