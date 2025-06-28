package main

import (
    "flag"
    "log"

    "github.com/sirupsen/logrus"
    "facebook-scraper/internal/api"
    "facebook-scraper/internal/config"
    "facebook-scraper/internal/database"
)

func main() {
    var (
        configFile = flag.String("config", "configs/config.yaml", "Configuration file path")
        port       = flag.String("port", "8080", "API server port")
    )
    flag.Parse()

    // Load configuration
    cfg, err := config.Load(*configFile)
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }

    // Setup logger
    logger := logrus.New()
    if cfg.Logging.Level == "debug" {
        logger.SetLevel(logrus.DebugLevel)
    } else {
        logger.SetLevel(logrus.InfoLevel)
    }

    // Initialize database
    db, err := database.NewConnection(&cfg.Database, logger)
    if err != nil {
        logger.Fatalf("Failed to connect to database: %v", err)
    }
    defer db.Close()

    // Create API server
    server := api.NewServer(db, logger, *port)

    logger.Infof("Starting Facebook Scraper API server on port %s", *port)
    logger.Info("Available endpoints:")
    logger.Info("  GET  /api/posts - List posts with pagination")
    logger.Info("  GET  /api/posts/group/{id} - Get posts by group")
    logger.Info("  GET  /api/stats - Get scraping statistics")
    logger.Info("  GET  /api/export/csv - Export posts to CSV")
    logger.Info("  GET  /api/health - Health check")
    logger.Info("  GET  /dashboard - Web dashboard")

    if err := server.Start(); err != nil {
        logger.Fatalf("Failed to start server: %v", err)
    }
}