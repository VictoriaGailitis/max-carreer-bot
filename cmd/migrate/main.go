package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var migrationName = regexp.MustCompile(`^([0-9]{4})_[a-z0-9_]+\.up\.sql$`)

type migration struct {
	version  int
	path     string
	checksum [32]byte
	SQL      string
}

func main() {
	urlFile := os.Getenv("DATABASE_URL_FILE")
	if urlFile == "" {
		log.Fatal("DATABASE_URL_FILE is required")
	}
	data, err := os.ReadFile(urlFile)
	if err != nil {
		log.Fatal("cannot read migration database URL")
	}
	databaseURL := strings.TrimSpace(string(data))
	if databaseURL == "" {
		log.Fatal("migration database URL is empty")
	}
	appURLFile := os.Getenv("APP_DATABASE_URL_FILE")
	appData, err := os.ReadFile(appURLFile)
	if err != nil {
		log.Fatal("cannot read API database URL")
	}
	appURL := strings.TrimSpace(string(appData))
	appPassword, err := checkAppURL(databaseURL, appURL)
	if err != nil {
		log.Fatal("API database URL is invalid")
	}
	dir := os.Getenv("MIGRATIONS_DIR")
	if dir == "" {
		dir = "migrations"
	}
	migrations, err := loadMigrations(dir)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatal("migration database unavailable")
	}
	defer conn.Close(context.Background())
	if err := apply(ctx, conn, migrations, appPassword); err != nil {
		log.Fatal(err)
	}
	log.Printf("migrations ready: %d", len(migrations))
}

func loadMigrations(dir string) ([]migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var all []migration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		var version int
		if _, err := fmt.Sscanf(match[1], "%d", &version); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("empty migration %s", entry.Name())
		}
		all = append(all, migration{version: version, path: path, checksum: sha256.Sum256(data), SQL: string(data)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].version < all[j].version })
	if len(all) == 0 {
		return nil, errors.New("no migrations found")
	}
	for index, item := range all {
		if item.version != index+1 {
			return nil, fmt.Errorf("migration version gap at %s", item.path)
		}
	}
	return all, nil
}

func checkAppURL(admin, app string) (string, error) {
	a, err := url.Parse(admin)
	if err != nil {
		return "", err
	}
	b, err := url.Parse(app)
	if err != nil {
		return "", err
	}
	if a.Scheme != "postgres" && a.Scheme != "postgresql" {
		return "", errors.New("invalid admin scheme")
	}
	if b.Scheme != a.Scheme || b.Host != a.Host || b.Path != a.Path || b.User == nil || b.User.Username() != "navigator_api" {
		return "", errors.New("API URL points to another database")
	}
	password, ok := b.User.Password()
	if !ok || len(password) < 20 {
		return "", errors.New("API database password is missing or too short")
	}
	return password, nil
}

func apply(ctx context.Context, conn *pgx.Conn, all []migration, appPassword string) error {
	// Session lock keeps two migrate containers from applying the same version.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(611782915)`); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(611782915)`)
	if err := provisionRole(ctx, conn, appPassword); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		checksum BYTEA NOT NULL CHECK (octet_length(checksum)=32),
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	rows, err := conn.Query(ctx, `SELECT version,checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return err
	}
	applied := make(map[int][]byte)
	for rows.Next() {
		var version int
		var checksum []byte
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return err
		}
		applied[version] = checksum
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for version := range applied {
		if version < 1 || version > len(all) {
			return fmt.Errorf("unknown applied migration %d", version)
		}
	}
	for _, item := range all {
		if saved, ok := applied[item.version]; ok {
			if string(saved) != string(item.checksum[:]) {
				return fmt.Errorf("migration %d checksum differs", item.version)
			}
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, item.SQL, pgx.QueryExecModeSimpleProtocol); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO schema_migrations(version,checksum) VALUES ($1,$2)`, item.version, item.checksum[:])
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %d failed: %w", item.version, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		log.Printf("applied migration %d", item.version)
	}
	// Catalog writes are reserved for the operator's CLI connection.
	_, err = conn.Exec(ctx, `GRANT USAGE ON SCHEMA public TO navigator_api;
		GRANT SELECT,INSERT,UPDATE,DELETE ON users,user_state,sessions,idempotency_records,bot_inbox TO navigator_api;
		GRANT SELECT ON questionnaire_versions,catalog_versions,catalog_active,opportunities TO navigator_api`, pgx.QueryExecModeSimpleProtocol)
	return err
}

func provisionRole(ctx context.Context, conn *pgx.Conn, password string) error {
	var exists, superuser, createDB, createRole bool
	err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='navigator_api'),
		COALESCE((SELECT rolsuper FROM pg_roles WHERE rolname='navigator_api'),false),
		COALESCE((SELECT rolcreatedb FROM pg_roles WHERE rolname='navigator_api'),false),
		COALESCE((SELECT rolcreaterole FROM pg_roles WHERE rolname='navigator_api'),false)`).Scan(&exists, &superuser, &createDB, &createRole)
	if err != nil {
		return err
	}
	if superuser || createDB || createRole {
		return errors.New("API database role has elevated privileges")
	}
	format := "CREATE ROLE navigator_api LOGIN PASSWORD %L"
	if exists {
		format = "ALTER ROLE navigator_api PASSWORD %L"
	}
	var statement string
	if err := conn.QueryRow(ctx, `SELECT format($1::text,$2::text)`, format, password).Scan(&statement); err != nil {
		return errors.New("cannot prepare API role")
	}
	if _, err := conn.Exec(ctx, statement); err != nil {
		return errors.New("cannot provision API role")
	}
	return nil
}
