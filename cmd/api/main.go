package main

import (
	"log"
	"net/http"

	"github.com/Ricardoleaom/backend-challenge-go/internal/httpapi"
)

func main() {
	server := &http.Server{
		Addr:    ":8080",
		Handler: httpapi.NewHandler(),
	}

	log.Println("HTTP server listening on :8080")

	if err := server.ListenAndServe(); err != nil &&
		err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
