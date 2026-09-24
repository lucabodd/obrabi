// Package auth implements the identity service: users, credentials and
// passwords. Session tokens are minted by the gateway once this service has
// verified the credentials.
package auth

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lucabodd/obrabi/internal/platform/database"
)

// User is the public view of an account.
type User struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

var (
	ErrNotFound      = errors.New("user not found")
	ErrUsernameTaken = errors.New("username already taken")
	ErrBadUsername   = errors.New("invalid username")
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{2,32}$`)

// NormalizeUsername lower-cases and validates a username.
func NormalizeUsername(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !usernamePattern.MatchString(s) {
		return "", ErrBadUsername
	}
	return s, nil
}

// Store persists users in the auth schema.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

const userColumns = `id, username, display_name, created_at, last_login_at`

func scanUser(row pgx.Row, extra ...any) (User, error) {
	var u User
	dest := append([]any{&u.ID, &u.Username, &u.DisplayName, &u.CreatedAt, &u.LastLoginAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return u, nil
}

// Credentials returns the user and password hash for username.
func (s *Store) Credentials(ctx context.Context, username string) (User, string, error) {
	var hash string
	u, err := scanUser(s.db.QueryRow(ctx,
		`SELECT `+userColumns+`, password_hash FROM auth.users WHERE username = $1`, username), &hash)
	return u, hash, err
}

// PasswordHash returns the password hash of the user with the given id.
func (s *Store) PasswordHash(ctx context.Context, id int64) (string, error) {
	var hash string
	err := s.db.QueryRow(ctx, `SELECT password_hash FROM auth.users WHERE id = $1`, id).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return hash, err
}

// ByID returns the user with the given id.
func (s *Store) ByID(ctx context.Context, id int64) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userColumns+` FROM auth.users WHERE id = $1`, id))
}

// ByUsername returns the user with the given username.
func (s *Store) ByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(s.db.QueryRow(ctx, `SELECT `+userColumns+` FROM auth.users WHERE username = $1`, username))
}

// List returns every user ordered by id.
func (s *Store) List(ctx context.Context) ([]User, error) {
	rows, err := s.db.Query(ctx, `SELECT `+userColumns+` FROM auth.users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// Create inserts a user with an already hashed password.
func (s *Store) Create(ctx context.Context, username, displayName, passwordHash string) (User, error) {
	u, err := scanUser(s.db.QueryRow(ctx,
		`INSERT INTO auth.users (username, display_name, password_hash)
		 VALUES ($1, $2, $3)
		 RETURNING `+userColumns, username, displayName, passwordHash))
	if database.IsUniqueViolation(err) {
		return User{}, ErrUsernameTaken
	}
	return u, err
}

// SetPassword replaces the password hash of a user.
func (s *Store) SetPassword(ctx context.Context, id int64, passwordHash string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE auth.users SET password_hash = $2, password_changed_at = now(), updated_at = now() WHERE id = $1`,
		id, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDisplayName changes the name shown in the interface.
func (s *Store) SetDisplayName(ctx context.Context, id int64, name string) (User, error) {
	return scanUser(s.db.QueryRow(ctx,
		`UPDATE auth.users SET display_name = $2, updated_at = now() WHERE id = $1 RETURNING `+userColumns,
		id, name))
}

// TouchLogin records a successful login.
func (s *Store) TouchLogin(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `UPDATE auth.users SET last_login_at = now() WHERE id = $1`, id)
	return err
}
