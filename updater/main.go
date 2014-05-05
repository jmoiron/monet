package main

import (
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db"
)

func main() {
	db.Connect(conf.Config.DbHostString(), conf.Config.DbName)

	updateTwitter()
}
