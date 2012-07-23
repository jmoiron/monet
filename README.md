### Preface

The code in this repository powers [jmoiron.net](http://jmoiron.net).

Writing this application was a learning experience and as such the code in this
repository might not always reflect the latest golang idioms, but I try to do
things properly.  This includes:

* the package is `go get` able
* all code follows `go fmt` default conventions

### Features

`monet` provides a website with:

* a clean blog with search, archive, admin w/ ajax preview
* a simple flatpage system for one-off URLs (ie. `/about/`)
* a front-end for twitter and github statuses (updater is currently in python).

The blog and flatpages are written in markdown and stored in mongodb.  The site
itself is rendered via mustache templates, which when the `Debug` configuration
option is `false` get cached.

### Building

To build:

    go get github.com/jmoiron/monet/

This will install the `monet` command, which is a webserver that takes a config
file as an argument (defaults to `./config.json`) and runs a webserver on the
configured port.

Monet depends on:

* github.com/hoisie/web
* github.com/hoisie/mustache
* github.com/russross/blackfriday
* code.google.com/p/gorilla/sessions
* labix.org/v1/mgo

