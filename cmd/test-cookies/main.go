package main

import (
    "fmt"
    "log"
    
    "facebook-scraper/internal/config"
    "facebook-scraper/internal/scraper"
    "github.com/sirupsen/logrus"
)

func main() {
    logger := logrus.New()
    logger.SetLevel(logrus.DebugLevel)
    
    cfg, err := config.Load("configs/config.yaml")
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }
    
    authManager, err := scraper.NewAuthManager(
        cfg.Facebook.Auth.CookiesFile,
        cfg.Facebook.Auth.UserAgent,
        logger,
    )
    if err != nil {
        log.Fatalf("Failed to create auth manager: %v", err)
    }
    
    fmt.Println("Testing cookie loading...")
    if err := authManager.LoadCookies(); err != nil {
        log.Fatalf("Failed to load cookies: %v", err)
    }
    
    fmt.Println("Testing authentication...")
    if err := authManager.ValidateAuth(); err != nil {
        log.Fatalf("Authentication failed: %v", err)
    }
    
    fmt.Println("✅ Cookies are valid and authentication successful!")
}