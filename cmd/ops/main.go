package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"max-carreer-bot/internal/catalog"
	"max-carreer-bot/internal/questionnaire"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "generate-key":
		runGenerateKey(os.Args[2:])
	case "catalog":
		runCatalog(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ops generate-key -out PATH | catalog validate|import -file PATH -allow-domain DOMAIN [-database-url-file PATH]")
	os.Exit(2)
}

func runGenerateKey(args []string) {
	flags := flag.NewFlagSet("generate-key", flag.ContinueOnError)
	output := flags.String("out", "", "path for a new 0600 key file")
	if err := flags.Parse(args); err != nil || *output == "" || len(flags.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "usage: ops generate-key -out PATH")
		os.Exit(2)
	}
	if err := generateKeyFile(*output, rand.Reader); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("key file created")
}

type domainFlags []string

func (d *domainFlags) String() string         { return strings.Join(*d, ",") }
func (d *domainFlags) Set(value string) error { *d = append(*d, value); return nil }

func runCatalog(args []string) {
	if len(args) < 1 || (args[0] != "validate" && args[0] != "import") {
		usage()
	}
	command := args[0]
	flags := flag.NewFlagSet("catalog "+command, flag.ContinueOnError)
	path := flags.String("file", "", "catalog JSON file")
	bankPath := flags.String("bank", "data/question-bank.v1.json", "question bank JSON file")
	databaseURLFile := flags.String("database-url-file", "", "PostgreSQL URL file for import")
	var domains domainFlags
	flags.Var(&domains, "allow-domain", "allowed source domain; repeatable")
	if err := flags.Parse(args[1:]); err != nil || len(flags.Args()) > 0 || *path == "" || len(domains) == 0 || (command == "import" && *databaseURLFile == "") {
		usage()
	}
	bank, err := questionnaire.Load(*bankPath)
	if err != nil {
		failCatalog("invalid question bank", err)
	}
	validator, err := catalog.NewValidator(bank, domains)
	if err != nil {
		failCatalog("invalid allowed domains", err)
	}
	info, err := os.Stat(*path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > catalog.MaxFileBytes {
		failCatalog("invalid catalog file", err)
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		failCatalog("cannot read catalog", err)
	}
	validated, err := validator.Validate(raw)
	if err != nil {
		failCatalog("catalog validation failed", err)
	}
	if command == "validate" {
		fmt.Printf("catalog valid: %d cards\n", len(validated.Dataset.Items))
		return
	}
	databaseURL, err := readSecret(*databaseURLFile)
	if err != nil {
		failCatalog("invalid database URL file", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		failCatalog("invalid database configuration", nil)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		failCatalog("database unavailable", nil)
	}
	store, err := catalog.NewStorage(pool)
	if err != nil {
		failCatalog("catalog storage unavailable", err)
	}
	if err := store.SchemaReady(ctx); err != nil {
		failCatalog("catalog migration missing", err)
	}
	version, changed, err := store.Import(ctx, validated)
	if err != nil {
		failCatalog("catalog import failed", err)
	}
	if changed {
		fmt.Printf("catalog activated: %s\n", version)
	} else {
		fmt.Printf("catalog unchanged: %s\n", version)
	}
}

func failCatalog(message string, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, message+":", err)
	} else {
		fmt.Fprintln(os.Stderr, message)
	}
	os.Exit(1)
}

func readSecret(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return "", errors.New("invalid secret file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("invalid secret")
	}
	return value, nil
}

func generateKeyFile(path string, random io.Reader) error {
	var key [32]byte
	if _, err := io.ReadFull(random, key[:]); err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create key file: %w", err)
	}
	value := make([]byte, hex.EncodedLen(len(key))+1)
	hex.Encode(value, key[:])
	value[len(value)-1] = '\n'
	_, writeErr := file.Write(value)
	syncErr := file.Sync()
	closeErr := file.Close()
	for i := range key {
		key[i] = 0
	}
	for i := range value {
		value[i] = 0
	}
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write key file: %w", err)
	}
	return nil
}
