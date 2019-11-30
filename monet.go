package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/handlers"
	"github.com/jmoiron/monet/conf"
	"github.com/spf13/pflag"
)

func main() {
	config := conf.Default()

	pflag.BoolVarP(&config.Debug, "debug", "d", false, "enable debug mode")
	pflag.Parse()

	fmt.Println(config.String())

	r := http.NewServeMux()
	fmt.Printf("Listening on %s\n", config.ListenAddr)
	http.ListenAndServe(config.ListenAddr, handlers.CompressHandler(r))
}
