package auth

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = 12

// Password length limits for passwords chosen by the user. bcrypt only looks
// at the first 72 bytes, so longer passwords are refused instead of silently
// truncated.
const (
	MinPasswordLength = 8
	MaxPasswordBytes  = 72
)

var ErrWeakPassword = errors.New("password too short or too long")

// Alphabet without look-alike characters (0/o, 1/l/i) so the generated
// password is easy to read from a log and to type on a phone.
const passwordAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// GeneratePassword returns a random password such as "k7mw-3xqp-9hte-rv4d"
// (16 symbols from a 31-letter alphabet, about 79 bits of entropy).
func GeneratePassword() string {
	const groups, groupLen = 4, 4
	max := big.NewInt(int64(len(passwordAlphabet)))
	var b strings.Builder
	for g := 0; g < groups; g++ {
		if g > 0 {
			b.WriteByte('-')
		}
		for i := 0; i < groupLen; i++ {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				panic("crypto/rand unavailable: " + err.Error())
			}
			b.WriteByte(passwordAlphabet[n.Int64()])
		}
	}
	return b.String()
}

// HashPassword returns the bcrypt hash of password after checking its length.
func HashPassword(password string) (string, error) {
	if len([]rune(password)) < MinPasswordLength || len(password) > MaxPasswordBytes {
		return "", ErrWeakPassword
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// dummyHash is compared against when the user does not exist, so that login
// takes the same time whether or not the username is valid.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("obrabi-timing-equaliser"), bcryptCost)

// CheckPassword reports whether password matches hash. An empty hash still
// costs one bcrypt comparison.
func CheckPassword(hash, password string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
