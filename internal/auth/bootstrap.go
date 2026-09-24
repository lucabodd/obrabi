package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode"
)

// EnsureUsers creates every listed user that does not exist yet, with a random
// password that is printed once to out (the container log). Existing users are
// never touched.
func EnsureUsers(ctx context.Context, store *Store, usernames []string, out io.Writer, log *slog.Logger) error {
	for _, raw := range usernames {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		username, err := NormalizeUsername(raw)
		if err != nil {
			return fmt.Errorf("bootstrap user %q: %w", raw, err)
		}
		_, err = store.ByUsername(ctx, username)
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		password, user, err := CreateWithRandomPassword(ctx, store, username, DefaultDisplayName(username))
		if errors.Is(err, ErrUsernameTaken) {
			continue // another replica created it meanwhile
		}
		if err != nil {
			return err
		}
		log.Info("bootstrap user created", "user_id", user.ID, "username", username)
		PrintCredentials(out, "Usuari creat", username, password)
	}
	return nil
}

// CreateWithRandomPassword creates a user and returns its generated password.
func CreateWithRandomPassword(ctx context.Context, store *Store, username, displayName string) (string, User, error) {
	password := GeneratePassword()
	hash, err := HashPassword(password)
	if err != nil {
		return "", User{}, err
	}
	user, err := store.Create(ctx, username, displayName, hash)
	return password, user, err
}

// ResetPassword gives the user a new random password and returns it.
func ResetPassword(ctx context.Context, store *Store, username string) (string, error) {
	user, err := store.ByUsername(ctx, username)
	if err != nil {
		return "", err
	}
	password := GeneratePassword()
	hash, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	return password, store.SetPassword(ctx, user.ID, hash)
}

// DefaultDisplayName turns "nando" into "Nando".
func DefaultDisplayName(username string) string {
	r := []rune(username)
	if len(r) == 0 {
		return username
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// PrintCredentials writes a banner that stands out in the container log.
func PrintCredentials(out io.Writer, title, username, password string) {
	line := strings.Repeat("=", 64)
	fmt.Fprintf(out, "\n%s\n  %s\n    usuari:      %s\n    contrasenya: %s\n  Canvia-la des de «Compte» després del primer accés.\n%s\n\n",
		line, title, username, password, line)
}
