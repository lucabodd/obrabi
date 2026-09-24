package feedback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var numbers = regexp.MustCompile(`\d+`)

// Fingerprint identifies "the same error": same source, same message once
// numbers are masked (ids, indexes, line numbers vary) and same location.
func Fingerprint(source, message, where string) string {
	norm := func(s string) string {
		return numbers.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "#")
	}
	sum := sha256.Sum256([]byte(source + "\x00" + norm(message) + "\x00" + norm(where)))
	return hex.EncodeToString(sum[:16])
}

// truncate shortens s to max runes.
func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// limitDetails caps every string value and the overall size of details, so
// that a misbehaving client cannot store megabytes per report.
func limitDetails(d map[string]any) map[string]any {
	out := make(map[string]any, len(d))
	for k, v := range d {
		if len(out) >= 40 {
			break
		}
		k = truncate(k, 60)
		switch t := v.(type) {
		case string:
			out[k] = truncate(t, 8000)
		case nil, bool, float64, int, int64:
			out[k] = t
		default:
			b, err := json.Marshal(t)
			if err != nil {
				continue
			}
			out[k] = truncate(string(b), 2000)
		}
	}
	return out
}

func displayName(username string) string {
	if username == "" {
		return "Qualcuno"
	}
	r := []rune(username)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// composeEmail renders the notification for a report. The e-mails go to the
// maintainer (Luca) and are therefore written in Italian.
func composeEmail(r Report, loc *time.Location) (subject, body string) {
	who := displayName(r.Username)
	short := truncate(strings.Join(strings.Fields(r.Message), " "), 70)

	var intro string
	switch r.Kind {
	case KindSuggestion:
		subject = fmt.Sprintf("[Obrabi] Suggerimento di %s: %s", who, short)
		intro = fmt.Sprintf("%s ha inviato un suggerimento da Obrabi.", who)
	case KindBug:
		subject = fmt.Sprintf("[Obrabi] %s ha segnalato un problema: %s", who, short)
		intro = fmt.Sprintf("%s ha segnalato un problema da Obrabi.", who)
	default:
		src := r.Source
		if src == "" {
			src = "sconosciuto"
		}
		subject = fmt.Sprintf("[Obrabi] Errore automatico (%s): %s", src, short)
		intro = fmt.Sprintf("Obrabi ha registrato un errore nel servizio «%s».", src)
	}

	var b strings.Builder
	b.WriteString(intro)
	b.WriteString("\n\nMessaggio:\n")
	b.WriteString(r.Message)
	b.WriteString("\n")

	var stack string
	if len(r.Details) > 0 {
		keys := make([]string, 0, len(r.Details))
		for k := range r.Details {
			if k == "stack" {
				stack = fmt.Sprint(r.Details[k])
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			b.WriteString("\nDettagli:\n")
			for _, k := range keys {
				fmt.Fprintf(&b, "  %s: %v\n", k, r.Details[k])
			}
		}
	}

	b.WriteString("\n")
	if r.Username != "" {
		fmt.Fprintf(&b, "Utente: %s (%s)\n", who, r.Username)
	}
	fmt.Fprintf(&b, "Data: %s\n", r.CreatedAt.In(loc).Format("02/01/2006 15:04"))
	if r.Kind == KindError && r.Occurrences > 1 {
		fmt.Fprintf(&b, "Occorrenze: %d (ultima %s)\n", r.Occurrences, r.LastSeenAt.In(loc).Format("02/01/2006 15:04"))
	}
	fmt.Fprintf(&b, "Report n. %d\n", r.ID)

	if stack != "" {
		b.WriteString("\nStack trace:\n")
		b.WriteString(stack)
		b.WriteString("\n")
	}
	b.WriteString("\n— Obrabi\n")
	return subject, b.String()
}
