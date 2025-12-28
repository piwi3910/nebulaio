package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/piwi3910/nebulaio/internal/api/admin"
	"github.com/piwi3910/nebulaio/internal/api/console"
	apimiddleware "github.com/piwi3910/nebulaio/internal/api/middleware"
	"github.com/piwi3910/nebulaio/internal/api/s3"
	"github.com/piwi3910/nebulaio/internal/audit"
	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/piwi3910/nebulaio/internal/bucket"
	"github.com/piwi3910/nebulaio/internal/cluster"
	"github.com/piwi3910/nebulaio/internal/config"
	"github.com/piwi3910/nebulaio/internal/health"
	"github.com/piwi3910/nebulaio/internal/lifecycle"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/metrics"
	"github.com/piwi3910/nebulaio/internal/object"
	"github.com/piwi3910/nebulaio/internal/storage/backend"
	"github.com/piwi3910/nebulaio/internal/storage/fs"
	"github.com/piwi3910/nebulaio/internal/storage/volume"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// Server is the main NebulaIO server
type Server struct {
	cfg *config.Config

	// Core services
	metaStore      *metadata.DragonboatStore
	storageBackend backend.MultipartBackend
	authService    *auth.Service
	bucketService  *bucket.Service
	objectService  *object.Service

	// Cluster discovery
	discovery *cluster.Discovery

	// Health checker
	healthChecker *health.Checker

	// Lifecycle manager
	lifecycleManager *lifecycle.Manager

	// Audit logger
	auditLogger *audit.AuditLogger

	// HTTP servers
	s3Server      *http.Server
	adminServer   *http.Server
	consoleServer *http.Server
}

// Version is the current version of NebulaIO
const Version = "0.1.0"

