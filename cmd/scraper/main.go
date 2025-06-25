package main

import (
    "flag"
    "log"
    "os"
    "time"

    "github.com/sirupsen/logrus"
    "facebook-scraper/internal/config"
    "facebook-scraper/internal/database"
    "facebook-scraper/internal/scraper"
)

func main() {
    var (
        configFile = flag.String("config", "configs/config.yaml", "Configuration file path")
        extractCmd = flag.Bool("extract-cookies", false, "Show instructions for extracting cookies")
    )
    flag.Parse()

    if *extractCmd {
        scraper.ExtractCookiesFromBrowser()
        return
    }

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
    
    // Create logs directory if it doesn't exist
    if err := os.MkdirAll("logs", 0755); err != nil {
        log.Fatalf("Failed to create logs directory: %v", err)
    }

    // Create log file
    if cfg.Logging.File != "" {
        file, err := os.OpenFile(cfg.Logging.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
        if err != nil {
            log.Fatalf("Failed to open log file: %v", err)
        }
        defer file.Close()
        logger.SetOutput(file)
    }

    // Initialize database
    db, err := database.NewConnection(&cfg.Database, logger)
    if err != nil {
        logger.Fatalf("Failed to connect to database: %v", err)
    }
    defer db.Close()

    // Run migrations
    if err := db.RunMigrations(); err != nil {
        logger.Fatalf("Failed to run migrations: %v", err)
    }

    // Initialize scraper with database
    fbScraper, err := scraper.NewFacebookScraper(
        cfg.Facebook.Auth.CookiesFile,
        cfg.Facebook.Auth.UserAgent,
        time.Duration(cfg.Facebook.RateLimit.DelayBetweenRequests)*time.Second,
        logger,
        db,
    )
    if err != nil {
        logger.Fatalf("Failed to create Facebook scraper: %v", err)
    }

    // Initialize the scraper (loads cookies and validates auth)
    if err := fbScraper.Initialize(); err != nil {
        logger.Fatalf("Failed to initialize scraper: %v", err)
    }
    defer fbScraper.Close()

    logger.Info("Scraper started successfully - filtering for posts with 1000+ likes in past 5 days")

    // Load groups to scrape
    groups, err := config.LoadGroups("configs/groups.yaml")
    if err != nil {
        logger.Fatalf("Failed to load groups: %v", err)
    }

    totalPosts := 0
    // Scrape each group
    for _, group := range groups {
        logger.Infof("Scraping group: %s (%s)", group.Name, group.ID)
        if err := fbScraper.ScrapeGroup(group.ID); err != nil {
            logger.Errorf("Failed to scrape group %s: %v", group.ID, err)
            continue
        }
        logger.Infof("Successfully scraped group: %s", group.Name)
        
        // Add delay between groups to respect rate limits
        time.Sleep(time.Duration(cfg.Facebook.RateLimit.DelayBetweenRequests) * time.Second)
    }

    logger.Infof("Scraping completed! Total posts meeting criteria (1000+ likes, past 5 days): %d", totalPosts)
    logger.Info("Data saved to PostgreSQL database. Use PgAdmin or connect directly to view results.")
}