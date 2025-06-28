package main

import (
    "flag"
    "fmt"
    "log"
    "time"

    "github.com/sirupsen/logrus"
    "facebook-scraper/internal/config"
    "facebook-scraper/internal/database"
    "facebook-scraper/internal/monitoring"
)

func main() {
    var (
        configFile  = flag.String("config", "configs/config.yaml", "Configuration file path")
        metricsFile = flag.String("metrics", "data/metrics.json", "Metrics file path")
        report      = flag.Bool("report", false, "Generate and display monitoring report")
        alerts      = flag.Bool("alerts", false, "Check and display alerts")
    )
    flag.Parse()

    // Setup logger
    logger := logrus.New()
    logger.SetLevel(logrus.InfoLevel)

    // Load configuration
    cfg, err := config.Load(*configFile)
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }

    // Initialize database
    db, err := database.NewConnection(&cfg.Database, logger)
    if err != nil {
        logger.Fatalf("Failed to connect to database: %v", err)
    }
    defer db.Close()

    // Initialize monitor
    monitor := monitoring.NewMonitor(logger, *metricsFile)

    if *report {
        // Generate and display report
        fmt.Println(monitor.GenerateReport())
        
        // Also show database stats
        stats, err := db.GetScrapingStats()
        if err != nil {
            logger.Errorf("Failed to get database stats: %v", err)
        } else {
            fmt.Println("\nDatabase Statistics:")
            fmt.Printf("- Total Posts: %v\n", stats["total_posts"])
            fmt.Printf("- High Engagement Posts: %v\n", stats["high_engagement_posts"])
            fmt.Printf("- Average Likes: %.2f\n", stats["average_likes"])
            fmt.Printf("- Groups Scraped: %v\n", stats["groups_scraped"])
            fmt.Printf("- Last Scraped: %v\n", stats["last_scraped_at"])
        }
        return
    }

    if *alerts {
        // Check and display alerts
        alertManager := monitoring.NewAlertManager(monitor, logger)
        alerts := alertManager.CheckAlerts()
        
        if len(alerts) == 0 {
            fmt.Println("✅ No alerts - system is healthy")
        } else {
            fmt.Println("⚠️  Active Alerts:")
            for _, alert := range alerts {
                fmt.Printf("  - %s\n", alert)
            }
        }
        return
    }

    // Default: show current status
    health := monitor.GetHealthStatus()
    fmt.Println("Facebook Scraper Status:")
    fmt.Printf("- Status: %s\n", health["status"])
    fmt.Printf("- Last Run: %s\n", health["last_run"])
    fmt.Printf("- Total Runs: %v\n", health["total_runs"])
    fmt.Printf("- Error Rate: %s\n", health["error_rate"])
    fmt.Printf("- Average Runtime: %s\n", health["average_runtime"])

    if warning, exists := health["warning"]; exists {
        fmt.Printf("- Warning: %s\n", warning)
    }
}