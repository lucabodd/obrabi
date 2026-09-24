package feedback

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeSMTP accepts one message without TLS or auth and returns what it got.
func fakeSMTP(t *testing.T) (addr string, got <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan string, 1)
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := func(s string) { conn.Write([]byte(s + "\r\n")) }
		w("220 fake ESMTP")
		var transcript strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				out <- transcript.String()
				return
			}
			transcript.WriteString(line)
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case inData:
				if strings.TrimRight(line, "\r\n") == "." {
					inData = false
					w("250 OK queued")
				}
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				w("250-fake greets you")
				w("250 8BITMIME")
			case strings.HasPrefix(cmd, "MAIL FROM"), strings.HasPrefix(cmd, "RCPT TO"):
				w("250 OK")
			case cmd == "DATA":
				inData = true
				w("354 go ahead")
			case cmd == "QUIT":
				w("221 bye")
				out <- transcript.String()
				return
			default:
				w("250 OK")
			}
		}
	}()
	return ln.Addr().String(), out
}

func TestSMTPMailerDelivers(t *testing.T) {
	addr, got := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	m := NewSMTPMailer(SMTPConfig{Host: host, Port: port, From: "obrabi@example.com", To: []string{"luca@example.com"}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Send(ctx, "[Obrabi] Prova", "Hola Luca,\nprova de correu."); err != nil {
		t.Fatalf("Send: %v", err)
	}
	transcript := <-got
	for _, want := range []string{"MAIL FROM:<obrabi@example.com>", "RCPT TO:<luca@example.com>", "Subject: [Obrabi] Prova", "prova de correu."} {
		if !strings.Contains(transcript, want) {
			t.Errorf("transcript misses %q:\n%s", want, transcript)
		}
	}
}

func TestSMTPMailerRefusesPlainAuth(t *testing.T) {
	addr, _ := fakeSMTP(t) // offers no STARTTLS
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	m := NewSMTPMailer(SMTPConfig{Host: host, Port: port, Username: "u", Password: "secret", From: "obrabi@example.com", To: []string{"luca@example.com"}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := m.Send(ctx, "x", "y")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("expected a refusal to send credentials without TLS, got %v", err)
	}
}
