package auth

import (
	"fmt"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           uint64
	Username     string
	PasswordHash string `db:"password_hash"`
}

var userMigration = monarch.Set{
	Name: "user",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS user (
			id integer NOT NULL PRIMARY KEY,
			username text NOT NULL UNIQUE,
			password_hash text NOT NULL
		);`,
			Down: `DROP TABLE user;`,
		},
	},
}

const bcryptCost = bcrypt.DefaultCost

type UserService struct {
	db db.DB
}

func NewUserService(conn db.DB) *UserService {
	return &UserService{db: conn}
}

// CreateUser attempts to create a new user with the username and password.
// If a user with that username already exists, an error is returned.
func (s *UserService) CreateUser(username, password string) error {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return err
	}
	q := `INSERT INTO user (username, password_hash) VALUES (?, ?);`
	_, err = s.db.Exec(q, username, hashed)
	return err
}

// Validate that the username and password match one in the database.  If
// an error occurs, ok will be false.
func (s *UserService) Validate(username, password string) (ok bool, err error) {
	return s.validate(s.db, username, password)
}

// validate a username and password w/ the provided getter. Returns false when
// validation fails for any reason.
func (s *UserService) validate(db db.Getter, username, password string) (ok bool, err error) {
	var u User
	if err := db.Get(&u, `SELECT * FROM user WHERE username=?`, username); err != nil {
		return false, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return false, err
	}
	return true, nil
}

// ChangePassword changes the user's password to newPassword, providing that
// the current password is correct.
func (s *UserService) ChangePassword(username, currentPassword, newPassword string) (ok bool, err error) {
	err = db.With(s.db, func(tx *sqlx.Tx) error {
		valid, err := s.validate(tx, username, currentPassword)
		if err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("invalid password")
		}
		var u User
		if err := tx.Get(&u, `SELECT * FROM user WHERE username=?`, username); err != nil {
			// user not found
			return err
		}

		newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE user SET password_hash=? WHERE username=?`, string(newHash), username)
		return err
	})

	return err == nil, err
}
