package conf

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	assert := assert.New(t)
	c := Default()
	assert.True(len(c.String()) > 0)

	buf := bytes.NewBuffer([]byte(`{"SessionSecret": "秘密"}`))
	oldLen := len(c.String())
	c.FromReader(buf)
	assert.Equal(c.SessionSecret, "秘密")
	assert.True(len(c.String()) != oldLen)

	assert.Error(c.FromPath("does/not/exist/path"))
}

// what --gen-config prints is a config that loads back as it was
func TestExampleRoundTrips(t *testing.T) {
	ex := Example()
	var got Config
	if err := got.FromReader(strings.NewReader(ex.String())); err != nil {
		t.Fatal(err)
	}
	if got.String() != ex.String() {
		t.Errorf("config changed in a round trip:\n%s\nwas\n%s", got.String(), ex.String())
	}
	if got.Classify.Embedder == "" || got.Geo.Dir == "" || got.FSS.Paths["photos"] == "" {
		t.Errorf("the example leaves out settings: %+v", got)
	}
}
