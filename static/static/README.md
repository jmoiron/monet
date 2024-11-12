# README

These static files use [lesscss](https://lesscss.org/).

You can install it with:

```sh
$ sudo npm install -g less
$ sudo npm install -g less-plugin-clean-css
```

From here, you can compile the `style.css` like this:

```sh
$ lessc --clean-css style.less style.css
```

Less makes it easy to do includes and to specify nested rules.