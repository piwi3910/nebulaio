// Package server provides the main NebulaIO server initialization and lifecycle.
//
// The server package is the entry point for NebulaIO, responsible for:
//   - Loading and validating configuration
//   - Initializing all subsystems (storage, metadata, auth, etc.)
//   - Starting HTTP servers for S3 API, Admin API, and Console
//   - Managing graceful shutdown and cleanup
//   - Coordinating cluster membership and consensus
//
// The server exposes three HTTP endpoints by default:
//   - Port 9000: S3-compatible API (HTTPS)
//   - Port 9001: Admin REST API + Prometheus metrics (HTTPS)
//   - Port 9002: Web Console static files (HTTPS)
//
// TLS is enabled by default with auto-generated certificates.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
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
	"github.com/piwi3910/nebulaio/internal/lambda"
	"github.com/piwi3910/nebulaio/internal/lifecycle"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/metrics"
	"github.com/piwi3910/nebulaio/internal/object"
	"github.com/piwi3910/nebulaio/internal/security"
	"github.com/piwi3910/nebulaio/internal/storage/backend"
	"github.com/piwi3910/nebulaio/internal/storage/erasure"
	"github.com/piwi3910/nebulaio/internal/storage/fs"
	"github.com/piwi3910/nebulaio/internal/storage/volume"
	"github.com/piwi3910/nebulaio/internal/tiering"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// Server is the main NebulaIO server.
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

	// Placement group manager
	placementGroupMgr *cluster.PlacementGroupManager

	// Health checker
	healthChecker *health.Checker

	// Lifecycle manager
	lifecycleManager *lifecycle.Manager

	// Audit logger
	auditLogger *audit.AuditLogger

	// Tiering service
	tieringService *tiering.AdvancedService

	// TLS manager
	tlsManager *security.TLSManager

	// HTTP servers
	s3Server      *http.Server
	adminServer   *http.Server
	consoleServer *http.Server
}

// Version is the current version of NebulaIO.
const Version = "0.1.0"

// Server configuration constants.
const (
	defaultShardSizeMB         = 64
	defaultShardSizeBytes      = defaultShardSizeMB * 1024 * 1024
	defaultAuditBufferSize     = 1000
	defaultReadTimeoutSec      = 30
	defaultWriteTimeoutSec     = 30
	defaultIdleTimeoutSec      = 120
	defaultCORSMaxAgeSec       = 300
	defaultShutdownTimeoutSec  = 30
	defaultMetricsIntervalSec  = 30
)

