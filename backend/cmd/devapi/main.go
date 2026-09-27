// devapi is a loopback-only integration harness for the questionnaire slice.
// It must not be deployed as the production API: MAX sessions are not wired yet.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"max-carreer-bot/internal/httpapi"
	"max-carreer-bot/internal/questionnaire"
)

func main() {
	if os.Getenv("APP_ENV") != "local" {
		log.Fatal("devapi requires APP_ENV=local")
	}
	addr := "127.0.0.1:8080"
	if value := os.Getenv("DEV_HTTP_ADDR"); value != "" {
		if value != "0.0.0.0:8080" {
			log.Fatal("DEV_HTTP_ADDR must be 0.0.0.0:8080 when set")
		}
		addr = value
	}
	bank, err := questionnaire.Load("data/question-bank.v1.json")
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/v1/questionnaire/preview", httpapi.QuestionnaireHandler(bank))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("local questionnaire API listening on %s", addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Print(err)
		os.Exit(1)
	}
}
