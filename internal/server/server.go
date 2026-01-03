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
	"github.com/piwi3910/nebulaio/internal/shutdown"
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

	// Shutdown coordinator
	shutdownCoord *shutdown.Coordinator

	// HTTP servers
	s3Server      *http.Server
	adminServer   *http.Server
	consoleServer *http.Server
}

// Version is the current version of NebulaIO.
const Version = "0.1.0"

// Server configuration constants.
const (
	defaultShardSizeMB          = 64
	defaultShardSizeBytes       = defaultShardSizeMB * 1024 * 1024
	defaultAuditBufferSize      = 1000
	defaultReadTimeoutSec       = 30
	defaultWriteTimeoutSec      = 30
	defaultIdleTimeoutSec       = 120
	defaultReadHeaderTimeoutSec = 10 // G112: Prevent Slowloris attacks
	defaultCORSMaxAgeSec        = 300
	defaultShutdownTimeoutSec   = 30
	defaultMetricsIntervalSec   = 30
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
	srv.initializeMetrics(cfg)

	// Initialize metadata store and cluster
	if err := srv.initializeCluster(cfg); err != nil {
		return nil, err
	}

	// Initialize storage tiers
	if err := srv.initializeStorage(cfg); err != nil {
		return nil, err
	}

	// Initialize services
	if err := srv.initializeServices(cfg); err != nil {
		return nil, err
	}

	// Initialize TLS
	if err := srv.initializeTLS(cfg); err != nil {
		return nil, err
	}

	// Setup HTTP servers
	srv.setupS3Server()
	srv.setupAdminServer()
	srv.setupConsoleServer()

	// Initialize shutdown coordinator with config
	srv.initializeShutdownCoordinator(cfg)

	// Mark initialization as successful to prevent cleanup
	initSuccess = true

	return srv, nil
}

func (s *Server) initializeMetrics(cfg *config.Config) {
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
}

func (s *Server) initializeShutdownCoordinator(cfg *config.Config) {
	shutdownCfg := shutdown.Config{
		TotalTimeout:    time.Duration(cfg.Shutdown.TotalTimeoutSeconds) * time.Second,
		DrainTimeout:    time.Duration(cfg.Shutdown.DrainTimeoutSeconds) * time.Second,
		WorkerTimeout:   time.Duration(cfg.Shutdown.WorkerTimeoutSeconds) * time.Second,
		HTTPTimeout:     time.Duration(cfg.Shutdown.HTTPTimeoutSeconds) * time.Second,
		MetadataTimeout: time.Duration(cfg.Shutdown.MetadataTimeoutSeconds) * time.Second,
		StorageTimeout:  time.Duration(cfg.Shutdown.StorageTimeoutSeconds) * time.Second,
		ForceTimeout:    time.Duration(cfg.Shutdown.ForceTimeoutSeconds) * time.Second,
	}

	s.shutdownCoord = shutdown.NewCoordinator(shutdownCfg)
	log.Info().
		Dur("total_timeout", shutdownCfg.TotalTimeout).
		Dur("drain_timeout", shutdownCfg.DrainTimeout).
		Msg("Shutdown coordinator initialized")
}

