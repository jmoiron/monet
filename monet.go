package main

import (
	"flag"
	"net/http"

	"github.com/gorilla/handlers"
	"github.com/jmoiron/monet/conf"
)

func main() {

	flag.BoolVar(&conf.Config.Debug, "debug", false, "enable debug mode")

	r := http.NewServeMux()
	http.ListenAndServe(":8001", handlers.CompressHandler(r))
}
