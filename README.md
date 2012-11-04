## Preface

The code in this repository has powered [jmoiron.net](http://jmoiron.net)
since Apr 2012.

Writing this application was a learning experience and as such the code in this
repository might not be the best example of idiomatic go, but I try:

 * the package is `go get` able
 * all code follows `go fmt` default conventions

## Features

`monet` runs a website with:

* a clean blog with search, archive, admin w/ ajax preview
* a simple flatpage system for one-off URLs (ie. `/about/`)
* a front-end for twitter and github statuses (updater is currently in python).

The blog and flatpages are written in markdown and stored in mongodb.  The site
itself is rendered via [mandira templates](http://jmoiron.github.com/mandira), 
which when the `Debug` configuration option is `false` get cached.

## How do I run this?

First, go get this repos:

    go get github.com/jmoiron/monet

This will install the `monet` command, which is a webserver that takes a config
file as an argument (defaults to `./config.json`) and runs a webserver on the
configured port.  Here is a sample config.json:

```javascript
{
    "SessionSecret": "(long random string here)",
    "WebPort": 8000,
    "TemplatePreCompile": true,
    "TemplatePaths": ["(path to templates)"],
    "Debug": false,
    "GoogleAnalyticsTrackingID": "UA-(your GA id)",
    "Streams": [{
            "type": "twitter",
            "user_id": "(your twitter user_id)"
        }, {
            "type": "github",
            "username": "jmoiron",
            "token": "(your github user token)"
        }
    ],

    "Gallery": {
        "Type": "picasa",
        "UserID": "(your picasa user id)"
    }
}
```

If you have mongodb running on the default port and the localhost, you should
now be able to run `monet` and hit your site on port 8000.

### TODO:

 * document in a way that go pkgdoc will work
 * make styles less monolithic, separate structure from character
 * load templates from monet's install path if no template paths are provided
   in config, which makes it easier to run monet from anywhere
 * take some things like port, config path, etc on command line
 * move things like the title & subtitle into the config
 * most of the gallery app

