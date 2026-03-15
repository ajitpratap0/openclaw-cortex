package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/api"
	"github.com/ajitpratap0/openclaw-cortex/internal/memgraph"
	"github.com/ajitpratap0/openclaw-cortex/internal/recall"
)

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP/JSON API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			emb := newEmbedder(logger)
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return fmt.Errorf("serve: connecting to store: %w", err)
			}
			defer func() { _ = st.Close() }()

			rec := recall.NewRecaller(recallWeightsFromConfig(cfg.Recall.Weights), logger)

			// Wire graph client — MemgraphStore implements graph.Client.
			gc := memgraph.NewGraphAdapter(st)
			rec.SetGraphClient(gc, st, cfg.Recall.GraphBudgetCLIMs)

			srv := api.NewServer(st, rec, emb, logger, cfg.API.AuthToken)

			if cfg.API.AuthToken == "" {
				logger.Warn("HTTP API: auth is DISABLED; set OPENCLAW_CORTEX_API_AUTH_TOKEN or cfg.api.auth_token for production use")
			}

			httpSrv := &http.Server{
				Addr:              cfg.API.ListenAddr,
				Handler:           srv.Handler(),
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      60 * time.Second,
				IdleTimeout:       120 * time.Second,
			}

			errCh := make(chan error, 1)
			go func() {
				logger.Info("HTTP API server starting", "addr", cfg.API.ListenAddr)
				if listenErr := httpSrv.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed {
					errCh <- fmt.Errorf("serve: HTTP server: %w", listenErr)
				}
				close(errCh)
			}()

			select {
			case <-cmd.Context().Done():
				logger.Info("shutting down")
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
