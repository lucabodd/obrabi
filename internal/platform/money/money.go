// Package money formats euro amounts, which Obrabi always stores as integer
// cents to avoid floating point rounding.
package money

import (
	"strconv"
	"strings"
)

// Format renders cents the Valencian way: "1.234,56 €", "-25,00 €".
func Format(cents int64) string {
	return Number(cents) + " €"
}

// Number renders cents without the currency sign: "1.234,56".
func Number(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	euros := cents / 100
	rest := cents % 100

	digits := strconv.FormatInt(euros, 10)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	b.WriteByte(',')
	if rest < 10 {
		b.WriteByte('0')
	}
	b.WriteString(strconv.FormatInt(rest, 10))
	return b.String()
}
