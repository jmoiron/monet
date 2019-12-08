package auth

import (
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/jmoiron/monet/db"
)

// FIXME: necessary?
type User struct {
	ID       uint64
	Username string
	Password string
}

type AuthService struct {
	db db.DB
}

func NewAuthService(conn db.DB) *AuthService {
	return &AuthService{db: conn}
}

// CreateUser attempts to create a new user with the username and password.
// If a user with that username already exists, an error is returned.
func (s *AuthService) CreateUser(username, password string) error {
	password = hashPassword(password)
	q := `INSERT INTO user (username, password) VALUES (?, ?);`
	_, err := s.db.Exec(q, username, password)
	return err
}

// Validate that the username and password match one in the database.  If
// an error occurs, ok will be false.
func (s *AuthService) Validate(username, password string) (ok bool, err error) {
	password = hashPassword(password)
	q := `SELECT * FROM user WHERE username=? AND password=?;`
	var u User
	err = s.db.Get(&u, q, username, password)
	// FIXME: differentiate validation failure from db failure
	return err == nil, err
}

func (s *AuthService) ChangePassword(username, oldpw, newpw string) (ok bool, err error) {
	oldpw = hashPassword(oldpw)
	newpw = hashPassword(newpw)
	q := `UPDATE user SET password=? WHERE username=? AND password=?;`
	res, err := s.db.Exec(q, newpw, username, oldpw)
	if err != nil {
		return false, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

// hashPassword returns the hex formatted cryptographic hash (sha256) of password.
func hashPassword(password string) string {
	hash := sha256.New()
	io.WriteString(hash, password)
	return fmt.Sprintf("%x", hash.Sum(nil))
}
