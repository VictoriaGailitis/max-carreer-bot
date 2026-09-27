// The API serves auth, profile, catalog, favorites and MAX webhook.
package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"max-carreer-bot/internal/auth"
	"max-carreer-bot/internal/catalog"
	"max-carreer-bot/internal/cryptostore"
	"max-carreer-bot/internal/httpapi"
	"max-carreer-bot/internal/maxbot"
	"max-carreer-bot/internal/questionnaire"
	"max-carreer-bot/internal/storage"
)

func main() {
	appEnv := os.Getenv("APP_ENV")
	if appEnv != "local" && appEnv != "production" {
		log.Fatal("APP_ENV must be local or production")
	}
	addr := "127.0.0.1:8081"
	if value := os.Getenv("HTTP_ADDR"); value != "" {
		if value != "0.0.0.0:8081" {
			log.Fatal("HTTP_ADDR must be 0.0.0.0:8081 when set")
		}
		addr = value
	}
	if appEnv == "production" {
		origin, err := url.Parse(os.Getenv("PUBLIC_ORIGIN"))
		if err != nil || origin.Scheme != "https" || origin.Hostname() == "" || origin.Port() != "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || !strings.Contains(origin.Hostname(), ".") || net.ParseIP(origin.Hostname()) != nil || strings.HasSuffix(origin.Hostname(), ".local") {
			log.Fatal("PUBLIC_ORIGIN must be a public HTTPS origin")
		}
		if addr != "0.0.0.0:8081" || os.Getenv("MAX_WORKER_ENABLED") != "1" {
			log.Fatal("production requires HTTP_ADDR and MAX worker")
		}
	}
	bank, err := questionnaire.Load("data/question-bank.v1.json")
	if err != nil {
		log.Fatal("questionnaire bank is invalid")
	}
	botToken, err := readSmallSecret(os.Getenv("MAX_BOT_TOKEN_FILE"))
	if err != nil {
		log.Fatal("MAX bot token file is invalid")
	}
	if appEnv == "production" && strings.HasPrefix(botToken, "local-only-") {
		log.Fatal("production requires a real MAX bot token")
	}
	validator, err := auth.NewValidator(botToken)
	if err != nil {
		log.Fatal("MAX bot token is invalid")
	}
	webhookSecret, err := readSmallSecret(os.Getenv("MAX_WEBHOOK_SECRET_FILE"))
	if err != nil {
		log.Fatal("MAX webhook secret file is invalid")
	}
	parser, err := maxbot.NewParser(botToken)
	if err != nil {
		log.Fatal("MAX webhook parser is invalid")
	}
	lookupKey, err := cryptostore.LoadHexKeyFile(os.Getenv("IDENTITY_LOOKUP_KEY_V1_FILE"))
	if err != nil {
		log.Fatal("identity lookup key file is invalid")
	}
	lookup, err := cryptostore.NewIdentityLookup(map[uint32][]byte{1: lookupKey}, 1)
	if err != nil {
		log.Fatal("identity lookup key is invalid")
	}
	dataKey, err := cryptostore.LoadHexKeyFile(os.Getenv("DATA_KEY_V1_FILE"))
	if err != nil {
		log.Fatal("data key file is invalid")
	}
	if subtle.ConstantTimeCompare(dataKey, lookupKey) == 1 {
		log.Fatal("data and lookup keys must differ")
	}
	crypto, err := cryptostore.NewStore(map[uint32][]byte{1: dataKey}, 1)
	if err != nil {
		log.Fatal("data key is invalid")
	}
	databaseURL, err := readSmallSecret(os.Getenv("DATABASE_URL_FILE"))
	if err != nil {
		log.Fatal("database URL file is invalid")
	}
	startup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(startup, databaseURL)
	if err != nil {
		log.Fatal("database configuration is invalid")
	}
	defer pool.Close()
	if err := pool.Ping(startup); err != nil {
		log.Fatal("database unavailable")
	}
	repo, err := storage.NewSessions(pool, lookup)
	if err != nil {
		log.Fatal("session storage is invalid")
	}
	if err := repo.Ready(startup); err != nil {
		log.Fatal("user migration is missing")
	}
	state, err := storage.NewUserState(pool, crypto, bank)
	if err != nil {
		log.Fatal("user state storage is invalid")
	}
	catalogStore, err := catalog.NewStorage(pool)
	if err != nil {
		log.Fatal("catalog storage is invalid")
	}
	if err := catalogStore.SchemaReady(startup); err != nil {
		log.Fatal("catalog migration is missing")
	}
	inbox, err := maxbot.NewInbox(pool, crypto)
	if err != nil {
		log.Fatal("MAX inbox configuration is invalid")
	}
	if err := inbox.Ready(startup); err != nil {
		log.Fatal("MAX inbox migration is missing")
	}
	webhook, err := httpapi.NewWebhook(webhookSecret, parser, inbox, time.Now)
	if err != nil {
		log.Fatal("MAX webhook configuration is invalid")
	}
	handler, err := httpapi.NewCatalogAPIHandler(validator, repo, state, catalogStore, bank, time.Now)
	if err != nil {
		log.Fatal("auth API configuration is invalid")
	}
	handler, err = httpapi.WithCORS(handler, os.Getenv("CORS_ALLOWED_ORIGINS"), appEnv == "production")
	if err != nil {
		log.Fatal("CORS configuration is invalid")
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/webhooks/max", webhook)
	mux.Handle("/", handler)
	server := &http.Server{
		Addr: addr, Handler: mux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}
	stopping, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if os.Getenv("MAX_WORKER_ENABLED") == "1" {
		httpClient, err := maxbot.NewHTTPClient(os.Getenv("MAX_CA_CERT_FILE"))
		if err != nil {
			log.Fatal("MAX CA configuration is invalid")
		}
		client, err := maxbot.NewClient(botToken, os.Getenv("MAX_API_BASE_URL"), os.Getenv("MAX_APP_BOT_USERNAME"), httpClient)
		if err != nil {
			log.Fatal("MAX delivery configuration is invalid")
		}
		go (maxbot.Worker{Inbox: inbox, Sender: client, Now: time.Now}).Run(stopping)
	} else if value := os.Getenv("MAX_WORKER_ENABLED"); value != "" && value != "0" {
		log.Fatal("MAX_WORKER_ENABLED must be 0 or 1")
	}
	go func() {
		<-stopping.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("session API listening on %s", addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("HTTP server stopped unexpectedly")
	}
}

func readSmallSecret(path string) (string, error) {
	if path == "" {
		return "", errors.New("secret file path is empty")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return "", errors.New("invalid secret file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(string(data), "\n")
	value = strings.TrimSuffix(value, "\r")
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("invalid secret file")
	}
	return value, nil
}
