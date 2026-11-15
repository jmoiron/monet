package gallery

import (
	"regexp"
	"testing"
)

var shortID = regexp.MustCompile(`^[0-9A-Za-z]{9}$`)

func TestNewID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if !shortID.MatchString(id) {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatalf("repeated id %q", id)
		}
		seen[id] = true
	}
}

func TestIDFor(t *testing.T) {
	a, b := idFor("ns", "1001"), idFor("ns", "1001")
	if a != b || !shortID.MatchString(a) {
		t.Fatalf("not stable or malformed: %q %q", a, b)
	}
	if a == idFor("ns", "1002") {
		t.Fatal("different names share an id")
	}
}

func TestContentID(t *testing.T) {
	a := contentID([]byte("some image"))
	if a != contentID([]byte("some image")) || !shortID.MatchString(a) {
		t.Fatalf("not stable or malformed: %q", a)
	}
	if a == contentID([]byte("some image!")) {
		t.Fatal("different content shares an id")
	}
}