func (s *Server) initializeCluster(cfg *config.Config) error {
	// Initialize metadata store (Dragonboat-backed)
	raftBindAddr := cfg.Cluster.AdvertiseAddress
	if raftBindAddr == "" {
		raftBindAddr = "127.0.0.1"
	}

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

	var err error
	s.metaStore, err = metadata.NewDragonboatStore(storeConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize metadata store: %w", err)
	}

	// Initialize cluster discovery
	s.discovery = cluster.NewDiscovery(cluster.DiscoveryConfig{
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

	s.discovery.SetNodeHost(s.metaStore.GetNodeHost(), storeConfig.ShardID)

	// Initialize placement group manager
	pgConfig := s.buildPlacementGroupConfig(cfg)
	s.placementGroupMgr, err = cluster.NewPlacementGroupManager(pgConfig)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize placement group manager, using single-node mode")
	} else {
		log.Info().
			Str("local_group", string(pgConfig.LocalGroupID)).
			Int("total_groups", len(pgConfig.Groups)).
			Msg("Placement group manager initialized")
	}

	return nil
}

func (s *Server) buildPlacementGroupConfig(cfg *config.Config) cluster.PlacementGroupConfig {
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

	return pgConfig
}

func (s *Server) initializeStorage(cfg *config.Config) error {
	hotDataDir := filepath.Join(cfg.DataDir, "tiering", "hot")
	warmDataDir := filepath.Join(cfg.DataDir, "tiering", "warm")
	coldDataDir := filepath.Join(cfg.DataDir, "tiering", "cold")

	// Initialize hot tier - this is the PRIMARY storage backend
	hotBackend, err := s.createBackend(cfg, hotDataDir, "hot")
	if err != nil {
		return fmt.Errorf("failed to initialize hot tier backend: %w", err)
	}

	s.storageBackend = &multipartWrapper{Backend: hotBackend}
	log.Info().Str("path", hotDataDir).Msg("Hot tier initialized as primary storage")

	// Initialize warm tier
	warmBackend, err := s.createBackend(cfg, warmDataDir, "warm")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize warm tier, warm tier disabled")
		warmBackend = nil
	} else {
		log.Info().Str("path", warmDataDir).Msg("Warm tier initialized")
	}

	// Initialize cold tier
	coldManager, coldBackend := s.initializeColdTier(cfg, coldDataDir)

	// Create tiering service
	tieringConfig := tiering.DefaultAdvancedServiceConfig()
	tieringConfig.NodeID = cfg.NodeID

	s.tieringService, err = tiering.NewAdvancedService(
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

	_ = coldBackend
	return nil
}

func (s *Server) createBackend(cfg *config.Config, dataDir, tierName string) (backend.Backend, error) {
	tierBackend := cfg.Storage.Backend
	var tierErasureConfig *config.ErasureStorageConfig

	if cfg.Storage.Tiering.Enabled && cfg.Storage.Tiering.Tiers != nil {
		if tierCfg, ok := cfg.Storage.Tiering.Tiers[tierName]; ok {
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
		return s.createErasureBackend(cfg, dataDir, tierName, tierErasureConfig)
	case "volume":
		log.Info().Str("tier", tierName).Msg("Initializing volume storage")
		return volume.NewBackend(dataDir)
	case "rawdevice":
		log.Info().Str("tier", tierName).Msg("Initializing raw device storage")
		return volume.NewBackend(dataDir)
	case "fs", "":
		log.Info().Str("tier", tierName).Msg("Initializing filesystem storage")
		return fs.New(fs.Config{DataDir: dataDir})
	default:
		return nil, fmt.Errorf("unsupported storage backend: %s (supported: fs, volume, erasure, rawdevice)", tierBackend)
	}
}

func (s *Server) createErasureBackend(cfg *config.Config, dataDir, tierName string, tierErasureConfig *config.ErasureStorageConfig) (backend.Backend, error) {
	var erasureCfg erasure.Config

	switch {
	case tierErasureConfig != nil:
		erasureCfg = erasure.Config{
			DataDir:      dataDir,
			DataShards:   tierErasureConfig.DataShards,
			ParityShards: tierErasureConfig.ParityShards,
			ShardSize:    int(tierErasureConfig.ShardSize),
		}
		if erasureCfg.ShardSize == 0 {
			erasureCfg.ShardSize = defaultShardSizeBytes
		}
	case cfg.Storage.DefaultRedundancy.Enabled:
		erasureCfg = erasure.Config{
			DataDir:      dataDir,
			DataShards:   cfg.Storage.DefaultRedundancy.DataShards,
			ParityShards: cfg.Storage.DefaultRedundancy.ParityShards,
			ShardSize:    defaultShardSizeBytes,
		}
	default:
		erasureCfg = erasure.ConfigFromPreset(erasure.PresetStandard, dataDir)
	}

	erasureCfg.PlacementGroupManager = s.placementGroupMgr

	log.Info().
		Str("tier", tierName).
		Int("data_shards", erasureCfg.DataShards).
		Int("parity_shards", erasureCfg.ParityShards).
		Bool("distributed", s.placementGroupMgr != nil).
		Msg("Initializing erasure coded storage")

	return erasure.New(erasureCfg, cfg.NodeID)
}

func (s *Server) initializeColdTier(cfg *config.Config, coldDataDir string) (*tiering.ColdStorageManager, backend.Backend) {
	coldManager := tiering.NewColdStorageManager()

	coldBackend, err := s.createBackend(cfg, coldDataDir, "cold")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize cold tier backend, cold tier disabled")
		return coldManager, nil
	}

	coldStorage, err := tiering.NewColdStorage(tiering.ColdStorageConfig{
		Name:    "cold-default",
		Type:    tiering.ColdStorageFileSystem,
		Enabled: true,
	}, coldBackend)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to initialize cold tier storage, cold tier disabled")
		return coldManager, nil
	}

	coldManager.Register(coldStorage)
	log.Info().Str("path", coldDataDir).Msg("Cold tier initialized")

	return coldManager, coldBackend
}