// New creates a new NebulaIO server.
func New(cfg *config.Config) (*Server, error) {
	srv := &Server{
		cfg: cfg,
	}

	// Track initialization success for cleanup on failure
	var initSuccess bool

	defer func() {
		if !initSuccess {
			srv.cleanupOnInitFailure()
		}
	}()

	// Initialize metrics
	metrics.Init(cfg.NodeID)
	log.Info().Str("node_id", cfg.NodeID).Msg("Metrics initialized")

	// Initialize Lambda configuration
	if cfg.Lambda.ObjectLambda.MaxTransformSize > 0 {
		lambda.SetMaxTransformSize(cfg.Lambda.ObjectLambda.MaxTransformSize)
		metrics.SetLambdaMaxTransformSize(cfg.Lambda.ObjectLambda.MaxTransformSize)
		log.Info().
			Int64("max_transform_size_bytes", cfg.Lambda.ObjectLambda.MaxTransformSize).
			Msg("Lambda max transform size configured")
	}

	if cfg.Lambda.ObjectLambda.StreamingThreshold > 0 {
		lambda.SetStreamingThreshold(cfg.Lambda.ObjectLambda.StreamingThreshold)
		metrics.SetLambdaStreamingThreshold(cfg.Lambda.ObjectLambda.StreamingThreshold)
		log.Info().
			Int64("streaming_threshold_bytes", cfg.Lambda.ObjectLambda.StreamingThreshold).
			Msg("Lambda streaming threshold configured")
	}

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

	// Set NodeHost for discovery to enable Raft-based leader election awareness
	srv.discovery.SetNodeHost(srv.metaStore.GetNodeHost(), storeConfig.ShardID)

	// Initialize placement group manager for distributed storage
	pgConfig := cluster.PlacementGroupConfig{
		LocalGroupID:       cluster.PlacementGroupID(cfg.Storage.PlacementGroups.LocalGroupID),
		Groups:             make([]cluster.PlacementGroup, 0, len(cfg.Storage.PlacementGroups.Groups)),
		MinNodesForErasure: cfg.Storage.PlacementGroups.MinNodesForErasure,
		ReplicationTargets: make([]cluster.PlacementGroupID, 0, len(cfg.Storage.PlacementGroups.ReplicationTargets)),
	}
	for _, g := range cfg.Storage.PlacementGroups.Groups {
		pgConfig.Groups = append(pgConfig.Groups, cluster.PlacementGroup{
			ID:         cluster.PlacementGroupID(g.ID),
			Name:       g.Name,
			Datacenter: g.Datacenter,
			Region:     g.Region,
			MinNodes:   g.MinNodes,
			MaxNodes:   g.MaxNodes,
		})
	}

	for _, target := range cfg.Storage.PlacementGroups.ReplicationTargets {
		pgConfig.ReplicationTargets = append(pgConfig.ReplicationTargets, cluster.PlacementGroupID(target))
	}

	srv.placementGroupMgr, err = cluster.NewPlacementGroupManager(pgConfig)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize placement group manager, using single-node mode")
	} else {
		log.Info().
			Str("local_group", string(pgConfig.LocalGroupID)).
			Int("total_groups", len(pgConfig.Groups)).
			Msg("Placement group manager initialized")
	}

	// Initialize tiered storage model
	// All storage uses tiered backends - hot tier is the primary/default storage
	// Objects are written to hot tier and can be transitioned to warm/cold via policies
	hotDataDir := filepath.Join(cfg.DataDir, "tiering", "hot")
	warmDataDir := filepath.Join(cfg.DataDir, "tiering", "warm")
	coldDataDir := filepath.Join(cfg.DataDir, "tiering", "cold")

	// Helper function to create a backend based on type
	// This now supports per-tier backend configuration
	createBackend := func(dataDir, tierName string) (backend.Backend, error) {
		// Check if we have tier-specific configuration
		tierBackend := cfg.Storage.Backend

		var tierErasureConfig *config.ErasureStorageConfig

		if cfg.Storage.Tiering.Enabled && cfg.Storage.Tiering.Tiers != nil {
			if tierCfg, ok := cfg.Storage.Tiering.Tiers[tierName]; ok {
				// Use tier-specific backend if configured
				if tierCfg.Backend != "" {
					tierBackend = tierCfg.Backend
				}

				if tierCfg.DataDir != "" {
					dataDir = tierCfg.DataDir
				}

				tierErasureConfig = tierCfg.ErasureConfig
			}
		}

		switch tierBackend {
		case "erasure":
			// Erasure coding provides redundancy through Reed-Solomon encoding
			// Data is split into data shards + parity shards across the tier
			var erasureCfg erasure.Config
			switch {
			case tierErasureConfig != nil:
				// Use tier-specific erasure configuration
				erasureCfg = erasure.Config{
					DataDir:      dataDir,
					DataShards:   tierErasureConfig.DataShards,
					ParityShards: tierErasureConfig.ParityShards,
					ShardSize:    int(tierErasureConfig.ShardSize),
				}
				if erasureCfg.ShardSize == 0 {
					erasureCfg.ShardSize = defaultShardSizeBytes // 64MB default
				}
			case cfg.Storage.DefaultRedundancy.Enabled:
				// Use default redundancy configuration
				erasureCfg = erasure.Config{
					DataDir:      dataDir,
					DataShards:   cfg.Storage.DefaultRedundancy.DataShards,
					ParityShards: cfg.Storage.DefaultRedundancy.ParityShards,
					ShardSize:    defaultShardSizeBytes, // 64MB default
				}
			default:
				// Fallback to preset
				erasureCfg = erasure.ConfigFromPreset(erasure.PresetStandard, dataDir)
			}

			// Attach placement group manager for distributed shard distribution
			// This enables multi-node erasure coding when placement groups are configured
			erasureCfg.PlacementGroupManager = srv.placementGroupMgr

			log.Info().
				Str("tier", tierName).
				Int("data_shards", erasureCfg.DataShards).
				Int("parity_shards", erasureCfg.ParityShards).
				Bool("distributed", srv.placementGroupMgr != nil).
				Msg("Initializing erasure coded storage")

			return erasure.New(erasureCfg, cfg.NodeID)
		case "volume":
			log.Info().Str("tier", tierName).Msg("Initializing volume storage")
			return volume.NewBackend(dataDir)
		case "rawdevice":
			log.Info().Str("tier", tierName).Msg("Initializing raw device storage")
			// Raw device storage is handled separately via TieredDeviceManager
			// For now, fall back to volume storage
			return volume.NewBackend(dataDir)
		case "fs", "":
			log.Info().Str("tier", tierName).Msg("Initializing filesystem storage")
			return fs.New(fs.Config{DataDir: dataDir})
		default:
			return nil, fmt.Errorf("unsupported storage backend: %s (supported: fs, volume, erasure, rawdevice)", tierBackend)
		}
	}

	// Initialize hot tier - this is the PRIMARY storage backend
	// All new objects are written here by default
	hotBackend, err := createBackend(hotDataDir, "hot")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize hot tier backend: %w", err)
	}

	// Use hot tier as the primary storage backend for all S3 operations
	// This ensures all objects start in the hot tier
	srv.storageBackend = &multipartWrapper{Backend: hotBackend}

	log.Info().Str("path", hotDataDir).Msg("Hot tier initialized as primary storage")

	// Initialize warm tier
	warmBackend, err := createBackend(warmDataDir, "warm")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize warm tier, warm tier disabled")

		warmBackend = nil
	} else {
		log.Info().Str("path", warmDataDir).Msg("Warm tier initialized")
	}

	// Initialize cold tier
	coldManager := tiering.NewColdStorageManager()

	coldBackend, err := createBackend(coldDataDir, "cold")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize cold tier backend, cold tier disabled")
	} else {
		coldStorage, err := tiering.NewColdStorage(tiering.ColdStorageConfig{
			Name:    "cold-default",
			Type:    tiering.ColdStorageFileSystem,
			Enabled: true,
		}, coldBackend)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to initialize cold tier storage, cold tier disabled")
		} else {
			coldManager.Register(coldStorage)
			log.Info().Str("path", coldDataDir).Msg("Cold tier initialized")
		}
	}

	// Create advanced tiering service with all tiers
	// The tiering service manages transitions between hot -> warm -> cold
	tieringConfig := tiering.DefaultAdvancedServiceConfig()
	tieringConfig.NodeID = cfg.NodeID

	srv.tieringService, err = tiering.NewAdvancedService(
		tieringConfig,
		hotBackend,
		warmBackend,
		coldManager,
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize tiering service")
	} else {
		log.Info().Msg("Tiering service initialized - all storage uses tiered model")
	}

	// Initialize auth service
	srv.authService = auth.NewService(auth.Config{
		JWTSecret:          cfg.Auth.JWTSecret,
		TokenExpiry:        time.Duration(cfg.Auth.TokenExpiry) * time.Minute,
		RefreshTokenExpiry: time.Duration(cfg.Auth.RefreshTokenExpiry) * time.Hour,
		RootUser:           cfg.Auth.RootUser,
		RootPassword:       cfg.Auth.RootPassword,
	}, srv.metaStore)

	// Initialize bucket service - uses hot tier as primary storage
	srv.bucketService = bucket.NewService(srv.metaStore, srv.storageBackend)

	// Initialize object service - uses hot tier as primary storage
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
		BufferSize: defaultAuditBufferSize,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize audit logger: %w", err)
	}

	log.Info().Str("path", auditLogPath).Msg("Audit logger initialized")

	// Initialize TLS if enabled
	if cfg.TLS.Enabled {
		hostname := cfg.NodeName
		if hostname == "" {
			hostname = "localhost"
		}

		tlsManager, err := security.NewTLSManager(&cfg.TLS, hostname)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize TLS: %w", err)
		}

		srv.tlsManager = tlsManager
		log.Info().
			Str("cert_file", tlsManager.GetCertFile()).
			Str("ca_file", tlsManager.GetCAFile()).
			Bool("require_client_cert", cfg.TLS.RequireClientCert).
			Msg("TLS initialized (secure by default)")
	} else {
		log.Warn().Msg("TLS disabled - running in insecure mode (NOT recommended for production)")
	}

	// Setup HTTP servers
	srv.setupS3Server()
	srv.setupAdminServer()
	srv.setupConsoleServer()

	// Mark initialization as successful to prevent cleanup
	initSuccess = true

	return srv, nil
}

