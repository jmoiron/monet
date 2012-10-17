package app

import (
	"crypto/sha1"
	"fmt"
	"github.com/jmoiron/monet/db"
	"io"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
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

func init() {
	db.Register(&User{})
}
