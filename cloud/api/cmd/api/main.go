package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/bright-guard/bright-guard/cloud/api/internal/api"
	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/config"
	"github.com/bright-guard/bright-guard/cloud/api/internal/db"
	"github.com/bright-guard/bright-guard/cloud/api/internal/email"
	"github.com/bright-guard/bright-guard/cloud/api/internal/policy"
	"github.com/bright-guard/bright-guard/cloud/api/internal/scheduler"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

const sessionTTL = 30 * 24 * time.Hour

func main() {
	// .env is best-effort — production environments inject env directly.
	_ = godotenv.Load()

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if cfg.DevLoginEnabled {
		log.Println("============================================================")
		log.Println(" WARNING: DEV_LOGIN_ENABLED=true — /auth/dev/login is OPEN.")
		log.Println(" This must NEVER be enabled in production.")
		log.Println("============================================================")
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(rootCtx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(rootCtx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations applied")

	users := &store.Users{Pool: pool}
	orgs := &store.Orgs{Pool: pool}
	sessions := &store.Sessions{Pool: pool, TTL: sessionTTL, Secret: []byte(cfg.SessionSecret)}
	gateways := &store.Gateways{Pool: pool, Secret: []byte(cfg.SessionSecret)}
	discovery := &store.Discovery{Pool: pool}
	activity := &store.Activity{Pool: pool}
	deviceAuth := &store.DeviceAuth{Pool: pool, Secret: []byte(cfg.SessionSecret)}

	aead, err := store.NewAEAD([]byte(cfg.SessionSecret))
	if err != nil {
		log.Fatalf("aead: %v", err)
	}
	connections := &store.Connections{Pool: pool, AEAD: aead}
	callers := &store.Callers{Pool: pool}
	invitations := &store.Invitations{Pool: pool}
	platform := &store.Platform{Pool: pool, SeedEmails: cfg.PlatformAdminSeedEmails}
	users.Platform = platform

	// Build the configured mail sender. Stub is the default for local dev; we
	// only fall through to gcp_email when explicitly requested AND credentials
	// resolve. On failure we log loudly and fall back to the stub so the API
	// still boots — better to drop one email than to crash a release.
	var mailer email.Sender = &email.StubSender{From: cfg.EmailFrom}
	if cfg.EmailProvider == "gcp_email" {
		gcp, err := email.NewGCPSender(rootCtx, cfg.GCPProject, cfg.GCPEmailLocation, cfg.EmailFrom)
		if err != nil {
			log.Printf("email: gcp init failed, falling back to stub: %v", err)
		} else {
			mailer = gcp
			log.Printf("email: using GCP Cloud Email (project=%s from=%s)", cfg.GCPProject, cfg.EmailFrom)
		}
	} else {
		log.Printf("email: using stub sender (EMAIL_PROVIDER=%q from=%s)", cfg.EmailProvider, cfg.EmailFrom)
	}

	discoveryInterval := time.Hour
	if v := os.Getenv("DISCOVERY_INTERVAL_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			discoveryInterval = time.Duration(n) * time.Minute
		}
	}
	sched := scheduler.New(connections, discovery, discoveryInterval)
	go sched.Run(rootCtx)

	exposureSweepInterval := 10 * time.Minute
	if v := os.Getenv("EXPOSURE_SWEEP_INTERVAL_MINUTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			exposureSweepInterval = time.Duration(n) * time.Minute
		}
	}
	exposureSweep := scheduler.NewExposureSweep(connections, discovery, exposureSweepInterval)
	go exposureSweep.Run(rootCtx)

	callerSweep := scheduler.NewCallerSweeper(callers, connections, 5*time.Minute)
	go callerSweep.Run(rootCtx)

	policies := &store.Policies{Pool: pool}
	policyEngine, err := policy.New()
	if err != nil {
		log.Fatalf("policy engine: %v", err)
	}
	policySweep := scheduler.NewPolicySweeper(policies, connections, policyEngine, 30*time.Second)
	go policySweep.Run(rootCtx)

	cookieOpt := auth.CookieOpts{
		Secure:   cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}

	google, err := auth.NewGoogle(rootCtx, cfg, users, orgs, sessions, cookieOpt)
	if err != nil {
		log.Fatalf("google oidc: %v", err)
	}
	if google == nil {
		log.Println("google oauth not configured — /auth/google/start will return 503")
	}

	var dev *auth.DevLogin
	if cfg.DevLoginEnabled {
		dev = &auth.DevLogin{
			Users:     users,
			Sessions:  sessions,
			CookieOpt: cookieOpt,
		}
	}

	srv := &api.Server{
		Cfg:         cfg,
		Users:       users,
		Orgs:        orgs,
		Sessions:    sessions,
		Gateways:    gateways,
		Discovery:   discovery,
		Activity:    activity,
		DeviceAuth:  deviceAuth,
		Connections: connections,
		Callers:     callers,
		Invitations: invitations,
		Email:       mailer,
		Platform:    platform,
		Policies:    policies,
		Scheduler:   sched,
		PolicySweep: policySweep,
		PolicyEngine: policyEngine,
		Google:      google,
		Dev:         dev,
		Cookie:      cookieOpt,
		ServeSPA:    os.Getenv("SERVE_SPA") == "true",
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-rootCtx.Done():
		log.Println("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			log.Fatalf("server: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
