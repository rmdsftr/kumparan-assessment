package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed *.sql
var FS embed.FS

func New(dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(FS, ".")
	if err != nil {
		return nil, fmt.Errorf("load embedded migrations: %w", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	drv, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("connect database: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "pgx5", drv)
	if err != nil {
		db.Close()
		return nil, err
	}
	return m, nil
}
