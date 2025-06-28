package database

import (
    "database/sql"
    "fmt"
    "facebook-scraper/internal/database/models"
)

// GetPostsWithPagination retrieves posts with pagination support
func (db *DB) GetPostsWithPagination(page, pageSize, minLikes int) ([]*models.Post, error) {
    offset := (page - 1) * pageSize
    
    query := `
        SELECT id, group_id, group_name, post_id, author_id, author_name, content,
               post_url, timestamp, likes, comments, shares, images, videos,
               links, hashtags, mentions, post_type, scraped_at, created_at, updated_at,
               media_count
        FROM posts 
        WHERE likes >= $1 
            AND scraped_at >= NOW() - INTERVAL '5 days'
        ORDER BY likes DESC, scraped_at DESC 
        LIMIT $2 OFFSET $3`

    rows, err := db.conn.Query(query, minLikes, pageSize, offset)
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
            &post.ScrapedAt, &post.CreatedAt, &post.UpdatedAt, &post.MediaCount,
        )
        if err != nil {
            return nil, fmt.Errorf("failed to scan post: %w", err)
        }
        posts = append(posts, post)
    }

    return posts, nil
}

// GetPostsCount returns the total count of posts matching criteria
func (db *DB) GetPostsCount(minLikes int) (int, error) {
    query := `
        SELECT COUNT(*) 
        FROM posts 
        WHERE likes >= $1 
            AND scraped_at >= NOW() - INTERVAL '5 days'`

    var count int
    err := db.conn.QueryRow(query, minLikes).Scan(&count)
    if err != nil {
        return 0, fmt.Errorf("failed to get posts count: %w", err)
    }

    return count, nil
}

// GetPostsForExport retrieves posts for CSV export
func (db *DB) GetPostsForExport(minLikes int) ([]*models.Post, error) {
    query := `
        SELECT id, group_id, group_name, post_id, author_id, author_name, content,
               post_url, timestamp, likes, comments, shares, post_type, scraped_at
        FROM posts 
        WHERE likes >= $1 
            AND scraped_at >= NOW() - INTERVAL '5 days'
        ORDER BY likes DESC`

    rows, err := db.conn.Query(query, minLikes)
    if err != nil {
        return nil, fmt.Errorf("failed to query posts for export: %w", err)
    }
    defer rows.Close()

    var posts []*models.Post
    for rows.Next() {
        post := &models.Post{}
        err := rows.Scan(
            &post.ID, &post.GroupID, &post.GroupName, &post.PostID, &post.AuthorID,
            &post.AuthorName, &post.Content, &post.PostURL, &post.Timestamp,
            &post.Likes, &post.Comments, &post.Shares, &post.PostType, &post.ScrapedAt,
        )
        if err != nil {
            return nil, fmt.Errorf("failed to scan post: %w", err)
        }
        posts = append(posts, post)
    }

    return posts, nil
}

