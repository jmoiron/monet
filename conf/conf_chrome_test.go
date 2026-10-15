package conf

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChromePathFromConfig(t *testing.T) {
	var c Config
	assert.NoError(t, c.FromReader(strings.NewReader(`{"ChromePath": "/usr/local/bin/chrome-nosandbox"}`)))
	assert.Equal(t, "/usr/local/bin/chrome-nosandbox", c.ChromePath)

	var empty Config
	assert.NoError(t, empty.FromReader(strings.NewReader(`{}`)))
	assert.Empty(t, empty.ChromePath)
}
