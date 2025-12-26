package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/piwi3910/nebulaio/internal/api/admin"
	"github.com/piwi3910/nebulaio/internal/api/console"
	"github.com/piwi3910/nebulaio/internal/api/s3"
	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/piwi3910/nebulaio/internal/bucket"
	"github.com/piwi3910/nebulaio/internal/config"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/object"
	"github.com/piwi3910/nebulaio/internal/storage/fs"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// Server is the main NebulaIO server
type Server struct {
	cfg *config.Config

	// Core services
	metaStore     metadata.Store
	storageBackend object.StorageBackend
	authService   *auth.Service
	bucketService *bucket.Service
	objectService *object.Service

	// HTTP servers
	s3Server      *http.Server
	adminServer   *http.Server
	consoleServer *http.Server
}

// New creates a new NebulaIO server
func New(cfg *config.Config) (*Server, error) {
	srv := &Server{
		cfg: cfg,
	}

	// Initialize metadata store (Raft-backed)
	var err error
	srv.metaStore, err = metadata.NewRaftStore(metadata.RaftConfig{
		NodeID:    cfg.NodeID,
		DataDir:   cfg.DataDir,
		Bootstrap: cfg.Cluster.Bootstrap,
		RaftBind:  fmt.Sprintf(":%d", cfg.Cluster.RaftPort),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize metadata store: %w", err)
	}

	// Initialize storage backend
	srv.storageBackend, err = fs.New(fs.Config{
		DataDir: cfg.DataDir,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage backend: %w", err)
	}

	// Initialize auth service
	srv.authService = auth.NewService(auth.Config{
		JWTSecret:          cfg.Auth.JWTSecret,
		TokenExpiry:        time.Duration(cfg.Auth.TokenExpiry) * time.Minute,
		RefreshTokenExpiry: time.Duration(cfg.Auth.RefreshTokenExpiry) * time.Hour,
		RootUser:           cfg.Auth.RootUser,
		RootPassword:       cfg.Auth.RootPassword,
	}, srv.metaStore)

	// Initialize bucket service
	srv.bucketService = bucket.NewService(srv.metaStore, srv.storageBackend)

	// Initialize object service
	srv.objectService = object.NewService(srv.metaStore, srv.storageBackend, srv.bucketService)

	// Setup HTTP servers
	srv.setupS3Server()
	srv.setupAdminServer()
	srv.setupConsoleServer()

	return srv, nil
}

func (s *Server) setupS3Server() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s3LoggerMiddleware)

	// S3 API handlers
	s3Handler := s3.NewHandler(s.authService, s.bucketService, s.objectService)
	s3Handler.RegisterRoutes(r)

	s.s3Server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.S3Port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

func (s *Server) setupAdminServer() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Admin API handlers
	adminHandler := admin.NewHandler(s.authService, s.bucketService, s.objectService, s.metaStore)
	r.Route("/api/v1/admin", func(r chi.Router) {
		adminHandler.RegisterRoutes(r)
	})

	// Console API handlers (user-facing)
	consoleHandler := console.NewHandler(s.authService, s.bucketService, s.objectService)
	r.Route("/api/v1/console", func(r chi.Router) {
		consoleHandler.RegisterRoutes(r)
	})

	s.adminServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.AdminPort),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

func (s *Server) setupConsoleServer() {
	r := chi.NewRouter()

	// Serve static files for web console
	// In production, this would serve the built React app
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// For now, redirect to admin port
		http.Redirect(w, r, fmt.Sprintf("http://localhost:%d", s.cfg.AdminPort), http.StatusTemporaryRedirect)
	})

	s.consoleServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.ConsolePort),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// Start starts all servers
func (s *Server) Start(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	// Start S3 server
	g.Go(func() error {
		log.Info().Int("port", s.cfg.S3Port).Msg("Starting S3 API server")
		if err := s.s3Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("S3 server error: %w", err)
		}
		return nil
	})

	// Start Admin server
	g.Go(func() error {
		log.Info().Int("port", s.cfg.AdminPort).Msg("Starting Admin API server")
		if err := s.adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("Admin server error: %w", err)
		}
		return nil
	})

	// Start Console server
	g.Go(func() error {
		log.Info().Int("port", s.cfg.ConsolePort).Msg("Starting Web Console server")
		if err := s.consoleServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("Console server error: %w", err)
		}
		return nil
	})

	// Wait for shutdown signal
	g.Go(func() error {
		<-ctx.Done()
		log.Info().Msg("Shutting down servers...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Shutdown all servers
		if err := s.s3Server.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Error shutting down S3 server")
		}
		if err := s.adminServer.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Error shutting down Admin server")
		}
		if err := s.consoleServer.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Error shutting down Console server")
		}

		// Close metadata store
		if err := s.metaStore.Close(); err != nil {
			log.Error().Err(err).Msg("Error closing metadata store")
		}

		return nil
	})

	return g.Wait()
}

func s3LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", ww.Status()).
			Dur("duration", time.Since(start)).
			Msg("S3 request")
	})
}
