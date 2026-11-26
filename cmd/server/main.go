// Command server runs Meals: the JSON API and the embedded web app.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/bhr-tools/kit/server"
	"github.com/bhr-tools/meals/internal/api"
	"github.com/bhr-tools/meals/internal/db"
	"github.com/bhr-tools/meals/web"
)

// Each flag defaults to its environment variable (kit/server), so the binary
// is configured with flags (NixOS module) or environment alike.
func main() {
	cfg := server.Flags(8080, "meals.db")
	flag.Parse()

	store, err := db.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer store.Close()
	a, err := api.New(store, cfg.NoAuth)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("database %s", cfg.DBPath())
	server.Run("Meals", cfg.Addr(), newHandler(store, a))
}

// newHandler routes the API, the health check, the kit files, the service
// worker and the web app (kit/server).
func newHandler(store *db.Store, a *api.API) http.Handler {
	return server.Handler(server.Options{API: a.Handler(), Web: web.Files, Ping: store.Ping, Offline: true})
}
