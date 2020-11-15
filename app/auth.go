package app

import (
	"net/http"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/monarch"
	"github.com/jmoiron/monet/sunrise"
	"github.com/jmoiron/monet/template"
	"golang.org/x/crypto/bcrypt"
)

type AdminPanel interface {
	Render() string
}

var Panels = []AdminPanel{}

func AddPanel(p AdminPanel) {
	Panels = append(Panels, p)
}

var adminBase = template.Base{Path: "admin/base.mandira"}
var CookieStore = sessions.NewCookieStore([]byte(conf.Config.SessionSecret))

const SessionJar = "monet-session"

func AttachAdmin(url string) {
	// auth
	// users
	/*  too much unnecessary work?
	    web.Get(url + "users/", userList)
	    web.Get(url + "users/edit/(.*)", userEdit)
	    web.Get(url + "users/delete/(.*)", userDelete)
	    web.Get(url + "users/add/", userAdd)
	    web.Post(url + "users/add/", userAddPost)

	web.Get(url, adminIndex)
	*/
}

// A User is someone who is authorized to write to the system.  It's up
// to apps to determine if/how to handle access control.  My needs are
// simple so there will be only one user and no access control.
type User struct {
	Username     string
	PasswordHash string `db:password_hash`
}

// UserAuth is an application that provides auth and user management.
type UserAuth struct {
	db       monarch.DB
	base     string
	loginURL string
}

// NewUserAuth returns a new application that manages users and authetication.
func NewUserAuth(db monarch.DB) *UserAuth {
	return &UserAuth{db: db}
}

// Attach UserAuth.  The base passed to this function becomes
// the UserAuth base.
func (u *UserAuth) Attach(r *mux.Router, base string) error {
	get := r.PathPrefix(base).Methods("GET").Subrouter()
	post := r.PathPrefix(base).Methods("POST").Subrouter()

	get.HandleFunc("/login/", u.login)
	get.HandleFunc("/logout/", u.logout)

	post.HandleFunc("/login/", u.login)

	u.base = base
	u.loginURL = filepath.Join(u.base, "/login/")

	return nil
}

func (u *UserAuth) Migrate() error {
	manager, err := monarch.NewManager(u.db)
	if err != nil {
		return nil
	}

	migrations := []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS user (
				id INTEGER PRIMARY KEY,
				username string,
				password_hash string,
			);`,
			Down: `DROP TABLE user;`,
		},
	}

	return manager.Upgrade(monarch.Set{
		Name:       "auth",
		Migrations: migrations,
	})
}

func (u *UserAuth) Admin() (sunrise.Admin, error) {
	return nil, nil
}

// Authenticated returns false if the user is not authenticated.  When
// the user is not authenticated, they're automatically redirected to
// the admin login URL.
//
// if !u.Authenticated(w, req) { return }
//
func (u *UserAuth) Authenticated(w http.ResponseWriter, req *http.Request) bool {
	session, _ := CookieStore.Get(req, SessionJar)

	// TODO: forward URL to go back to where you wanted to go
	if session.Values["authenticated"] != true {
		http.Redirect(w, req, u.loginURL, 302)
		return false
	}
	return true
}

func (u *UserAuth) login(w http.ResponseWriter, req *http.Request) {
	// if we're trying to log in, validate
	session, _ := CookieStore.Get(req, SessionJar)

	if req.Method == "POST" {
		username, password := req.Form.get("username"), req.Form.Get("password")
		if validate(username, password) {
			session.Values["authenticated"] = true
			session.Values["user"] = username
			session.Save(req, w)
			http.Redirect(w, req, u.base, 302)
		} else {
			session.Flash("invalid username or password")
		}
	}

	return adminBase.Render("admin/login.mandira", map[string]interface{}{
		"login": true,
	})

}

func (u *UserAuth) logout(w http.ResponseWriter, req *http.Request) {
	session, _ := CookieStore.Get(ctx.Request, "monet-session")
	session.Values["authenticated"] = false
	session.Values["user"] = ""
	session.Save(req, w)
	http.Redirect(w, req, u.loginURL, 302)
	return ""

}

/*
func adminIndex() string {
	if RequireAuthentication(ctx) {
		return ""
	}

	return adminBase.Render("admin/index.mandira", M{
		"Panels": Panels,
	})
}
*/

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword(password, 13)
	return string(bytes), err
}

// CheckPassword checks password against the hash.  It returns true if the
// password matches the hash (ie, if we hash the password and its the same).
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

/*
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
*/