// New creates a new NebulaIO server
func New(cfg *config.Config) (*Server, error) {
	srv := &Server{
		cfg: cfg,
	}

	// Initialize metrics
	metrics.Init(cfg.NodeID)
	log.Info().Str("node_id", cfg.NodeID).Msg("Metrics initialized")

	// Initialize metadata store (Dragonboat-backed)
	// Use advertise address for raft binding if specified, otherwise use localhost for single-node
	raftBindAddr := cfg.Cluster.AdvertiseAddress
	if raftBindAddr == "" {
		raftBindAddr = "127.0.0.1"
	}
	var err error

	// Create Dragonboat store configuration
	storeConfig := metadata.DragonboatConfig{
		NodeID:      cfg.Cluster.ReplicaID,
		ShardID:     cfg.Cluster.ShardID,
		DataDir:     cfg.DataDir,
		RaftAddress: fmt.Sprintf("%s:%d", raftBindAddr, cfg.Cluster.RaftPort),
		Bootstrap:   cfg.Cluster.Bootstrap,
	}

	if cfg.Cluster.Bootstrap {
		storeConfig.InitialMembers = map[uint64]string{
			cfg.Cluster.ReplicaID: storeConfig.RaftAddress,
		}
	}

	srv.metaStore, err = metadata.NewDragonboatStore(storeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize metadata store: %w", err)
	}

	// Initialize cluster discovery
	srv.discovery = cluster.NewDiscovery(cluster.DiscoveryConfig{
		NodeID:        cfg.NodeID,
		AdvertiseAddr: cfg.Cluster.AdvertiseAddress,
		JoinAddresses: cfg.Cluster.JoinAddresses,
		GossipPort:    cfg.Cluster.GossipPort,
		RaftPort:      cfg.Cluster.RaftPort,
		S3Port:        cfg.S3Port,
		AdminPort:     cfg.AdminPort,
		Role:          cfg.Cluster.NodeRole,
		Version:       Version,
	})

	// Set NodeHost for discovery
	// TODO: Temporarily commented out until DragonboatStore is fully implemented
	// Once DragonboatStore exists with GetNodeHost() method, uncomment this:
	// srv.discovery.SetNodeHost(srv.metaStore.GetNodeHost(), storeConfig.ShardID)
	
	// Old RaftStore approach (no longer compatible):
	// srv.discovery.SetRaft(srv.metaStore.GetRaft())

	// Initialize storage backend based on configuration
	switch cfg.Storage.Backend {
	case "volume":
		log.Info().Msg("Initializing volume storage backend")
		volumeBackend, err := volume.NewBackend(cfg.DataDir)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize volume storage backend: %w", err)
		}
		// Volume backend implements Backend but not MultipartBackend yet
		// Wrap it with a multipart adapter
		srv.storageBackend = &multipartWrapper{Backend: volumeBackend}
	case "fs", "":
		log.Info().Msg("Initializing filesystem storage backend")
		srv.storageBackend, err = fs.New(fs.Config{
			DataDir: cfg.DataDir,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize storage backend: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported storage backend: %s (supported: fs, volume)", cfg.Storage.Backend)
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

	// Initialize health checker
	srv.healthChecker = health.NewChecker(srv.metaStore, srv.storageBackend)

	// Initialize lifecycle manager
	srv.lifecycleManager = lifecycle.NewManager(
		srv.metaStore,
		&lifecycleObjectService{srv.objectService},
		srv.objectService,
	)

	// Initialize audit logger
	auditLogPath := filepath.Join(cfg.DataDir, "audit.log")
	srv.auditLogger, err = audit.NewAuditLogger(audit.Config{
		Store:      srv.metaStore,
		FilePath:   auditLogPath,
		BufferSize: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize audit logger: %w", err)
	}
	log.Info().Str("path", auditLogPath).Msg("Audit logger initialized")

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
	r.Use(apimiddleware.MetricsMiddleware) // Add metrics middleware
	r.Use(apimiddleware.RequestID)         // Add S3 request ID and x-amz-id-2 headers
	r.Use(s3LoggerMiddleware)

	// Audit middleware for S3 operations
	s3AuditMiddleware := apimiddleware.NewAuditMiddleware(s.auditLogger, audit.SourceS3)
	r.Use(s3AuditMiddleware.Handler)

	// S3 CORS middleware - handles bucket-specific CORS configuration
	s3CORSMiddleware := apimiddleware.NewS3CORSMiddleware(s.bucketService)
	r.Use(s3CORSMiddleware.Handler)

	// S3 authentication middleware
	// AllowAnonymous is true to support public bucket operations
	// Bucket policies determine actual access at the handler level
	s3AuthConfig := apimiddleware.S3AuthConfig{
		AuthService:    s.authService,
		Region:         "",   // Empty string defaults to "us-east-1"
		AllowAnonymous: true, // Allow anonymous access, bucket policies will be checked
	}
	r.Use(apimiddleware.S3Auth(s3AuthConfig))

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

	// Audit middleware for Admin operations
	adminAuditMiddleware := apimiddleware.NewAuditMiddleware(s.auditLogger, audit.SourceAdmin)
	r.Use(adminAuditMiddleware.Handler)

	// Health check handlers
	healthHandler := health.NewHandler(s.healthChecker)
	r.Get("/health", healthHandler.HealthHandler)
	r.Get("/health/live", healthHandler.LivenessHandler)
	r.Get("/health/ready", healthHandler.ReadinessHandler)

	// Prometheus metrics endpoint
	r.Handle("/metrics", promhttp.Handler())

	// Admin API handlers
	adminHandler := admin.NewHandler(s.authService, s.bucketService, s.objectService, s.metaStore, s.discovery)
	r.Route("/api/v1/admin", func(r chi.Router) {
		adminHandler.RegisterRoutes(r)
		// Detailed health endpoint under admin API
		r.Get("/health/detailed", healthHandler.DetailedHandler)

		// Cluster management endpoints
		clusterHandler := admin.NewClusterHandler(s.discovery, s.metaStore)
		clusterHandler.RegisterClusterRoutes(r)
	})

	// Console API handlers (user-facing)
	consoleHandler := console.NewHandler(s.authService, s.bucketService, s.objectService, s.metaStore)
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
	// Ensure root admin user exists
	if err := s.ensureRootUser(ctx); err != nil {
		return fmt.Errorf("failed to ensure root user: %w", err)
	}

	// Start audit logger
	if s.auditLogger != nil {
		s.auditLogger.Start()
		log.Info().Msg("Audit logger started")
	}

	// Start cluster discovery
	if s.discovery != nil {
		if err := s.discovery.Start(ctx); err != nil {
			return fmt.Errorf("failed to start cluster discovery: %w", err)
		}
		log.Info().
			Str("node_id", s.cfg.NodeID).
			Int("gossip_port", s.cfg.Cluster.GossipPort).
			Strs("join_addresses", s.cfg.Cluster.JoinAddresses).
			Msg("Cluster discovery started")
	}

	g, ctx := errgroup.WithContext(ctx)

	// Start metrics collector background goroutine
	g.Go(func() error {
		s.runMetricsCollector(ctx)
		return nil
	})

	// Start lifecycle manager background goroutine
	g.Go(func() error {
		s.lifecycleManager.Start(ctx)
		return nil
	})

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
		log.Info().Int("port", s.cfg.AdminPort).Msg("Prometheus metrics available at /metrics")
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

		// Stop cluster discovery first (graceful leave)
		if s.discovery != nil {
			if err := s.discovery.Stop(); err != nil {
				log.Error().Err(err).Msg("Error stopping cluster discovery")
			}
		}

		// Stop lifecycle manager
		if s.lifecycleManager != nil {
			s.lifecycleManager.Stop()
		}

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

		// Stop audit logger
		if s.auditLogger != nil {
			s.auditLogger.Stop()
			log.Info().Msg("Audit logger stopped")
		}

		// Close metadata store
		if err := s.metaStore.Close(); err != nil {
			log.Error().Err(err).Msg("Error closing metadata store")
		}

		return nil
	})

	return g.Wait()
}

// runMetricsCollector periodically collects and updates metrics
func (s *Server) runMetricsCollector(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Collect initial metrics
	s.collectMetrics(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.collectMetrics(ctx)
		}
	}
}

