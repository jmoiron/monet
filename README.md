Learning Go by writing a blog.  This blog is written in Go1.  Since I am a beginner, the code might not be idiomatic, but I try.

Uses markdown blog posts stored in mongodb, rendered by mustache templates.

Requires:

* github.com/hoisie/web.go
* github.com/hoisie/mustache.go
* github.com/russross/blackfriday
* code.google.com/p/gorilla/sessions
* launchpad.net/mgo

To build, clone repos, `go get` the above requirements, add the repos root to your `$GOPATH`, and `go build monet.go`.

Features:

* simple frontend and backend for blog
* flatpages for static endpoings, eg. "/about/", "/contact/"
* simple configuration system, optional json config file
* cookie based sessions via gorilla
* template pre-compiling

