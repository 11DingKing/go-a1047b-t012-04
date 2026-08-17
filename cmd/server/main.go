// Package main launches the Arctic Express coordination platform.
//
// The service listens on :58021 and exposes the REST API defined in
// internal/transport. It keeps all state in-process (persistence package:
// internal/store) and runs background sweeps for the 72h deposit auto-release
// and the 10-minute temperature-breach standby transfer.
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

	"arcticexpress/internal/domain"
	"arcticexpress/internal/service"
	"arcticexpress/internal/store"
	"arcticexpress/internal/transport"
	"arcticexpress/internal/worker"
)

func main() {
	logger := log.New(os.Stdout, "arctic-express ", log.LstdFlags|log.Lmsgprefix)

	st := store.New()
	clock := domain.SystemClock{}

	booking := service.NewBookingService(st, clock)
	route := service.NewRouteService(st, clock)
	warehouse := service.NewWarehouseService(st, clock)
	dispatch := service.NewDispatchService(st, clock)
	customs := service.NewCustomsService(st, clock)
	lock := service.NewLockService(st, clock)

	wk := worker.New(st, clock, booking, warehouse)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go wk.Start(ctx, 30*time.Second)

	srv := transport.NewServer(st, booking, route, warehouse, dispatch, customs, lock)
	httpServer := &http.Server{
		Addr:              transport.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Printf("listening on %s", transport.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Printf("shutting down")
	shutdownCtx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()
	_ = httpServer.Shutdown(shutdownCtx)
}