// collectMetrics gathers system metrics and updates Prometheus gauges
func (s *Server) collectMetrics(ctx context.Context) {
	// Update Raft state metrics
	shardID := fmt.Sprintf("%d", s.cfg.Cluster.ShardID)
	isLeader := s.metaStore.IsLeader()
	metrics.SetRaftLeader(shardID, isLeader)

	// Get cluster info and update node count
	if clusterInfo, err := s.metaStore.GetClusterInfo(ctx); err == nil {
		metrics.SetClusterNodesTotal(len(clusterInfo.Nodes))
	}

	// Update storage metrics
	if storageInfo, err := s.storageBackend.GetStorageInfo(ctx); err == nil {
		metrics.SetStorageStats(storageInfo.UsedBytes, storageInfo.TotalBytes)
	}

	// Update bucket count
	if buckets, err := s.metaStore.ListBuckets(ctx, ""); err == nil {
		metrics.SetBucketsTotal(len(buckets))
	}

	// Update multipart uploads count (approximate - count for all buckets)
	var multipartCount int
	if buckets, err := s.metaStore.ListBuckets(ctx, ""); err == nil {
		for _, bucket := range buckets {
			if uploads, err := s.metaStore.ListMultipartUploads(ctx, bucket.Name); err == nil {
				multipartCount += len(uploads)
			}
		}
	}
	metrics.SetMultipartUploadsActive(multipartCount)
}

// ensureRootUser ensures the root admin user exists on startup
func (s *Server) ensureRootUser(ctx context.Context) error {
	log.Info().Str("username", s.cfg.Auth.RootUser).Msg("Ensuring root admin user exists")

	created, err := s.authService.EnsureRootUser(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to ensure root user")
		return err
	}

	if created {
		log.Info().
			Str("username", s.cfg.Auth.RootUser).
			Msg("Root admin user created successfully")
	} else {
		log.Debug().
			Str("username", s.cfg.Auth.RootUser).
			Msg("Root admin user already exists")
	}

	return nil
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

// Discovery returns the cluster discovery instance
func (s *Server) Discovery() *cluster.Discovery {
	return s.discovery
}

// MetaStore returns the metadata store (for admin API)
func (s *Server) MetaStore() *metadata.DragonboatStore {
	return s.metaStore
}

// lifecycleObjectService adapts object.Service to lifecycle.ObjectService interface
type lifecycleObjectService struct {
	svc *object.Service
}

func (l *lifecycleObjectService) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := l.svc.DeleteObject(ctx, bucket, key)
	return err
}

func (l *lifecycleObjectService) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	_, err := l.svc.DeleteObjectVersion(ctx, bucket, key, versionID)
	return err
}

func (l *lifecycleObjectService) TransitionStorageClass(ctx context.Context, bucket, key, targetClass string) error {
	return l.svc.TransitionStorageClass(ctx, bucket, key, targetClass)
}

// multipartWrapper wraps a basic Backend to provide a stub MultipartBackend interface
// This is used for backends that don't yet support multipart uploads natively
type multipartWrapper struct {
	backend.Backend
}

func (m *multipartWrapper) CreateMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return fmt.Errorf("multipart uploads not yet supported by volume backend")
}

func (m *multipartWrapper) PutPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*backend.PutResult, error) {
	return nil, fmt.Errorf("multipart uploads not yet supported by volume backend")
}

func (m *multipartWrapper) GetPart(ctx context.Context, bucket, key, uploadID string, partNumber int) (io.ReadCloser, error) {
	return nil, fmt.Errorf("multipart uploads not yet supported by volume backend")
}

func (m *multipartWrapper) CompleteParts(ctx context.Context, bucket, key, uploadID string, parts []int) (*backend.PutResult, error) {
	return nil, fmt.Errorf("multipart uploads not yet supported by volume backend")
}

func (m *multipartWrapper) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return fmt.Errorf("multipart uploads not yet supported by volume backend")
}
