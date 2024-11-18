package gallery

import (
	"testing"
)

func TestTimeParsing(t *testing.T) {
	x := ParsePicasaTime("2009-04-27T07:18:46.000Z")
	if x != 1240816726 {
		t.Errorf("Got %d, expected 1240816726\n", x)
	}
	x = ParsePicasaTime("2011-06-30T05:33:15.334Z")
	if x != 1309411995 {
		t.Errorf("Got %d, expected 1309411995\n", x)
	}

}
