package conf

import (
	"bytes"
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
