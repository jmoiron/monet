
export CGO_CFLAGS := -g -O2 -Wno-return-local-addr 
# -Wno-stringop-overflow

build:
	CGO_FLAGS=$CGO_FLAGS go build --tags="fts5"

