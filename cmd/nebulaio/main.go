package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/piwi3910/nebulaio/internal/config"
	"github.com/piwi3910/nebulaio/internal/server"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Default port configurations.
const (
	defaultS3Port      = 9000
	defaultAdminPort   = 9001
	defaultConsolePort = 9002
)

var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	dataDir := flag.String("data", "./data", "Data directory path")
	s3Port := flag.Int("s3-port", defaultS3Port, "S3 API port")
	adminPort := flag.Int("admin-port", defaultAdminPort, "Admin/Console API port")
	consolePort := flag.Int("console-port", defaultConsolePort, "Web console port")
	debug := flag.Bool("debug", false, "Enable debug logging")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("NebulaIO %s\n", version)
		fmt.Printf("  Commit: %s\n", commit)
		fmt.Printf("  Built:  %s\n", buildDate)
		os.Exit(0)
	}

	// Configure logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	log.Info().
		Str("version", version).
		Str("commit", commit).
		Msg("Starting NebulaIO")

	// Load configuration
	cfg, err := config.Load(*configPath, config.Options{
		DataDir:     *dataDir,
		S3Port:      *s3Port,
		AdminPort:   *adminPort,
		ConsolePort: *consolePort,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Create and start server
	srv, err := server.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create server")
	}

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
		cancel()
	}()

	// Start server
	if err := srv.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server error")
	}

	log.Info().Msg("NebulaIO shutdown complete")
}
