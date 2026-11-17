package conf

import (
	"bytes"
	"context"
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

func TestSecrets(t *testing.T) {
	assert := assert.New(t)
	assert.True(Default().InsecureSecret())
	assert.True((&Config{}).InsecureSecret())
	assert.True((&Config{SessionSecret: "SET-A-LONG-RANDOM-STRING"}).InsecureSecret())
	a, b := RandomSecret(), RandomSecret()
	assert.Len(a, 64)
	assert.NotEqual(a, b)
	assert.False((&Config{SessionSecret: a}).InsecureSecret())
	assert.False(Example().InsecureSecret())
}

func TestRedacted(t *testing.T) {
	c := Example()
	secret := c.SessionSecret
	out := c.Redacted().String()
	assert.NotContains(t, out, secret)
	assert.Contains(t, out, `"(set)"`)
	assert.Equal(t, secret, c.SessionSecret) // the config itself is as it was
	assert.Contains(t, c.String(), secret)   // and --gen-config still prints it
}

func TestConfigInContext(t *testing.T) {
	c := Example()
	assert.Same(t, c, ConfigFromContext(c.WithConfig(context.Background())))
	assert.Nil(t, ConfigFromContext(context.Background())) // not a panic
}
