package main

import (
	"fmt"
	"os"

	"dev.helix.code/internal/database"
)

func main() {
	// HXC-168: the password MUST NOT be a literal — it is read from the
	// environment (see .env.full-test / HELIX_DATABASE_PASSWORD) with a clear,
	// loud failure when unset rather than silently falling back to a
	// (previously leaked) hardcoded value.
	password := os.Getenv("HELIX_DATABASE_PASSWORD")
	if password == "" {
		fmt.Fprintln(os.Stderr, "HELIX_DATABASE_PASSWORD must be set (see .env.full-test); refusing to use a hardcoded default")
		os.Exit(3)
	}
	cfg := database.Config{
		Host:     "127.0.0.1",
		Port:     55432,
		User:     "helixcode",
		Password: password,
		DBName:   "helixcode_test",
		SSLMode:  "disable",
	}
	db, err := database.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect failed:", err)
		os.Exit(1)
	}
	if err := db.InitializeSchema(); err != nil {
		fmt.Fprintln(os.Stderr, "schema init failed:", err)
		os.Exit(2)
	}
	fmt.Println("SCHEMA_OK")
}
