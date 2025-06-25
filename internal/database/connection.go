package database

import (
    "database/sql"
    "fmt"
    "io/ioutil"
    "os"
    "path/filepath"
    "sort"

    "facebook-scraper/internal/config"
    "facebook-scraper/internal/database/models"
    
    _ "github.com/lib/pq"
    "github.com/sirupsen/logrus"
)

type DB struct {
    conn   *sql.DB
    logger *logrus.Logger
}

func NewConnection(cfg *config.DatabaseConfig, logger *logrus.Logger) (*DB, error) {
    // Override config with environment variables if they exist
    host := getEnvOrDefault("DB_HOST", cfg.Host)
    port := getEnvOrDefault("DB_PORT", fmt.Sprintf("%d", cfg.Port))
    user := getEnvOrDefault("DB_USER", cfg.User)
    password := getEnvOrDefault("DB_PASSWORD", cfg.Password)
    dbname := getEnvOrDefault("DB_NAME", cfg.Name)
    sslmode := getEnvOrDefault("DB_SSL_MODE", cfg.SSLMode)

    connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
        host, port, user, password, dbname, sslmode)

    logger.Infof("Connecting to database: host=%s port=%s dbname=%s user=%s", host, port, dbname, user)

    conn, err := sql.Open("postgres", connStr)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to database: %w", err)
    }

    if err := conn.Ping(); err != nil {
        return nil, fmt.Errorf("failed to ping database: %w", err)
    }

    db := &DB{
        conn:   conn,
        logger: logger,
    }

    logger.Info("Database connection established")
    return db, nil
}

func getEnvOrDefault(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}

func (db *DB) RunMigrations() error {
    db.logger.Info("Running database migrations...")

    migrationFiles, err := filepath.Glob("internal/database/migrations/*.sql")
    if err != nil {
        return fmt.Errorf("failed to find migration files: %w", err)
    }

    sort.Strings(migrationFiles)

    for _, file := range migrationFiles {
        db.logger.Infof("Running migration: %s", file)
        
        content, err := ioutil.ReadFile(file)
        if err != nil {
            return fmt.Errorf("failed to read migration file %s: %w", file, err)
        }

        if _, err := db.conn.Exec(string(content)); err != nil {
            return fmt.Errorf("failed to execute migration %s: %w", file, err)
        }
    }

    db.logger.Info("Migrations completed successfully")
    return nil
}

func (db *DB) SavePost(post *models.Post) error {
    query := `
        INSERT INTO posts (
            group_id, group_name, post_id, author_id, author_name, content, 
            post_url, timestamp, likes, comments, shares, images, videos, 
            links, hashtags, mentions, post_type
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
        ) ON CONFLICT (post_id) DO UPDATE SET
            content = EXCLUDED.content,
            likes = EXCLUDED.likes,
            comments = EXCLUDED.comments,
            shares = EXCLUDED.shares,
            updated_at = CURRENT_TIMESTAMP
        RETURNING id`

    err := db.conn.QueryRow(
        query,
        post.GroupID, post.GroupName, post.PostID, post.AuthorID, post.AuthorName,
        post.Content, post.PostURL, post.Timestamp, post.Likes, post.Comments,
        post.Shares, post.Images, post.Videos, post.Links, post.Hashtags,
        post.Mentions, post.PostType,
    ).Scan(&post.ID)

    if err != nil {
        return fmt.Errorf("failed to save post: %w", err)
    }

    return nil
}

func (db *DB) GetPostsByGroup(groupID string, limit int) ([]*models.Post, error) {
    query := `
        SELECT id, group_id, group_name, post_id, author_id, author_name, content,
               post_url, timestamp, likes, comments, shares, images, videos,
               links, hashtags, mentions, post_type, scraped_at, created_at, updated_at
        FROM posts 
        WHERE group_id = $1 
        ORDER BY timestamp DESC 
        LIMIT $2`

    rows, err := db.conn.Query(query, groupID, limit)
    if err != nil {
        return nil, fmt.Errorf("failed to query posts: %w", err)
    }
    defer rows.Close()

    var posts []*models.Post
    for rows.Next() {
        post := &models.Post{}
        err := rows.Scan(
            &post.ID, &post.GroupID, &post.GroupName, &post.PostID, &post.AuthorID,
            &post.AuthorName, &post.Content, &post.PostURL, &post.Timestamp,
            &post.Likes, &post.Comments, &post.Shares, &post.Images, &post.Videos,
            &post.Links, &post.Hashtags, &post.Mentions, &post.PostType,
            &post.ScrapedAt, &post.CreatedAt, &post.UpdatedAt,
        )
        if err != nil {
            return nil, fmt.Errorf("failed to scan post: %w", err)
        }
        posts = append(posts, post)
    }

    return posts, nil
}

func (db *DB) Close() error {
    return db.conn.Close()
}