// cleanupOnInitFailure cleans up resources if initialization fails.
func (s *Server) cleanupOnInitFailure() {
	log.Debug().Msg("Cleaning up resources after initialization failure")

	// Close metadata store if initialized
	if s.metaStore != nil {
		err := s.metaStore.Close()
		if err != nil {
			log.Warn().Err(err).Msg("Failed to close metadata store during cleanup")
		}
	}

	// Close storage backend if initialized
	if s.storageBackend != nil {
		if closer, ok := s.storageBackend.(interface{ Close() error }); ok {
			err := closer.Close()
			if err != nil {
				log.Warn().Err(err).Msg("Failed to close storage backend during cleanup")
			}
		}
	}

	// Stop discovery if initialized
	if s.discovery != nil {
		err := s.discovery.Stop()
		if err != nil {
			log.Warn().Err(err).Msg("Failed to stop discovery during cleanup")
		}
	}
}

func (s *Server) setupS3Server() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(apimiddleware.S3SecurityHeaders()) // S3-appropriate security headers
	r.Use(apimiddleware.MetricsMiddleware)   // Add metrics middleware
	r.Use(apimiddleware.RequestID)           // Add S3 request ID and x-amz-id-2 headers
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
		ReadTimeout:  defaultReadTimeoutSec * time.Second,
		WriteTimeout: defaultWriteTimeoutSec * time.Second,
		IdleTimeout:  defaultIdleTimeoutSec * time.Second,
	}

	// Apply TLS configuration if enabled
	if s.tlsManager != nil {
		s.s3Server.TLSConfig = s.tlsManager.GetTLSConfig()
	}
}

