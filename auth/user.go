package auth

import "github.com/jmoiron/monet/monarch"

type User struct {
	ID           uint64
	Username     string
	PasswordHash string `db:"password_hash"`
}

var userMigration = monarch.Set{
	Name: "user",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS users (
			id int NOT NULL,
			username text NOT NULL,
			password_hash text NOT NULL
			PRIMARY KEY (username)
		);`,
			Down: `DROP TABLE users;`,
		},
	},
}
