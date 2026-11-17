// Command migrate applies, rolls back or inspects database migrations.
//
//	go run ./cmd/migrate up
//	go run ./cmd/migrate down 1
//	go run ./cmd/migrate status
//	go run ./cmd/migrate force 3    # recovery only; see docs/OPERATIONS.md
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/sanyam/medicart/services/api/internal/config"
	"github.com/sanyam/medicart/services/api/internal/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	args := os.Args[1:]
	if len(args) == 0 {
		return fmt.Errorf("usage: migrate <up|down [n]|status|force VERSION>")
	}

	switch args[0] {
	case "up":
		from, to, err := db.MigrateUp(cfg.DatabaseURL)
		if err != nil {
			return err
		}
		if from == to {
			fmt.Printf("already at version %d — nothing to apply\n", to)
		} else {
			fmt.Printf("migrated %d → %d\n", from, to)
		}
		return nil

	case "down":
		n := 1
		if len(args) > 1 {
			if n, err = strconv.Atoi(args[1]); err != nil {
				return fmt.Errorf("invalid step count %q", args[1])
			}
		}
		if err := db.MigrateDown(cfg.DatabaseURL, n); err != nil {
			return err
		}
		v, _, _ := db.MigrationStatus(cfg.DatabaseURL)
		fmt.Printf("rolled back %d migration(s); now at version %d\n", n, v)
		return nil

	case "status":
		v, dirty, err := db.MigrationStatus(cfg.DatabaseURL)
		if err != nil {
			return err
		}
		fmt.Printf("version: %d\ndirty:   %v\n", v, dirty)
		if dirty {
			fmt.Println("\nThe schema is DIRTY: a migration failed part-way.")
			fmt.Println("Inspect the database, then clear it with:  migrate force <last-good-version>")
			os.Exit(2)
		}
		return nil

	case "force":
		if len(args) < 2 {
			return fmt.Errorf("force requires a version")
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("invalid version %q", args[1])
		}
		if err := db.ForceVersion(cfg.DatabaseURL, v); err != nil {
			return err
		}
		fmt.Printf("forced version to %d (dirty flag cleared)\n", v)
		return nil

	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
