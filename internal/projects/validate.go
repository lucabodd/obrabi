package projects

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// ValidationError carries a message meant for the user.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(msg string) error { return &ValidationError{Message: msg} }

// IsValidation reports whether err is a ValidationError and returns it.
func IsValidation(err error) (*ValidationError, bool) {
	var v *ValidationError
	ok := errors.As(err, &v)
	return v, ok
}

// Upper bound for any amount: 100 million euros, far above anything real and
// safely inside int64 when summed.
const maxCents = 100_000_000_00

// singleLine trims s and collapses internal whitespace.
func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func tooLong(s string, max int) bool { return utf8.RuneCountInString(s) > max }

// normDate validates a YYYY-MM-DD date; an empty value becomes today.
func normDate(s string, today string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return today, nil
	}
	d, err := time.Parse("2006-01-02", s)
	if err != nil || d.Year() < 2000 || d.Year() > 2100 {
		return "", invalid("La data no és vàlida.")
	}
	return d.Format("2006-01-02"), nil
}

// NormalizeName validates the name of a town or category.
func NormalizeName(s string, max int, what string) (string, error) {
	s = singleLine(s)
	if s == "" {
		return "", invalid("Escriu el nom " + what + ".")
	}
	if tooLong(s, max) {
		return "", invalid("El nom és massa llarg.")
	}
	return s, nil
}

func (in *ProjectInput) normalize(today string) error {
	in.Name = singleLine(in.Name)
	in.TownName = singleLine(in.TownName)
	in.ClientName = singleLine(in.ClientName)
	in.ClientPhone = singleLine(in.ClientPhone)
	in.Address = singleLine(in.Address)
	in.Notes = strings.TrimSpace(in.Notes)

	switch {
	case in.Name == "":
		return invalid("Posa-li un nom a l'obra.")
	case tooLong(in.Name, 120):
		return invalid("El nom de l'obra és massa llarg.")
	case in.TownID <= 0 && in.TownName == "":
		return invalid("Tria o escriu el poble de l'obra.")
	case tooLong(in.TownName, 80):
		return invalid("El nom del poble és massa llarg.")
	case tooLong(in.ClientName, 120), tooLong(in.ClientPhone, 40), tooLong(in.Address, 200):
		return invalid("Algun camp és massa llarg.")
	case tooLong(in.Notes, 4000):
		return invalid("Les notes són massa llargues.")
	}
	var err error
	in.StartedOn, err = normDate(in.StartedOn, today)
	return err
}

func (in *ItemInput) normalize(today string) error {
	in.Description = singleLine(in.Description)
	in.CategoryName = singleLine(in.CategoryName)

	switch in.Kind {
	case KindExpense:
		if in.CategoryID <= 0 && in.CategoryName == "" {
			return invalid("Tria o escriu una categoria.")
		}
		if tooLong(in.CategoryName, 60) {
			return invalid("El nom de la categoria és massa llarg.")
		}
		in.Hours = nil
	case KindLabor:
		in.CategoryID, in.CategoryName = 0, ""
		if in.Hours != nil {
			h := *in.Hours
			if math.IsNaN(h) || h < 0 || h > 10000 {
				return invalid("Les hores no són vàlides.")
			}
			h = math.Round(h*100) / 100
			in.Hours = &h
		}
	default:
		return invalid("Tipus de partida desconegut.")
	}

	switch {
	case tooLong(in.Description, 200):
		return invalid("La descripció és massa llarga.")
	case in.CostCents < 0 || in.PriceCents < 0:
		return invalid("Els imports no poden ser negatius.")
	case in.CostCents > maxCents || in.PriceCents > maxCents:
		return invalid("L'import és massa gran.")
	case in.CostCents == 0 && in.PriceCents == 0:
		return invalid("Posa almenys un import.")
	}
	var err error
	in.ItemDate, err = normDate(in.ItemDate, today)
	return err
}

func (in *PaymentInput) normalize(today string) error {
	in.Note = singleLine(in.Note)
	switch {
	case in.AmountCents <= 0:
		return invalid("L'import ha de ser més gran que zero.")
	case in.AmountCents > maxCents:
		return invalid("L'import és massa gran.")
	case tooLong(in.Note, 200):
		return invalid("La nota és massa llarga.")
	}
	var err error
	in.PaidOn, err = normDate(in.PaidOn, today)
	return err
}