func (s *Server) setupAdminServer() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)

	// Security headers middleware (with HSTS enabled when TLS is active)
	securityConfig := apimiddleware.DefaultSecurityHeadersConfig()
	securityConfig.EnableHSTS = s.tlsManager != nil
	r.Use(apimiddleware.SecurityHeaders(securityConfig))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           defaultCORSMaxAgeSec,
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

	// Wire up tiering handler if tiering service is available
	if s.tieringService != nil {
		tieringHandler := admin.NewTieringHandler(s.tieringService)
		adminHandler.SetTieringHandler(tieringHandler)
		log.Info().Msg("Tiering handler registered with admin API")
	}

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
		ReadTimeout:  defaultReadTimeoutSec * time.Second,
		WriteTimeout: defaultWriteTimeoutSec * time.Second,
		IdleTimeout:  defaultIdleTimeoutSec * time.Second,
	}

	// Apply TLS configuration if enabled
	if s.tlsManager != nil {
		s.adminServer.TLSConfig = s.tlsManager.GetTLSConfig()
	}
}

func (s *Server) setupConsoleServer() {
	r := chi.NewRouter()

	// Security headers middleware for web console (with HSTS enabled when TLS is active)
	securityConfig := apimiddleware.DefaultSecurityHeadersConfig()
	securityConfig.EnableHSTS = s.tlsManager != nil
	r.Use(apimiddleware.SecurityHeaders(securityConfig))

	// Serve static files for web console
	// In production, this would serve the built React app
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		// For now, redirect to admin port
		http.Redirect(w, r, fmt.Sprintf("http://localhost:%d", s.cfg.AdminPort), http.StatusTemporaryRedirect)
	})

	s.consoleServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.ConsolePort),
		Handler:      r,
		ReadTimeout:  defaultReadTimeoutSec * time.Second,
		WriteTimeout: defaultWriteTimeoutSec * time.Second,
		IdleTimeout:  defaultIdleTimeoutSec * time.Second,
	}

	// Apply TLS configuration if enabled
	if s.tlsManager != nil {
		s.consoleServer.TLSConfig = s.tlsManager.GetTLSConfig()
	}
}

