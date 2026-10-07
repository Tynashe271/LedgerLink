// Command api is the BranchLedger Go API: the authoritative backend for
// access control, accounting, synchronisation and reporting (architecture
// doc, "Architecture decision"). It is never exposed directly to browsers;
// the Laravel gateway proxies /api requests to it over the same deployment
// network (see ops/deploy).
package main

import (
	"context"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ledgerlink/branchledger/backend/internal/accounting"
	"github.com/ledgerlink/branchledger/backend/internal/auth"
	"github.com/ledgerlink/branchledger/backend/internal/catalog"
	"github.com/ledgerlink/branchledger/backend/internal/exports"
	"github.com/ledgerlink/branchledger/backend/internal/httpapi"
	"github.com/ledgerlink/branchledger/backend/internal/listings"
	"github.com/ledgerlink/branchledger/backend/internal/reporting"
	"github.com/ledgerlink/branchledger/backend/internal/storage"
	"github.com/ledgerlink/branchledger/backend/internal/sync"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connString := requireEnv("DATABASE_URL")
	delegationSecret := requireEnv("DELEGATION_SECRET") // shared HMAC secret with the Laravel gateway
	listenAddr := envOr("API_LISTEN_ADDR", ":8080")

	secretBytes, err := hex.DecodeString(delegationSecret)
	if err != nil || len(secretBytes) < 32 {
		log.Fatal("DELEGATION_SECRET must be a hex-encoded value of at least 32 bytes")
	}

	db, err := storage.Open(ctx, connString)
	if err != nil {
		log.Fatalf("storage.Open: %v", err)
	}
	defer db.Close()

	verifier := auth.NewVerifier(secretBytes, 2*time.Minute)
	accountingSvc := accounting.NewService(db)
	pusher := sync.NewPusher(accountingSvc)
	puller := sync.NewPuller(db)
	reportingSvc := reporting.NewService(db)
	exportsSvc := exports.NewService(db.Exports())
	listingsSvc := listings.NewService(db.Listings())
	catalogSvc := catalog.NewService(db.Catalog())

	router := httpapi.NewRouter(httpapi.Deps{
		Verifier:      verifier,
		AccountingSvc: accountingSvc,
		Pusher:        pusher,
		Puller:        puller,
		ReportingSvc:  reportingSvc,
		ExportsSvc:    exportsSvc,
		ListingsSvc:   listingsSvc,
		CatalogSvc:    catalogSvc,
	})

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("branchledger api listening on %s", listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required environment variable %s", key)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
