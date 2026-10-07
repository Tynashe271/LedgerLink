// Command worker processes the committed outbox: summaries, exports, alerts
// and attachment processing, per the architecture's "Go workers" component.
// External effects never happen inside the financial database commit; this
// process is where they happen afterward, each with its own idempotency key.
package main

import (
	"context"
	"encoding/csv"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/ledgerlink/branchledger/backend/internal/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connString := requireEnv("DATABASE_URL")
	exportsDir := envOr("EXPORTS_DIR", "./data/exports")
	if err := os.MkdirAll(exportsDir, 0o750); err != nil {
		log.Fatalf("create exports dir: %v", err)
	}

	db, err := storage.Open(ctx, connString)
	if err != nil {
		log.Fatalf("storage.Open: %v", err)
	}
	defer db.Close()

	log.Println("branchledger worker starting")

	pollInterval := 5 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("worker shutting down")
			return
		case <-ticker.C:
			drainOutbox(ctx, db, exportsDir)
		}
	}
}

// drainOutbox claims and processes jobs until none are due, so a burst of
// commits (e.g. a large sync/push batch) doesn't wait for the next tick per
// job.
func drainOutbox(ctx context.Context, db *storage.DB, exportsDir string) {
	for {
		job, found, err := db.ClaimNextOutboxJob(ctx)
		if err != nil {
			log.Printf("claim outbox job: %v", err)
			return
		}
		if !found {
			return
		}
		processJob(ctx, db, exportsDir, job)
	}
}

func processJob(ctx context.Context, db *storage.DB, exportsDir string, job storage.OutboxJob) {
	var err error
	switch job.JobType {
	case "export":
		err = processExportJob(ctx, db, exportsDir, job)
	case "summary_refresh":
		// Dashboard figures are computed live from posted journals (see
		// internal/reporting), not from a cached projection, so there is no
		// projection to refresh yet. Acknowledging the job here keeps the
		// outbox from filling with work that nothing currently reads.
		err = nil
	default:
		log.Printf("job %s: unknown job_type %q, marking failed", job.ID, job.JobType)
		_ = db.MarkOutboxJobFailed(ctx, job.ID, job.Attempts, "unknown_job_type")
		return
	}

	if err != nil {
		log.Printf("job %s (%s) failed: %v", job.ID, job.JobType, err)
		if markErr := db.MarkOutboxJobFailed(ctx, job.ID, job.Attempts, err.Error()); markErr != nil {
			log.Printf("job %s: record failure: %v", job.ID, markErr)
		}
		return
	}
	if err := db.MarkOutboxJobDone(ctx, job.ID); err != nil {
		log.Printf("job %s: mark done: %v", job.ID, err)
	}
}

// processExportJob generates the CSV a POST /exports request asked for and
// writes it to local storage under a generated, non-client-chosen name
// (launch control: "Use generated object names, tenant-scoped access").
// A production deployment swaps exportsDir's filesystem writes for the
// architecture's proposed object storage; the export row's object_key is
// already an opaque identifier either way, so Get in internal/exports never
// exposes a filesystem path to the client.
func processExportJob(ctx context.Context, db *storage.DB, exportsDir string, job storage.OutboxJob) error {
	exportIDRaw, _ := job.Payload["export_id"].(string)
	exportID, err := uuid.Parse(exportIDRaw)
	if err != nil {
		return err
	}

	meta, found, err := db.GetExportMeta(ctx, exportID)
	if err != nil {
		return err
	}
	if !found {
		return nil // export row was deleted/expired before the job ran
	}

	rows, err := db.QueryTransactionsForExport(ctx, meta.CompanyID, meta.BranchID, meta.From, meta.To)
	if err != nil {
		markErr := db.MarkExportFailed(ctx, exportID, "query_failed")
		if markErr != nil {
			return markErr
		}
		return err
	}

	objectKey := exportID.String() + ".csv"
	f, err := os.Create(filepath.Join(exportsDir, objectKey))
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	if err := w.Write([]string{"transaction_id", "branch_id", "document_type", "document_date", "currency_code", "status", "net_amount"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			r.ID.String(), r.BranchID.String(), r.DocumentType, r.DocumentDate.Format("2006-01-02"),
			r.CurrencyCode, r.Status, r.NetAmount.StringFixed(2),
		}); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}

	// Exports are temporary artefacts, not accounting records — a bounded
	// retention separate from the financial data they summarise (section
	// 5.9: "Set retention for temporary exports separately from accounting
	// records").
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	return db.MarkExportReady(ctx, exportID, objectKey, len(rows), expiresAt)
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
