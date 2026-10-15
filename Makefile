
export CGO_CFLAGS := -g -O2 -Wno-return-local-addr 
# -Wno-stringop-overflow

.PHONY: all build test reload fmt run

all:
	$(MAKE) --no-print-directory -C static/static
	$(MAKE) --no-print-directory -C static/static/fa
	$(MAKE) --no-print-directory build

build:
	go build --tags="fts5"

test:
	go test --tags="fts5" ./...

reload:
	./monet --load-posts backup/posts.json
	./monet --load-stream backup/stream.json
	./monet --load-pages backup/pages.json

fmt:
	goimports -w $(shell git ls-files '*.go')

run:
	reflex -g '*.go' -s -- sh -c "go build --tags=fts5 && ./monet --config=dev.cfg.json"
