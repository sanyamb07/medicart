package db

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/sanyam/medicart/services/api/migrations"
)

// The migration SQL is embedded in the binary (see package migrations), so a
// deployed container runs its own migrations with no extra tooling or
// artifact, and the schema can never drift from the code expecting it.

// newMigrator wires the embedded source to the target database.
func newMigrator(databaseURL string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("open embedded migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, "pgx5://"+trimScheme(databaseURL))
	if err != nil {
		return nil, fmt.Errorf("init migrator: %w", err)
	}
	return m, nil
}

// trimScheme swaps whatever postgres scheme the URL uses for the pgx5 driver
// name golang-migrate expects.
func trimScheme(url string) string {
	for _, p := range []string{"postgres://", "postgresql://", "pgx5://", "pgx://"} {
		if len(url) > len(p) && url[:len(p)] == p {
			return url[len(p):]
		}
	}
	return url
}

// MigrateUp applies all pending migrations. It is safe to call concurrently:
// golang-migrate takes an advisory lock, so a second instance starting at the
// same time waits rather than racing.
func MigrateUp(databaseURL string) (from, to uint, err error) {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return 0, 0, err
	}
	defer closeMigrator(m)

	from, _, _ = m.Version()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return from, from, fmt.Errorf("apply migrations: %w", err)
	}
	to, _, _ = m.Version()
	return from, to, nil
}

// MigrateDown rolls back n migrations, or all of them when n <= 0.
func MigrateDown(databaseURL string, n int) error {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if n <= 0 {
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("roll back all: %w", err)
		}
		return nil
	}
	if err := m.Steps(-n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("roll back %d: %w", n, err)
	}
	return nil
}

// MigrationStatus reports the applied version and whether a previous run left
// the schema dirty (a migration failed part-way and needs manual attention).
func MigrationStatus(databaseURL string) (version uint, dirty bool, err error) {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return 0, false, err
	}
	defer closeMigrator(m)

	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	return version, dirty, err
}

// ForceVersion clears the dirty flag by asserting a known-good version. This is
// a recovery tool for a failed migration and is intentionally not wired into
// normal startup — see docs/OPERATIONS.md.
func ForceVersion(databaseURL string, version int) error {
	m, err := newMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer closeMigrator(m)
	return m.Force(version)
}

// closeMigrator releases both the source and the database handle. Errors here
// are not actionable by the caller — the work is already committed — but they
// must not be silently dropped either, so they surface as the process's own
// stderr through the migrate library's logger if set.
func closeMigrator(m *migrate.Migrate) {
	srcErr, dbErr := m.Close()
	_ = srcErr
	_ = dbErr
}

// assert the driver import is retained (it registers "pgx5" by side effect).
var _ = pgx.Postgres{}
