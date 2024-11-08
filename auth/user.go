package auth

type User struct {
	ID           uint64
	Username     string
	PasswordHash string `db:"password_hash"`
}
