
export CGO_CFLAGS := -g -O2 -Wno-return-local-addr 
# -Wno-stringop-overflow

build:
	CGO_FLAGS=$CGO_FLAGS go build --tags="fts5"

reload:
	./monet --load-posts backup/posts.json
	./monet --load-stream backup/stream.json
	./monet --load-pages backup/pages.json

fmt:
	goimports -w $(shell git ls-files '*.go')

run:
	reflex -g '*.go' -s -- sh -c "GO_FLAGS=$CGO_FLAGS go build --tags=fts5 && ./monet --config=dev.cfg.json"


