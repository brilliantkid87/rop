// Command ropd is the reference ROP server: the demo resource API with ROP
// Core mounted under /.well-known/rop (Master Prompt §70).
//
// ROP is an experimental protocol / research project; this binary is a
// reference implementation for testing the hypothesis, not production
// software.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	resourceapi "github.com/brilliantkid87/rop/examples/resource-api"
	"github.com/brilliantkid87/rop/internal/authz"
	"github.com/brilliantkid87/rop/internal/clock"
	"github.com/brilliantkid87/rop/internal/dependency"
	"github.com/brilliantkid87/rop/internal/httpapi"
	"github.com/brilliantkid87/rop/internal/operation"
	"github.com/brilliantkid87/rop/internal/planner"
	"github.com/brilliantkid87/rop/internal/reversal"
	"github.com/brilliantkid87/rop/internal/store"
	"github.com/brilliantkid87/rop/internal/verification"
	"github.com/brilliantkid87/rop/pkg/rop"
)

func main() {
	var (
		addr       = flag.String("addr", "127.0.0.1:8080", "listen address")
		dbPath     = flag.String("db", "ropd.db", "SQLite database path")
		migrations = flag.String("migrations", "migrations", "migrations directory")
		scope      = flag.String("scope", "default", "authorization scope")
		createTTL  = flag.Duration("create-ttl", time.Hour, "reversal eligibility window for resource.create")
		updateTTL  = flag.Duration("update-ttl", time.Hour, "reversal eligibility window for resource.update")
		notifyTTL  = flag.Duration("notify-ttl", time.Hour, "reversal eligibility window for resource.notify")
		noReversal = flag.Bool("no-reversal", false, "advertise reversal=false (capability-model demo)")
		noPlanning = flag.Bool("no-planning", false, "advertise planning=false (capability-model demo)")
	)
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx := context.Background()

	st, err := store.Open(*dbPath)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()
	if err := st.Migrate(ctx, *migrations); err != nil {
		logger.Error("migrate", "err", err)
		os.Exit(1)
	}

	api := &resourceapi.API{
		Store:      st,
		Clock:      clock.System{},
		Scope:      *scope,
		CreateTTL:  *createTTL,
		UpdateTTL:  *updateTTL,
		NotifyTTL:  *notifyTTL,
		ProviderID: "rop-demo",
	}
	ops, err := api.Operations()
	if err != nil {
		logger.Error("register operations", "err", err)
		os.Exit(1)
	}
	reg, err := operation.NewRegistry(ops...)
	if err != nil {
		logger.Error("operations invalid", "err", err)
		os.Exit(1)
	}
	// Seed Operation metadata durably (Operation vs Action separation, §5).
	for _, o := range ops {
		row := store.OperationRow{
			OperationID:   o.ID,
			Reversibility: string(o.Reversibility),
			Guarantee:     string(o.Guarantee),
		}
		if o.TTL != 0 {
			s := int64(o.TTL / time.Second)
			row.TTLSeconds = &s
		}
		if o.ReverseOperationID != "" {
			v := o.ReverseOperationID
			row.ReverseOperationID = &v
		}
		if err := st.UpsertOperation(ctx, st.DB(), row); err != nil {
			logger.Error("persist operation metadata", "err", err)
			os.Exit(1)
		}
	}

	principal := authz.Principal{ID: "local", Scopes: map[string]bool{*scope: true}}
	authorizer := authz.ScopeAllow{}
	depSvc := &dependency.Service{Store: st}
	reversalSvc := &reversal.Service{
		Store: st, Clock: clock.System{}, Registry: reg, Authorizer: authorizer,
		Dependencies: depSvc,
	}
	// Restart contract (Master Prompt §60): after startup, park any attempt
	// found RUNNING by a previous crashed process as awaiting reconciliation.
	// Recovery inspects durable evidence; it never concludes failure.
	if n, err := reversalSvc.RecoverAll(ctx); err != nil {
		logger.Error("startup recovery", "err", err)
		os.Exit(1)
	} else if n > 0 {
		logger.Warn("startup recovery parked uncertain attempts", "count", n)
	}
	cfg := httpapi.Config{
		ProviderID: api.ProviderID,
		Clock:      clock.System{},
		Store:      st,
		Registry:   reg,
		Authorizer: authorizer,
		Reversal:   reversalSvc,
		Planner: &planner.Service{
			Store: st, Clock: clock.System{}, Registry: reg, Authorizer: authorizer,
			Dependencies: depSvc,
		},
		Verification: &verification.Service{
			Store: st, Clock: clock.System{}, Registry: reg, Authorizer: authorizer,
		},
		Capabilities: rop.Capabilities{
			Receipts:     true,
			Planning:     !*noPlanning,
			Reversal:     !*noReversal,
			Verification: true,
		},
		Principal: principal,
		Scope:     *scope,
		Logger:    logger,
	}
	ropMux := httpapi.Handler(cfg)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.Handler(ropMux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("ropd listening", "addr", *addr, "scope", *scope,
			"capabilities", cfg.Capabilities)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
}
