package feedback

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Mailer sends plain-text e-mails.
type Mailer interface {
	Send(ctx context.Context, subject, body string) error
}

// SMTPConfig configures delivery through an SMTP server. For Google
// Workspace/Gmail use smtp.gmail.com:587 with the account address as
// Username and an App Password as Password.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
}

// Enabled reports whether enough is configured to send e-mails.
func (c SMTPConfig) Enabled() bool {
	return c.Host != "" && c.Port > 0 && c.From != "" && len(c.To) > 0
}

// Validate checks the addresses.
func (c SMTPConfig) Validate() error {
	if _, err := mail.ParseAddress(c.From); err != nil {
		return fmt.Errorf("SMTP_FROM: %w", err)
	}
	for _, to := range c.To {
		if _, err := mail.ParseAddress(to); err != nil {
			return fmt.Errorf("OBRABI_REPORT_EMAIL_TO %q: %w", to, err)
		}
	}
	return nil
}

// SMTPMailer delivers through net/smtp, using implicit TLS on port 465 and
// mandatory STARTTLS otherwise (credentials are never sent in clear text).
type SMTPMailer struct {
	cfg SMTPConfig
}

func NewSMTPMailer(cfg SMTPConfig) *SMTPMailer { return &SMTPMailer{cfg: cfg} }

func (m *SMTPMailer) Send(ctx context.Context, subject, body string) error {
	msg, err := buildMessage(m.cfg.From, m.cfg.To, subject, body, time.Now())
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	tlsConfig := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	if m.cfg.Port == 465 {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer client.Close()

	if m.cfg.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else if m.cfg.Username != "" {
			return errors.New("the SMTP server does not offer STARTTLS; refusing to send the password in clear text")
		}
	}
	if m.cfg.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	from, _ := mail.ParseAddress(m.cfg.From)
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, to := range m.cfg.To {
		addr, _ := mail.ParseAddress(to)
		if err := client.Rcpt(addr.Address); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", addr.Address, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return client.Quit()
}

// buildMessage renders an RFC 5322 message with a UTF-8, quoted-printable
// plain-text body.
func buildMessage(from string, to []string, subject, body string, now time.Time) ([]byte, error) {
	fromAddr, err := mail.ParseAddress(from)
	if err != nil {
		return nil, err
	}
	if fromAddr.Name == "" {
		fromAddr.Name = "Obrabi"
	}
	var rcpts []string
	for _, t := range to {
		a, err := mail.ParseAddress(t)
		if err != nil {
			return nil, err
		}
		rcpts = append(rcpts, a.String())
	}
	var id [12]byte
	_, _ = rand.Read(id[:])
	domain := "obrabi.local"
	if at := strings.LastIndex(fromAddr.Address, "@"); at >= 0 {
		domain = fromAddr.Address[at+1:]
	}

	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", fromAddr.String())
	header("To", strings.Join(rcpts, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", sanitizeHeader(subject)))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", fmt.Sprintf("<%s@%s>", hex.EncodeToString(id[:]), domain))
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")

	qp := quotedprintable.NewWriter(&b)
	normalized := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	if _, err := qp.Write([]byte(normalized)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// sanitizeHeader removes line breaks (header injection) and trims length.
func sanitizeHeader(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 150 {
		s = string(r[:149]) + "…"
	}
	return s
}
