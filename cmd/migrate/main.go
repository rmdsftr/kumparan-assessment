package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/golang-migrate/migrate/v4"

	"kumparan-assessment/internal/config"
	"kumparan-assessment/migrations"
)

const usage = `usage: go run ./cmd/migrate <command>

commands:
  up [N]        apply all pending migrations, or the next N
  down [N|all]  revert the last N migrations (default 1), or all of them
  goto V        migrate up or down to version V (e.g. "goto 1" = schema without seed)
  version       print the current version and dirty flag
  force V       set version V without running SQL; use after fixing a failed migration`

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("migrate: %v", err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cmd, rest := args[0], args[1:]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	m, err := migrations.New(cfg.DB.DSN())
	if err != nil {
		return err
	}
	defer m.Close()
	m.Log = logger{}

	switch cmd {
	case "up":
		n, err := optionalInt(rest, 0)
		if err != nil {
			return err
		}
		if n > 0 {
			err = m.Steps(n)
		} else {
			err = m.Up()
		}
		if err := ignoreNoChange(err); err != nil {
			return err
		}

	case "down":
		if len(rest) > 0 && rest[0] == "all" {
			err = m.Down()
		} else {
			n, perr := optionalInt(rest, 1)
			if perr != nil {
				return perr
			}
			err = m.Steps(-n)
		}
		if err := ignoreNoChange(err); err != nil {
			return err
		}

	case "goto":
		v, err := requiredInt(rest, "goto V")
		if err != nil {
			return err
		}
		if err := ignoreNoChange(m.Migrate(uint(v))); err != nil {
			return err
		}

	case "force":
		v, err := requiredInt(rest, "force V")
		if err != nil {
			return err
		}
		if err := m.Force(v); err != nil {
			return err
		}

	case "version":

	default:
		return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}

	return printVersion(m)
}

func printVersion(m *migrate.Migrate) error {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Println("version: none (no migrations applied)")
		return nil
	}
	if err != nil {
		return err
	}
	if dirty {
		fmt.Printf("version: %d (DIRTY - migration %d failed partway; fix the database, then run `force <last good version>`)\n", v, v)
		return nil
	}
	fmt.Printf("version: %d\n", v)
	return nil
}

func ignoreNoChange(err error) error {
	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Println("no change")
		return nil
	}
	return err
}

func optionalInt(args []string, def int) (int, error) {
	if len(args) == 0 {
		return def, nil
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		return 0, fmt.Errorf("expected a positive number, got %q", args[0])
	}
	return n, nil
}

func requiredInt(args []string, form string) (int, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("missing version: %s", form)
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("expected a version number, got %q", args[0])
	}
	return n, nil
}

type logger struct{}

func (logger) Printf(format string, v ...any) { fmt.Printf(format, v...) }
func (logger) Verbose() bool                  { return false }