// Start starts all servers.
func (s *Server) Start(ctx context.Context) error {
	// Ensure root admin user exists
	err := s.ensureRootUser(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure root user: %w", err)
	}

	// Start audit logger
	if s.auditLogger != nil {
		s.auditLogger.Start()
		log.Info().Msg("Audit logger started")
	}

	// Start cluster discovery
	if s.discovery != nil {
		err := s.discovery.Start(ctx)
		if err != nil {
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
		var err error

		if s.tlsManager != nil {
			log.Info().Int("port", s.cfg.S3Port).Msg("Starting S3 API server (TLS enabled)")
			err = s.s3Server.ListenAndServeTLS(s.tlsManager.GetCertFile(), s.tlsManager.GetKeyFile())
		} else {
			log.Info().Int("port", s.cfg.S3Port).Msg("Starting S3 API server")
			err = s.s3Server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("S3 server error: %w", err)
		}

		return nil
	})

	// Start Admin server
	g.Go(func() error {
		var err error

		if s.tlsManager != nil {
			log.Info().Int("port", s.cfg.AdminPort).Msg("Starting Admin API server (TLS enabled)")
			log.Info().Int("port", s.cfg.AdminPort).Msg("Prometheus metrics available at /metrics")
			err = s.adminServer.ListenAndServeTLS(s.tlsManager.GetCertFile(), s.tlsManager.GetKeyFile())
		} else {
			log.Info().Int("port", s.cfg.AdminPort).Msg("Starting Admin API server")
			log.Info().Int("port", s.cfg.AdminPort).Msg("Prometheus metrics available at /metrics")
			err = s.adminServer.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("Admin server error: %w", err)
		}

		return nil
	})

	// Start Console server
	g.Go(func() error {
		var err error

		if s.tlsManager != nil {
			log.Info().Int("port", s.cfg.ConsolePort).Msg("Starting Web Console server (TLS enabled)")
			err = s.consoleServer.ListenAndServeTLS(s.tlsManager.GetCertFile(), s.tlsManager.GetKeyFile())
		} else {
			log.Info().Int("port", s.cfg.ConsolePort).Msg("Starting Web Console server")
			err = s.consoleServer.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("Console server error: %w", err)
		}

		return nil
	})

	// Wait for shutdown signal
	g.Go(func() error {
		<-ctx.Done()
		log.Info().Msg("Shutting down servers...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeoutSec*time.Second)
		defer cancel()

		// Stop cluster discovery first (graceful leave)
		if s.discovery != nil {
			err := s.discovery.Stop()
			if err != nil {
				log.Error().Err(err).Msg("Error stopping cluster discovery")
			}
		}

		// Stop lifecycle manager
		if s.lifecycleManager != nil {
			s.lifecycleManager.Stop()
		}

		// Shutdown all servers
		err := s.s3Server.Shutdown(shutdownCtx)
		if err != nil {
			log.Error().Err(err).Msg("Error shutting down S3 server")
		}
		err = s.adminServer.Shutdown(shutdownCtx)

		if err != nil {
			log.Error().Err(err).Msg("Error shutting down Admin server")
		}
		err = s.consoleServer.Shutdown(shutdownCtx)

		if err != nil {
			log.Error().Err(err).Msg("Error shutting down Console server")
		}

		// Stop audit logger
		if s.auditLogger != nil {
			s.auditLogger.Stop()
			log.Info().Msg("Audit logger stopped")
		}

		// Close metadata store
		err = s.metaStore.Close()
		if err != nil {
			log.Error().Err(err).Msg("Error closing metadata store")
		}

		return nil
	})

	return g.Wait()
}

// runMetricsCollector periodically collects and updates metrics.
func (s *Server) runMetricsCollector(ctx context.Context) {
	ticker := time.NewTicker(defaultMetricsIntervalSec * time.Second)
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

// collectMetrics gathers system metrics and updates Prometheus gauges.
func (s *Server) collectMetrics(ctx context.Context) {
	// Update Raft state metrics
	shardID := strconv.FormatUint(s.cfg.Cluster.ShardID, 10)
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

// ensureRootUser ensures the root admin user exists on startup.
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

// Discovery returns the cluster discovery instance.
func (s *Server) Discovery() *cluster.Discovery {
	return s.discovery
}

// MetaStore returns the metadata store (for admin API).
func (s *Server) MetaStore() *metadata.DragonboatStore {
	return s.metaStore
}

// lifecycleObjectService adapts object.Service to lifecycle.ObjectService interface.
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
// This is used for backends that don't yet support multipart uploads natively.
type multipartWrapper struct {
	backend.Backend
}

func (m *multipartWrapper) CreateMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return errors.New("multipart uploads not yet supported by volume backend")
}

func (m *multipartWrapper) PutPart(ctx context.Context, bucket, key, uploadID string, partNumber int, reader io.Reader, size int64) (*backend.PutResult, error) {
	return nil, errors.New("multipart uploads not yet supported by volume backend")
}

func (m *multipartWrapper) GetPart(ctx context.Context, bucket, key, uploadID string, partNumber int) (io.ReadCloser, error) {
	return nil, errors.New("multipart uploads not yet supported by volume backend")
}

func (m *multipartWrapper) CompleteParts(ctx context.Context, bucket, key, uploadID string, parts []int) (*backend.PutResult, error) {
	return nil, errors.New("multipart uploads not yet supported by volume backend")
}

func (m *multipartWrapper) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return errors.New("multipart uploads not yet supported by volume backend")
}