func (s *Server) initializeServices(cfg *config.Config) error {
	// Initialize auth service
	s.authService = auth.NewService(auth.Config{
		JWTSecret:          cfg.Auth.JWTSecret,
		TokenExpiry:        time.Duration(cfg.Auth.TokenExpiry) * time.Minute,
		RefreshTokenExpiry: time.Duration(cfg.Auth.RefreshTokenExpiry) * time.Hour,
		RootUser:           cfg.Auth.RootUser,
		RootPassword:       cfg.Auth.RootPassword,
	}, s.metaStore)

	// Initialize bucket service
	s.bucketService = bucket.NewService(s.metaStore, s.storageBackend)

	// Initialize object service
	s.objectService = object.NewService(s.metaStore, s.storageBackend, s.bucketService)

	// Initialize health checker
	s.healthChecker = health.NewChecker(s.metaStore, s.storageBackend)

	// Initialize lifecycle manager
	s.lifecycleManager = lifecycle.NewManager(
		s.metaStore,
		&lifecycleObjectService{s.objectService},
		s.objectService,
	)

	// Initialize audit logger
	auditLogPath := filepath.Join(cfg.DataDir, "audit.log")

	var err error
	s.auditLogger, err = audit.NewAuditLogger(audit.Config{
		Store:      s.metaStore,
		FilePath:   auditLogPath,
		BufferSize: defaultAuditBufferSize,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize audit logger: %w", err)
	}

	log.Info().Str("path", auditLogPath).Msg("Audit logger initialized")

	return nil
}

func (s *Server) initializeTLS(cfg *config.Config) error {
	if !cfg.TLS.Enabled {
		log.Warn().Msg("TLS disabled - running in insecure mode (NOT recommended for production)")
		return nil
	}

	hostname := cfg.NodeName
	if hostname == "" {
		hostname = "localhost"
	}

	tlsManager, err := security.NewTLSManager(&cfg.TLS, hostname)
	if err != nil {
		return fmt.Errorf("failed to initialize TLS: %w", err)
	}

	s.tlsManager = tlsManager
	log.Info().
		Str("cert_file", tlsManager.GetCertFile()).
		Str("ca_file", tlsManager.GetCAFile()).
		Bool("require_client_cert", cfg.TLS.RequireClientCert).
		Msg("TLS initialized (secure by default)")

	return nil
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
		Addr:              fmt.Sprintf(":%d", s.cfg.S3Port),
		Handler:           r,
		ReadTimeout:       defaultReadTimeoutSec * time.Second,
		WriteTimeout:      defaultWriteTimeoutSec * time.Second,
		IdleTimeout:       defaultIdleTimeoutSec * time.Second,
		ReadHeaderTimeout: defaultReadHeaderTimeoutSec * time.Second,
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

	// OpenAPI documentation endpoints (public)
	openAPIHandler := admin.NewOpenAPIHandler(admin.OpenAPISpec)
	r.Get("/api/v1/openapi.json", openAPIHandler.ServeOpenAPIJSON)
	r.Get("/api/v1/openapi.yaml", openAPIHandler.ServeOpenAPIYAML)
	r.Get("/api/v1/openapi", openAPIHandler.ServeOpenAPI)
	r.Get("/api/v1/api-explorer/config", openAPIHandler.ServeAPIExplorerConfig)
	r.Post("/api/v1/api-explorer/code-snippets", openAPIHandler.GenerateCodeSnippets)

	s.adminServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.cfg.AdminPort),
		Handler:           r,
		ReadTimeout:       defaultReadTimeoutSec * time.Second,
		WriteTimeout:      defaultWriteTimeoutSec * time.Second,
		IdleTimeout:       defaultIdleTimeoutSec * time.Second,
		ReadHeaderTimeout: defaultReadHeaderTimeoutSec * time.Second,
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
		Addr:              fmt.Sprintf(":%d", s.cfg.ConsolePort),
		Handler:           r,
		ReadTimeout:       defaultReadTimeoutSec * time.Second,
		WriteTimeout:      defaultWriteTimeoutSec * time.Second,
		IdleTimeout:       defaultIdleTimeoutSec * time.Second,
		ReadHeaderTimeout: defaultReadHeaderTimeoutSec * time.Second,
	}

	// Apply TLS configuration if enabled
	if s.tlsManager != nil {
		s.consoleServer.TLSConfig = s.tlsManager.GetTLSConfig()
	}
}

