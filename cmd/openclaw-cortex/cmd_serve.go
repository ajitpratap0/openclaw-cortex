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
	var unsafeNoAuth bool
	var tlsCert, tlsKey string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP/JSON API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := newLogger()
			ctx := cmd.Context()

			// Auth gate — fail fast unless the operator explicitly opts out.
			if cfg.API.AuthToken == "" && !unsafeNoAuth {
				return fmt.Errorf("serve: api.auth_token is not set; " +
					"set OPENCLAW_CORTEX_API_AUTH_TOKEN or pass --unsafe-no-auth to disable auth (insecure)")
			}
			if cfg.API.AuthToken == "" {
				logger.Warn("HTTP API: auth is DISABLED (--unsafe-no-auth); do not expose this port")
			}

			if (tlsCert == "") != (tlsKey == "") {
				return fmt.Errorf("serve: --tls-cert and --tls-key must both be set or both be empty")
			}

			emb := newEmbedder(logger)
			st, err := newMemgraphStore(ctx, logger)
			if err != nil {
				return cmdErr("serve: connecting to store", err)
			}
			defer func() { _ = st.Close() }()

			rec := recall.NewRecaller(recallWeightsFromConfig(cfg.Recall.Weights), logger)

			// Wire graph client — MemgraphStore implements graph.Client.
			gc := memgraph.NewGraphAdapter(st)
			rec.SetGraphClient(gc, st, cfg.Recall.GraphBudgetCLIMs)

			srv := api.NewServer(st, rec, emb, logger, cfg.API.AuthToken, cfg.API.CursorSecret)

			rl := api.RateLimitMiddleware(ctx, cfg.API.RateLimitRPS, cfg.API.RateLimitBurst)
			httpSrv := &http.Server{
				Addr:              cfg.API.ListenAddr,
				Handler:           rl(srv.Handler()),
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				WriteTimeout:      60 * time.Second,
				IdleTimeout:       120 * time.Second,
			}

			startServer := func() error {
				if tlsCert != "" && tlsKey != "" {
					logger.Info("HTTP API server starting (TLS)", "addr", cfg.API.ListenAddr)
					return httpSrv.ListenAndServeTLS(tlsCert, tlsKey)
				}
				logger.Info("HTTP API server starting", "addr", cfg.API.ListenAddr)
				return httpSrv.ListenAndServe()
			}

			errCh := make(chan error, 1)
			go func() {
				if listenErr := startServer(); listenErr != nil && listenErr != http.ErrServerClosed {
					errCh <- cmdErr("serve: HTTP server", listenErr)
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
				return cmdErr("serve: graceful shutdown", shutdownErr)
			}

			// Drain the errCh in case ListenAndServe returned after Shutdown.
			if startErr := <-errCh; startErr != nil {
				return startErr
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&unsafeNoAuth, "unsafe-no-auth", false,
		"Allow serving without authentication (insecure)")
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "",
		"Path to TLS certificate file (PEM). Must be paired with --tls-key.")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "",
		"Path to TLS private key file (PEM). Must be paired with --tls-cert.")

	return cmd
}
