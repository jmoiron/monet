package app

import (
	"crypto/sha1"
	"fmt"
	"github.com/jmoiron/monet/db"
	"io"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
	"regexp"
	"strings"
)

// -- Standard Models --

type User struct {
	Id       bson.ObjectId "_id"
	Username string
	Password string
}

func (u *User) Indexes() [][]string { return [][]string{[]string{"username"}} }
func (u *User) Collection() string  { return "users" }
func (u *User) Unique() bson.M      { return bson.M{"username": u.Username} }
func (u *User) PreSave()            {}

// -- Users --

func CreateUser(username, password string) error {
	hash := sha1.New()
	io.WriteString(hash, password)
	user := new(User)
	user.Username = username
	user.Password = fmt.Sprintf("%x", hash.Sum(nil))
	_, err := db.Upsert(user)
	return err
}

func ValidateUser(username, password string) bool {

	hash := sha1.New()
	io.WriteString(hash, password)
	user := new(User)
	hashstr := fmt.Sprintf("%x", hash.Sum(nil))
	err := db.Find(user, bson.M{"username": username}).One(&user)
	if err == mgo.ErrNotFound {
		return false
	}
	if user.Password != hashstr {
		return false
	}
	return true
}

// -- Utilities --

var (
	stripspace  = regexp.MustCompile("[^\\w\\s\\-]")
	dashreplace = regexp.MustCompile("[^\\w]+")
)

// Slugify some text.  Do not strip words as django does, but do collapse
// spaces and use dashes in favor of all other non-alphanum characters.
func Slugify(text string) string {
	s := text
	s = stripspace.ReplaceAllString(s, "")
	s = dashreplace.ReplaceAllString(s, "-")
	s = strings.Replace(s, "_", "-", -1)
	return strings.ToLower(s)
}

func init() {
	db.Register(&User{})
}
