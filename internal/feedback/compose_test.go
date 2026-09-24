package feedback

import (
	"strings"
	"testing"
	"time"
)

func TestFingerprintIgnoresNumbers(t *testing.T) {
	a := Fingerprint("web", "Cannot read property of undefined (index 12)", "at render (/js/views/project.js:120:5)")
	b := Fingerprint("web", "Cannot read property of undefined (index 7)", "at render (/js/views/project.js:121:9)")
	if a != b {
		t.Error("errors differing only in numbers should share a fingerprint")
	}
	if a == Fingerprint("gateway", "Cannot read property of undefined (index 7)", "at render (/js/views/project.js:121:9)") {
		t.Error("different sources must not share a fingerprint")
	}
}

func TestComposeEmail(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Madrid")
	uid := int64(1)
	r := Report{
		ID:        42,
		Kind:      KindSuggestion,
		UserID:    &uid,
		Username:  "nando",
		Message:   "Estaria bé poder afegir fotos a les obres",
		Details:   map[string]any{"pagina": "/obres/3"},
		CreatedAt: time.Date(2026, 9, 24, 8, 30, 0, 0, time.UTC),
	}
	subject, body := composeEmail(r, loc)
	if !strings.HasPrefix(subject, "[Obrabi] Suggerimento di Nando") {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{"Estaria bé poder afegir fotos", "pagina: /obres/3", "24/09/2026 10:30", "Report n. 42"} {
		if !strings.Contains(body, want) {
			t.Errorf("body misses %q:\n%s", want, body)
		}
	}
}

func TestBuildMessage(t *testing.T) {
	msg, err := buildMessage("Obrabi <obrabi@example.com>", []string{"luca@example.com"},
		"[Obrabi] Errore\r\nBcc: evil@example.com", "Línia amb accents: à è ò\nsegona línia", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := string(msg)
	if strings.Contains(s, "\r\nBcc:") {
		t.Fatal("header injection through the subject")
	}
	for _, want := range []string{"To: <luca@example.com>", "Content-Transfer-Encoding: quoted-printable", "=C3=A0"} {
		if !strings.Contains(s, want) {
			t.Errorf("message misses %q:\n%s", want, s)
		}
	}
}

func TestLimitDetails(t *testing.T) {
	d := limitDetails(map[string]any{"stack": strings.Repeat("x", 20000), "n": 3, "nested": map[string]any{"a": 1}})
	if got := len([]rune(d["stack"].(string))); got != 8000 {
		t.Errorf("stack truncated to %d runes", got)
	}
	if d["n"] != 3 || d["nested"] != `{"a":1}` {
		t.Errorf("unexpected details %v", d)
	}
}
