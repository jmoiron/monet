package conf

import ("os"; "fmt")

type config struct {
    // serving options
    WebHost string "web address"
    WebPort int "web port"

    SessionSecret string

    StaticPath string
    TemplatePaths []string
    TemplatePreCompile bool

    DbHost string
    DbPort int
    DbName string
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
    s := "{\n"
    s += fmt.Sprintf("   Host: %s,\n", c.HostString())
    s += fmt.Sprintf("   TemplatePaths: %s,\n", c.TemplatePaths)
    s += "}\n"
    return s
}

func (c *config) AddTemplatePath(path string) {
    c.TemplatePaths = append(c.TemplatePaths, path)
}

func init() {
    env_path := os.Getenv("MONET_CONFIG_PATH")
    if env_path != "" {
        Path = env_path
    }
    // defaults
    Config.WebHost = "0.0.0.0"
    Config.WebPort = 4000
    Config.DbHost = "127.0.0.1"
    Config.DbPort = 0
    Config.DbName = "monet"
    Config.StaticPath = "./static"
    Config.AddTemplatePath("./templates")
    Config.SessionSecret = "SECRET-KEY-SET-IN-CONFIG"
    Config.TemplatePreCompile = true
}

// post-flag initialize
func Init() {
    // FIXME: ghost defaults with the json file @ path

}
