package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/mediaworker"
)

func main() {
	root := env("MEDIA_WORKER_DATA_DIR", "/app/data")
	token := os.Getenv("MEDIA_WORKER_TOKEN")
	port := env("MEDIA_WORKER_PORT", "18810")
	baseURL := os.Getenv("MEDIA_WORKER_PUBLIC_BASE_URL")
	concurrency, _ := strconv.Atoi(env("MEDIA_WORKER_CONCURRENCY", "1"))
	processor := mediaworker.NewProcessor(nil, root, nil)
	server, err := mediaworker.NewServer(processor, root, token, baseURL, concurrency)
	if err != nil {
		log.Fatal(err)
	}
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Tekshot media worker listening on %s", httpServer.Addr)
	log.Fatal(httpServer.ListenAndServe())
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
