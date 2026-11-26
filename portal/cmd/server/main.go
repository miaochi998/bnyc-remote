// Command server runs the bnyc-remote portal (accounts + address book).
// R1: all configuration comes from the environment; no secrets are embedded.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/miaochi998/bnyc-remote/portal/internal/api"
	"github.com/miaochi998/bnyc-remote/portal/internal/auth"
	"github.com/miaochi998/bnyc-remote/portal/internal/config"
	"github.com/miaochi998/bnyc-remote/portal/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("store error: %v", err)
	}
	defer func() { _ = st.Close() }()

	if err := bootstrapAdmin(cfg, st); err != nil {
		log.Fatalf("admin bootstrap error: %v", err)
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.New(cfg, st).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("portal listening on %s", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("portal stopped")
}

// bootstrapAdmin creates the first admin when the database is empty.
// The password is read from the environment and never logged.
func bootstrapAdmin(cfg *config.Config, st *store.Store) error {
	n, err := st.CountUsers()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if cfg.AdminUser == "" || cfg.AdminPass == "" {
		log.Println("no users yet and PORTAL_ADMIN_USER/PORTAL_ADMIN_PASSWORD not set; skip bootstrap")
		return nil
	}
	hash, err := auth.HashPassword(cfg.AdminPass)
	if err != nil {
		return err
	}
	if _, err := st.CreateUser(cfg.AdminUser, hash, "Administrator", "admin"); err != nil {
		return err
	}
	log.Printf("bootstrap admin %q created", cfg.AdminUser)
	return nil
}
