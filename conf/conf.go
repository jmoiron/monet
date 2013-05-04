package conf

import (
	"encoding/json"
	"fmt"
	"os"
)

type config struct {
	// serving options
	Debug bool

	WebHost string "web address"
	WebPort int    "web port"

	SessionSecret             string
	GoogleAnalyticsTrackingID string

	StaticPath         string
	TemplatePaths      []string
	TemplatePreCompile bool

	DbHost string
	DbPort int
	DbName string

	Gallery map[string]string
}

var Path = "./config.json"
var Config = new(config)

func (c *config) HostString() string {
	return fmt.Sprintf("%s:%d", c.WebHost, c.WebPort)
}

func (c *config) DbHostString() string {
	if c.DbPort > 0 {
		return fmt.Sprintf("mongodb://%s:%d", c.DbHost, c.DbPort)
	}
	return fmt.Sprintf("mongodb://%s", c.DbHost)
}

func (c *config) String() string {
	s := "Config:"
	s += fmt.Sprintf("   Host: %s,\n", c.HostString())
	s += fmt.Sprintf("   DB: %s,\n", c.DbHostString())
	s += fmt.Sprintf("   TemplatePaths: %s,\n", c.TemplatePaths)
	s += fmt.Sprintf("   StaticPath: %s,\n", c.StaticPath)
	s += fmt.Sprintf("   TemplatePreCompile: %v,\n", c.TemplatePreCompile)
	s += fmt.Sprintf("   Debug: %v\n", c.Debug)
	s += fmt.Sprintf("   Gallery: %v\n", c.Gallery)
	s += fmt.Sprintf("   GoogleAnalyticsTrackingID: %v\n", c.GoogleAnalyticsTrackingID)
	return s
}

func (c *config) AddTemplatePath(path string) {
	c.TemplatePaths = append(c.TemplatePaths, path)
}

func init() {
	// defaults
	Config.WebHost = "0.0.0.0"
	Config.WebPort = 7000
	Config.DbHost = "127.0.0.1"
	Config.DbPort = 0
	Config.DbName = "monet"
	Config.StaticPath = "./static"
	Config.AddTemplatePath("./templates")
	Config.SessionSecret = "SECRET-KEY-SET-IN-CONFIG"
	Config.Debug = false
	Config.TemplatePreCompile = true

	if ecp := os.Getenv("MONET_CONFIG_PATH"); ecp != "" {
		Path = ecp
	}
	file, err := os.Open(Path)
	if err != nil {
		if len(Path) > 1 {
			fmt.Printf("Error: could not read config file %s.\n", Path)
		}
		return
	}
	decoder := json.NewDecoder(file)
	// overwrite in-mem config with new values
	err = decoder.Decode(Config)
	if err != nil {
		fmt.Printf("Error decoding file %s\n%s\n", Path, err)
	}
}
