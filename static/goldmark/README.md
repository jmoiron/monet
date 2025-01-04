# Goldmark WASM Program

Source code for running Goldmark through WASM.

This exports a similar interface to the [goldmark playground](https://yuin.github.io/goldmark/playground/), but without options.

The default `Makefile` builder is to use [tinygo](https://tinygo.org/docs/guides/webassembly/wasm/). The build works, but the engine fails with [known stack overflow](https://github.com/tinygo-org/tinygo/issues/3748) due to the way that the `regexp` library works.

That target is retained, but for now, `make go` will produce a wasm file with the active build chain.  The `wasm_exec.js` file must be copied from the version that made the build.

For go, that is:

```sh
$ $(go env GOROOT)/misc/wasm/wasm_exec.js ./
```

For `tinygo`, that is:

```sh
$ cp $(tinygo env TINYGOROOT)/targets/wasm_exec.js wasm_exec_tiny.js
```
