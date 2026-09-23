package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FastR-D/FastCAS/internal/core"
	"github.com/FastR-D/FastCAS/internal/httpapi"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fastcas command failed", "error", err)
		os.Exit(1)
	}
}
func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: fastcas init-keys|rotate-keys|migrate|bootstrap|register-client|serve|doctor|check-outbox|fence-restore")
	}
	cmd := os.Args[1]
	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	keys := flags.String("keys", env("FASTCAS_KEYS_DIR", ".local/keys"), "private key directory")
	file := flags.String("file", "", "application registration JSON file (secret comes from FASTCAS_CLIENT_SECRET)")
	retain := flags.Duration("retain", 24*time.Hour, "old signing public-key retention (offline rotation)")
	emergency := flags.Bool("emergency", false, "remove all previous signing keys; requires coordinated verifier cache invalidation")
	maxPendingAge := flags.Duration("max-pending-age", 30*time.Second, "alert if an undelivered event is older than this")
	maxP95Delivery := flags.Duration("max-p95-delivery", 30*time.Second, "alert if recent p95 delivery latency exceeds this")
	maxDead := flags.Int("max-dead", 0, "maximum permitted dead-letter count")
	window := flags.Duration("window", time.Hour, "recent delivery latency observation window")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if cmd == "init-keys" {
		return core.InitializeKeys(*keys)
	}
	if cmd == "rotate-keys" {
		return core.RotateKeys(*keys, *retain, *emergency)
	}
	ctx := context.Background()
	dsn := os.Getenv("FASTCAS_DATABASE_URL")
	if dsn == "" {
		return errors.New("FASTCAS_DATABASE_URL is required")
	}
	store, err := core.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer store.Close()
	store.Dev = os.Getenv("FASTCAS_ENV") == "development"
	switch cmd {
	case "migrate":
		return store.Migrate(ctx)
	case "fence-restore":
		return store.FenceRestoredState(ctx)
	case "doctor":
		if err = store.Health(ctx); err != nil {
			return err
		}
		if _, err = core.LoadKeys(*keys); err != nil {
			return err
		}
		fmt.Println("Database connection and key files are healthy; no credentials displayed.")
		return nil
	case "check-outbox":
		if *maxPendingAge <= 0 || *maxP95Delivery <= 0 || *maxDead < 0 || *window <= 0 {
			return errors.New("outbox alert thresholds and window must be positive; max-dead may be zero")
		}
		report, err := store.OutboxTelemetry(ctx, *window)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(report)
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		if report.Dead > int64(*maxDead) || report.OldestPendingAgeSeconds > maxPendingAge.Seconds() || report.P95DeliverySeconds > maxP95Delivery.Seconds() {
			return errors.New("outbox delivery threshold exceeded")
		}
		return nil
	case "bootstrap":
		// Serialize concurrent bootstrap commands without turning ordinary sign-up into administrator provisioning.
		conn, err := store.DB.Acquire(ctx)
		if err != nil {
			return err
		}
		defer conn.Release()
		if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(89004322)`); err != nil {
			return err
		}
		defer conn.Exec(ctx, `SELECT pg_advisory_unlock(89004322)`)
		var count int
		if err = conn.QueryRow(ctx, `SELECT count(*) FROM identities WHERE role='admin'`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("administrator already exists")
		}
		_, err = store.CreateIdentity(ctx, os.Getenv("FASTCAS_ADMIN_EMAIL"), env("FASTCAS_ADMIN_NAME", "FastCAS Administrator"), os.Getenv("FASTCAS_ADMIN_PASSWORD"), "admin")
		return err
	case "register-client":
		if *file == "" {
			return errors.New("--file is required")
		}
		data, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		var c core.Client
		if err = json.Unmarshal(data, &c); err != nil {
			return err
		}
		return store.RegisterClient(ctx, c, os.Getenv("FASTCAS_CLIENT_SECRET"))
	case "serve":
		store.Keys, err = core.LoadKeys(*keys)
		if err != nil {
			return err
		}
		api, err := httpapi.New(store, env("FASTCAS_ISSUER", "http://127.0.0.1:8900"), store.Dev)
		if err != nil {
			return err
		}
		if secret := os.Getenv("FASTCAS_PROXY_SECRET"); secret != "" {
			if len(secret) < 32 {
				return errors.New("FASTCAS_PROXY_SECRET must be at least 32 bytes")
			}
			api.ProxySecret = secret
		}
		var handler http.Handler = api
		if uiDir := os.Getenv("FASTCAS_UI_DIR"); uiDir != "" {
			handler, err = httpapi.Console(handler, uiDir)
			if err != nil {
				return err
			}
			api.ConsoleEnabled = true
		}
		server := &http.Server{Addr: env("FASTCAS_LISTEN", "127.0.0.1:8900"), Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
		signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-signalCtx.Done():
					return
				case <-ticker.C:
					for range 32 {
						worked, err := store.DeliverEvent(signalCtx, env("FASTCAS_ISSUER", "http://127.0.0.1:8900"))
						if err != nil {
							slog.Error("event delivery failed")
							break
						}
						if !worked {
							break
						}
					}
				}
			}
		}()
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-signalCtx.Done():
					return
				case <-ticker.C:
					if err := store.Maintenance(signalCtx); err != nil {
						slog.Error("maintenance failed")
					}
				}
			}
		}()
		go func() {
			<-signalCtx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		}()
		slog.Info("FastCAS listening", "address", server.Addr)
		err = server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	default:
		return errors.New("unknown command")
	}
}