// GetScrapingStats returns comprehensive scraping statistics
func (db *DB) GetScrapingStats() (map[string]interface{}, error) {
    stats := make(map[string]interface{})

    // Total posts
    var totalPosts int
    err := db.conn.QueryRow("SELECT COUNT(*) FROM posts").Scan(&totalPosts)
    if err != nil {
        return nil, fmt.Errorf("failed to get total posts: %w", err)
    }
    stats["total_posts"] = totalPosts

    // High engagement posts (1000+ likes in past 5 days)
    var highEngagementPosts int
    err = db.conn.QueryRow(`
        SELECT COUNT(*) FROM posts 
        WHERE likes >= 1000 AND scraped_at >= NOW() - INTERVAL '5 days'
    `).Scan(&highEngagementPosts)
    if err != nil {
        return nil, fmt.Errorf("failed to get high engagement posts: %w", err)
    }
    stats["high_engagement_posts"] = highEngagementPosts

    // Average likes
    var avgLikes sql.NullFloat64
    err = db.conn.QueryRow(`
        SELECT AVG(likes) FROM posts 
        WHERE scraped_at >= NOW() - INTERVAL '5 days'
    `).Scan(&avgLikes)
    if err != nil {
        return nil, fmt.Errorf("failed to get average likes: %w", err)
    }
    if avgLikes.Valid {
        stats["average_likes"] = avgLikes.Float64
    } else {
        stats["average_likes"] = 0
    }

    // Top group by post count
    var topGroup sql.NullString
    err = db.conn.QueryRow(`
        SELECT group_name FROM posts 
        WHERE scraped_at >= NOW() - INTERVAL '5 days'
        GROUP BY group_name 
        ORDER BY COUNT(*) DESC 
        LIMIT 1
    `).Scan(&topGroup)
    if err != nil && err != sql.ErrNoRows {
        return nil, fmt.Errorf("failed to get top group: %w", err)
    }
    if topGroup.Valid {
        stats["top_group"] = topGroup.String
    } else {
        stats["top_group"] = "None"
    }

    // Last scraped timestamp
    var lastScraped sql.NullString
    err = db.conn.QueryRow(`
        SELECT MAX(scraped_at)::text FROM posts
    `).Scan(&lastScraped)
    if err != nil {
        return nil, fmt.Errorf("failed to get last scraped time: %w", err)
    }
    if lastScraped.Valid {
        stats["last_scraped_at"] = lastScraped.String
    } else {
        stats["last_scraped_at"] = "Never"
    }

    // Number of groups scraped
    var groupsScraped int
    err = db.conn.QueryRow(`
        SELECT COUNT(DISTINCT group_id) FROM posts 
        WHERE scraped_at >= NOW() - INTERVAL '5 days'
    `).Scan(&groupsScraped)
    if err != nil {
        return nil, fmt.Errorf("failed to get groups scraped: %w", err)
    }
    stats["groups_scraped"] = groupsScraped

    // Posts by type
    rows, err := db.conn.Query(`
        SELECT post_type, COUNT(*) FROM posts 
        WHERE scraped_at >= NOW() - INTERVAL '5 days'
        GROUP BY post_type
    `)
    if err != nil {
        return nil, fmt.Errorf("failed to get posts by type: %w", err)
    }
    defer rows.Close()

    postsByType := make(map[string]int)
    for rows.Next() {
        var postType string
        var count int
        if err := rows.Scan(&postType, &count); err != nil {
            continue
        }
        postsByType[postType] = count
    }
    stats["posts_by_type"] = postsByType

    return stats, nil
}

// Ping checks if the database connection is alive
func (db *DB) Ping() error {
    return db.conn.Ping()
}

// GetTopAuthors returns authors with most high-engagement posts
func (db *DB) GetTopAuthors(limit int) ([]map[string]interface{}, error) {
    query := `
        SELECT author_name, COUNT(*) as post_count, AVG(likes) as avg_likes
        FROM posts 
        WHERE likes >= 1000 AND scraped_at >= NOW() - INTERVAL '5 days'
        GROUP BY author_name 
        ORDER BY post_count DESC, avg_likes DESC 
        LIMIT $1`

    rows, err := db.conn.Query(query, limit)
    if err != nil {
        return nil, fmt.Errorf("failed to query top authors: %w", err)
    }
    defer rows.Close()

    var authors []map[string]interface{}
    for rows.Next() {
        var authorName string
        var postCount int
        var avgLikes float64

        err := rows.Scan(&authorName, &postCount, &avgLikes)
        if err != nil {
            return nil, fmt.Errorf("failed to scan author: %w", err)
        }

        authors = append(authors, map[string]interface{}{
            "author_name": authorName,
            "post_count":  postCount,
            "avg_likes":   avgLikes,
        })
    }

    return authors, nil
}

// GetEngagementTrends returns engagement trends over time
func (db *DB) GetEngagementTrends() ([]map[string]interface{}, error) {
    query := `
        SELECT 
            DATE(scraped_at) as date,
            COUNT(*) as posts_count,
            AVG(likes) as avg_likes,
            MAX(likes) as max_likes
        FROM posts 
        WHERE scraped_at >= NOW() - INTERVAL '30 days'
        GROUP BY DATE(scraped_at)
        ORDER BY date DESC`

    rows, err := db.conn.Query(query)
    if err != nil {
        return nil, fmt.Errorf("failed to query engagement trends: %w", err)
    }
    defer rows.Close()

    var trends []map[string]interface{}
    for rows.Next() {
        var date string
        var postsCount int
        var avgLikes, maxLikes float64

        err := rows.Scan(&date, &postsCount, &avgLikes, &maxLikes)
        if err != nil {
            return nil, fmt.Errorf("failed to scan trend: %w", err)
        }

        trends = append(trends, map[string]interface{}{
            "date":        date,
            "posts_count": postsCount,
            "avg_likes":   avgLikes,
            "max_likes":   maxLikes,
        })
    }

    return trends, nil
}