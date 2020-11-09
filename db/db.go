package db

import (
	"database/sql"
	"regexp"
	"strings"

	"github.com/jmoiron/sqlx"
)

// A DB is an interface to a database.
type DB interface {
	Get(interface{}, string, ...interface{}) error
	Select(interface{}, string, ...interface{}) error
	PrepareNamed(string) (*sqlx.NamedStmt, error)
	Beginx() (*sqlx.Tx, error)
	Exec(string, ...interface{}) (sql.Result, error)
}

// With executes fn with the provided transaction.  If an error is returned,
// the transaction is rolled back;  otherwise, it's committed and the
// potential error from the commit is returned.
func With(tx *sqlx.Tx, fn func(tx *sqlx.Tx) error) error {
	if err := fn(tx); err != nil {
		// XXX: this can return an error too but you probably are more interested
		// in the error caused by your function
		tx.Rollback()
		return err
	}
	return tx.Commit()
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

func Ensure(db DB) error {
	// create all tables if they don't exist
	post := `CREATE TABLE IF NOT EXISTS post (
		id INTEGER PRIMARY KEY,
		title TEXT,
		slug TEXT,
		content TEXT DEFAULT '',
		content_rendered TEXT DEFAULT '',
		summary TEXT DEFAULT '',
		timestamp INTEGER DEFAULT (strftime('%s', 'now')),
		published INTEGER DEFAULT 0
	);`

	postTag := `CREATE TABLE IF NOT EXISTS post_tag (
		post_id INTEGER,
		tag TEXT,
		FOREIGN KEY (post_id) REFERENCES post(id)
	);`

	user := `CREATE TABLE IF NOT EXISTS user (
		id INTEGER PRIMARY KEY,
		username TEXT,
		password TEXT
	);`

	tables := []string{post, postTag, user}
	indexes := []string{}

	var ex []string
	ex = append(ex, tables...)
	ex = append(ex, indexes...)

	for _, stmt := range ex {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	return nil
}
