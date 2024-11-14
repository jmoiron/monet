package db

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/jmoiron/sqlx"
)

// A Getter implements the sqlx Get semantics
type Getter interface {
	Get(any, string, ...any) error
}

// A DB is an interface to a database.
type DB interface {
	Get(interface{}, string, ...interface{}) error
	Select(interface{}, string, ...interface{}) error
	Exec(string, ...interface{}) (sql.Result, error)

	PrepareNamed(string) (*sqlx.NamedStmt, error)
	Beginx() (*sqlx.Tx, error)
}

// With executes fn in a new transaction. If an error is returned,
// the transaction is rolled back;  otherwise, it's committed and the
// potential error from the commit is returned.
func With(db DB, fn func(tx *sqlx.Tx) error) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		if err2 := tx.Rollback(); err2 != nil {
			return errors.Join(err2, err)
		}
		return err
	}
	return tx.Commit()
}

type dbKey struct{}

// WithDb adds the db to the context. Fetch with DbFromContext(ctx).
func WithDb(ctx context.Context, db DB) context.Context {
	return context.WithValue(ctx, dbKey{}, db)
}

func DbFromContext(ctx context.Context) DB {
	return ctx.Value(dbKey{}).(DB)
}

func AddDbMiddleware(db DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithDb(r.Context(), db)))
		})
	}
}

var (
	stripspace  = regexp.MustCompile("[^\\w\\s\\-]")
	dashreplace = regexp.MustCompile("[^\\w]+")
)

// Slugify returns a "slug" for some text, which is a string suitable for
// inclusion in a URL.
func Slugify(s string) string {
	s = stripspace.ReplaceAllString(s, "")
	s = dashreplace.ReplaceAllString(s, "-")
	s = strings.Replace(s, "_", "-", -1)
	return strings.ToLower(s)
}