// Start starts all servers.
func (s *Server) Start(ctx context.Context) error {
	if err := s.ensureRootUser(ctx); err != nil {
		return fmt.Errorf("failed to ensure root user: %w", err)
	}

	s.startAuditLogger(ctx)

	if err := s.startClusterDiscovery(ctx); err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	s.startBackgroundWorkers(g, ctx)
	s.startHTTPServers(g)
	s.startShutdownHandler(g, ctx)

	return g.Wait()
}

func (s *Server) startAuditLogger(ctx context.Context) {
	if s.auditLogger != nil {
		s.auditLogger.Start(ctx)
		log.Info().Msg("Audit logger started")
	}
}

func (s *Server) startClusterDiscovery(ctx context.Context) error {
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
	return nil
}

func (s *Server) startBackgroundWorkers(g *errgroup.Group, ctx context.Context) {
	g.Go(func() error {
		s.runMetricsCollector(ctx)
		return nil
	})

	g.Go(func() error {
		s.lifecycleManager.Start(ctx)
		return nil
	})
}

func (s *Server) startHTTPServers(g *errgroup.Group) {
	g.Go(func() error {
		return s.startHTTPServer(s.s3Server, s.cfg.S3Port, "S3 API")
	})

	g.Go(func() error {
		log.Info().Int("port", s.cfg.AdminPort).Msg("Prometheus metrics available at /metrics")
		return s.startHTTPServer(s.adminServer, s.cfg.AdminPort, "Admin API")
	})

	g.Go(func() error {
		return s.startHTTPServer(s.consoleServer, s.cfg.ConsolePort, "Web Console")
	})
}

func (s *Server) startHTTPServer(server *http.Server, port int, name string) error {
	var err error

	if s.tlsManager != nil {
		log.Info().Int("port", port).Msgf("Starting %s server (TLS enabled)", name)
		err = server.ListenAndServeTLS(s.tlsManager.GetCertFile(), s.tlsManager.GetKeyFile())
	} else {
		log.Info().Int("port", port).Msgf("Starting %s server", name)
		err = server.ListenAndServe()
	}

	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("%s server error: %w", name, err)
	}

	return nil
}

func (s *Server) startShutdownHandler(g *errgroup.Group, ctx context.Context) {
	g.Go(func() error {
		<-ctx.Done()
		log.Info().Msg("Initiating graceful shutdown...")

		// Use WithoutCancel to create a fresh context for shutdown, derived from cancelled parent
		// This ensures shutdown completes even though parent context is cancelled
		baseCtx := context.WithoutCancel(ctx)

		// Build shutdown components
		components := s.buildShutdownComponents()

		// Execute shutdown via coordinator
		return s.shutdownCoord.Shutdown(baseCtx, components)
	})
}

