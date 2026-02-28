package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
)

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP/JSON API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()

			emb := newEmbedder(logger)
			st, err := newStore(logger)
			if err != nil {
				return fmt.Errorf("serve: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			rec := recall.NewRecaller(recall.DefaultWeights(), logger)

			srv := api.NewServer(st, rec, emb, logger, cfg.API.AuthToken)

			httpSrv := &http.Server{
				Addr:    cfg.API.ListenAddr,
				Handler: srv.Handler(),
			}

			// Listen for OS signals to trigger graceful shutdown.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			errCh := make(chan error, 1)
			go func() {
				logger.Info("HTTP API server starting", "addr", cfg.API.ListenAddr)
				if listenErr := httpSrv.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed {
					errCh <- fmt.Errorf("serve: HTTP server: %w", listenErr)
				}
				close(errCh)
			}()

			select {
			case sig := <-sigCh:
				logger.Info("shutting down", "signal", sig)
			case startErr := <-errCh:
				if startErr != nil {
					return startErr
				}
				return nil
			}

			const shutdownTimeout = 10 * time.Second
			if shutdownErr := api.Shutdown(httpSrv, shutdownTimeout); shutdownErr != nil {
				return fmt.Errorf("serve: graceful shutdown: %w", shutdownErr)
			}

			// Drain the errCh in case ListenAndServe returned after Shutdown.
			if startErr := <-errCh; startErr != nil {
				return startErr
			}

			return nil
		},
	}
	return cmd
}
