package conf

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// A Config holds options for the running website.
type Config struct {
	Debug      bool
	ListenAddr string

	SessionSecret             string
	GoogleAnalyticsTrackingID string

	StaticPath         string
	TemplatePaths      []string
	TemplatePreCompile bool

	// DatabaseURI is a connectable URI string
	DatabaseURI string
}

// String returns the config as a string.
func (c *Config) String() string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	err := enc.Encode(c)
	if err != nil {
		panic(err)
	}
	return buf.String()
}

// FromFile loads a config from path and merges it into c.
func (c *Config) FromPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return c.FromReader(f)
}

// FromReader loads a config from the reader r.
func (c *Config) FromReader(r io.Reader) error {
	return json.NewDecoder(r).Decode(c)
}

// Default returns a sensible default config with environment overrides
// and the a config file loaded into it.
func Default() *Config {
	c := &Config{}
	c.ListenAddr = "0.0.0.0:7000"
	c.StaticPath = "./static"
	c.TemplatePaths = []string{"./templates"}
	c.SessionSecret = "SET-IN-CONFIG-FILE"
	c.TemplatePreCompile = true

	/*
		if path := os.Getenv("MONET_CONFIG_PATH"); len(path) > 0 {
			err := c.FromPath(path)
			if err != nil {
				fmt.Printf("Error loading config: %s\n", err)
			}
		}
	*/

	return c
}