// buildShutdownComponents creates the shutdown components for the coordinator.
func (s *Server) buildShutdownComponents() shutdown.ShutdownComponents {
	components := shutdown.ShutdownComponents{
		HTTPServers: []shutdown.HTTPServerShutdown{
			&httpServerWrapper{name: "S3 API", server: s.s3Server},
			&httpServerWrapper{name: "Admin API", server: s.adminServer},
			&httpServerWrapper{name: "Web Console", server: s.consoleServer},
		},
	}

	// Add discovery if available
	if s.discovery != nil {
		components.Discovery = &discoveryWrapper{discovery: s.discovery}
	}

	// Add lifecycle manager if available
	if s.lifecycleManager != nil {
		components.LifecycleManager = s.lifecycleManager
	}

	// Add audit logger if available
	if s.auditLogger != nil {
		components.AuditLogger = s.auditLogger
	}

	// Add metadata store if available
	if s.metaStore != nil {
		components.MetadataStore = &metadataStoreWrapper{store: s.metaStore}
	}

	// Add storage backend if it supports Close
	if s.storageBackend != nil {
		if closer, ok := s.storageBackend.(io.Closer); ok {
			components.StorageBackend = closer
		} else {
			log.Warn().
				Str("backend_type", fmt.Sprintf("%T", s.storageBackend)).
				Msg("Storage backend does not implement io.Closer, will not be closed during shutdown")
		}
	}

	// Add tiering service if available
	if s.tieringService != nil {
		components.TieringService = &tieringServiceWrapper{service: s.tieringService}
	}

	return components
}

// httpServerWrapper wraps an http.Server for shutdown coordination.
type httpServerWrapper struct {
	name   string
	server *http.Server
}

func (w *httpServerWrapper) Name() string {
	return w.name
}

func (w *httpServerWrapper) Shutdown(ctx context.Context) error {
	return w.server.Shutdown(ctx)
}

// discoveryWrapper wraps cluster.Discovery for shutdown coordination.
type discoveryWrapper struct {
	discovery *cluster.Discovery
}

func (w *discoveryWrapper) Name() string {
	return "cluster_discovery"
}

func (w *discoveryWrapper) Stop() error {
	return w.discovery.Stop()
}

// metadataStoreWrapper wraps metadata.DragonboatStore for shutdown coordination.
type metadataStoreWrapper struct {
	store *metadata.DragonboatStore
}

func (w *metadataStoreWrapper) Name() string {
	return "metadata_store"
}

func (w *metadataStoreWrapper) Close() error {
	return w.store.Close()
}

// tieringServiceWrapper wraps tiering.AdvancedService for shutdown coordination.
type tieringServiceWrapper struct {
	service *tiering.AdvancedService
}

func (w *tieringServiceWrapper) Name() string {
	return "tiering_service"
}

func (w *tieringServiceWrapper) Stop() error {
	return w.service.Stop()
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
	clusterInfo, clusterErr := s.metaStore.GetClusterInfo(ctx)
	if clusterErr == nil {
		metrics.SetClusterNodesTotal(len(clusterInfo.Nodes))
	}

	// Update storage metrics
	storageInfo, storageErr := s.storageBackend.GetStorageInfo(ctx)
	if storageErr == nil {
		metrics.SetStorageStats(storageInfo.UsedBytes, storageInfo.TotalBytes)
	}

	// Update bucket count
	buckets, bucketsErr := s.metaStore.ListBuckets(ctx, "")
	if bucketsErr == nil {
		metrics.SetBucketsTotal(len(buckets))
	}

	// Update multipart uploads count (approximate - count for all buckets)
	var multipartCount int

	multipartBuckets, multipartErr := s.metaStore.ListBuckets(ctx, "")
	if multipartErr == nil {
		for _, bucket := range multipartBuckets {
			uploads, uploadsErr := s.metaStore.ListMultipartUploads(ctx, bucket.Name)
			if uploadsErr == nil {
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
