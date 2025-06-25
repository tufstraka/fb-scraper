package config

import (
    "fmt"
    "io/ioutil"
    "os"

    "gopkg.in/yaml.v2"
    "github.com/joho/godotenv"
)

type Config struct {
    Facebook FacebookConfig `yaml:"facebook"`
    Scraper  ScraperConfig  `yaml:"scraper"`
    Database DatabaseConfig `yaml:"database"`
    Logging  LoggingConfig  `yaml:"logging"`
}

type FacebookConfig struct {
    BaseURL   string        `yaml:"base_url"`
    MobileURL string        `yaml:"mobile_url"`
    Timeout   int           `yaml:"timeout"`
    RateLimit RateLimitConfig `yaml:"rate_limit"`
    Auth      AuthConfig    `yaml:"auth"`
}

type AuthConfig struct {
    Method      string `yaml:"method"`
    CookiesFile string `yaml:"cookies_file"`
    UserAgent   string `yaml:"user_agent"`
}

type RateLimitConfig struct {
    RequestsPerMinute    int `yaml:"requests_per_minute"`
    DelayBetweenRequests int `yaml:"delay_between_requests"`
}

type ScraperConfig struct {
    ConcurrentWorkers int    `yaml:"concurrent_workers"`
    RetryAttempts     int    `yaml:"retry_attempts"`
    RetryDelay        int    `yaml:"retry_delay"`
    OutputFormat      string `yaml:"output_format"`
}

type DatabaseConfig struct {
    Host     string `yaml:"host"`
    Port     int    `yaml:"port"`
    Name     string `yaml:"name"`
    User     string `yaml:"user"`
    Password string `yaml:"password"`
    SSLMode  string `yaml:"ssl_mode"`
}

type LoggingConfig struct {
    Level      string `yaml:"level"`
    File       string `yaml:"file"`
    MaxSize    int    `yaml:"max_size"`
    MaxBackups int    `yaml:"max_backups"`
    MaxAge     int    `yaml:"max_age"`
}

type Group struct {
    ID   string `yaml:"id"`
    Name string `yaml:"name"`
}

func Load(configFile string) (*Config, error) {
    // Load environment variables
    if err := godotenv.Load(); err != nil {
        // .env file is optional, so don't fail if it doesn't exist
    }

    // Check if config file exists
    if _, err := os.Stat(configFile); os.IsNotExist(err) {
        return nil, fmt.Errorf("config file not found: %s", configFile)
    }

    // Read config file
    data, err := ioutil.ReadFile(configFile)
    if err != nil {
        return nil, fmt.Errorf("failed to read config file: %w", err)
    }

    var config Config
    if err := yaml.Unmarshal(data, &config); err != nil {
        return nil, fmt.Errorf("failed to parse config file: %w", err)
    }

    // Override with environment variables if they exist
    if dbHost := os.Getenv("DB_HOST"); dbHost != "" {
        config.Database.Host = dbHost
    }
    if dbUser := os.Getenv("DB_USER"); dbUser != "" {
        config.Database.User = dbUser
    }
    if dbPassword := os.Getenv("DB_PASSWORD"); dbPassword != "" {
        config.Database.Password = dbPassword
    }
    if dbName := os.Getenv("DB_NAME"); dbName != "" {
        config.Database.Name = dbName
    }

    return &config, nil
}

func LoadGroups(groupsFile string) ([]Group, error) {
    if _, err := os.Stat(groupsFile); os.IsNotExist(err) {
        return nil, fmt.Errorf("groups file not found: %s", groupsFile)
    }

    data, err := ioutil.ReadFile(groupsFile)
    if err != nil {
        return nil, fmt.Errorf("failed to read groups file: %w", err)
    }

    var groups struct {
        Groups []Group `yaml:"groups"`
    }

    if err := yaml.Unmarshal(data, &groups); err != nil {
        return nil, fmt.Errorf("failed to parse groups file: %w", err)
    }

    return groups.Groups, nil
}