package conf

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
)

type configKey struct{}

type FSSConfig struct {
	Paths map[string]string
	URLs  map[string]string
}

// ClassifyConfig is how photos are searched by what is in them.
type ClassifyConfig struct {
	// Embedder is a pkg/classify spec of a worker that embeds search queries
	// ("siglip2=worker:python3 scripts/worker_openclip.py ..."), and so says which
	// model's embeddings are searched. Without one only captions and labels are.
	Embedder string
	// MinVector is the least cosine a photo has to have with a query to match
	// it by vector; 0 for the default.
	MinVector float64
	// IdleMinutes is how long the embedder is kept after the last search; 0 for
	// always. With it the embedder is not loaded when the server starts, but
	// when a gallery page is looked at.
	IdleMinutes int
}

// GeoConfig is where the names of places come from.
type GeoConfig struct {
	// Dir is where the GeoNames data is kept, and fetched to if it isn't there
	// when the server starts. Without it photos are not given places.
	Dir string
}

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

	// Paths are named full paths to directories for things like media, uploads, etc
	FSS FSSConfig

	// ChromePath is the Chrome or Chromium binary gowitness should use to take
	// bookmark screenshots (its --chrome-path). Empty means gowitness finds one
	// itself. It can be a wrapper script, eg. to add --no-sandbox when the
	// browser's own sandbox can't start.
	ChromePath string

	// Classify configures search of photos by their content.
	Classify ClassifyConfig

	// Geo configures the places that photos were taken.
	Geo GeoConfig
}

// Redacted is a copy of the config that is safe to log: the secrets are hidden.
func (c *Config) Redacted() *Config {
	r := *c
	if r.SessionSecret != "" {
		r.SessionSecret = "(set)"
		if c.InsecureSecret() {
			r.SessionSecret = "(NOT SECRET: " + c.SessionSecret + ")"
		}
	}
	return &r
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

// AddConfigMiddleware adds this config to the request contxt.
func (c *Config) AddConfigMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(c.WithConfig(r.Context())))
	})
}

// WithConfig adds this config to the context. Get it back out with
// conf.ConfigFromContext(ctx).
func (c *Config) WithConfig(ctx context.Context) context.Context {
	return context.WithValue(ctx, configKey{}, c)
}

// ConfigFromContext returns the config embedded within the context.
func ConfigFromContext(ctx context.Context) *Config {
	return ctx.Value(configKey{}).(*Config)
}

// Secrets that are in this repository, and so are no secret.
const (
	defaultSecret = "SET-IN-CONFIG-FILE"
)

// RandomSecret is a new random string to sign sessions with.
func RandomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // there is no safe way to go on without randomness
	}
	return hex.EncodeToString(b)
}

// InsecureSecret is whether sessions are signed with something that is empty
// or known: anyone could then make a cookie that logs them in.
func (c *Config) InsecureSecret() bool {
	return c.SessionSecret == "" || c.SessionSecret == defaultSecret || strings.HasPrefix(c.SessionSecret, "SET-")
}

// Example is a config for a server, with every setting there is and a value
// that works if the directories are made: what `monet --gen-config` prints, to
// be edited. Relative paths are from where monet is run.
func Example() *Config {
	c := Default()
	c.ListenAddr = "0.0.0.0:7000"
	c.SessionSecret = RandomSecret()
	c.DatabaseURI = "monet.db?cache=shared"
	c.FSS = FSSConfig{
		Paths: map[string]string{
			"static":      "./static",
			"images":      "./images",
			"uploads":     "./images",
			"blog-files":  "./images",
			"screenshots": "./images/ss/",
			"photos":      "./images/photos/",
		},
		URLs: map[string]string{
			"images":      "/i/",
			"uploads":     "/i/",
			"blog-files":  "/i/",
			"screenshots": "/i/ss/",
			"photos":      "/i/photos/",
		},
	}
	// make -C pkg/classify onnx onnxruntime, and copy the two directories it makes
	c.Classify = ClassifyConfig{
		Embedder:    "siglip2=onnx:onnx/siglip2@onnx/lib",
		IdleMinutes: 10,
	}
	c.Geo = GeoConfig{Dir: "geo"}
	return c
}

// Default returns a sensible default config with environment overrides
// and the a config file loaded into it.
func Default() *Config {
	c := &Config{}
	c.ListenAddr = "0.0.0.0:7000"
	c.StaticPath = "./static"
	c.TemplatePaths = []string{"./templates"}
	c.SessionSecret = defaultSecret
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
