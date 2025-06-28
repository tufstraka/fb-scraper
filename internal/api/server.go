package api

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strconv"
    "time"

    "github.com/sirupsen/logrus"
    "facebook-scraper/internal/database"
    "facebook-scraper/internal/database/models"
)

type Server struct {
    db     *database.DB
    logger *logrus.Logger
    port   string
}

type APIResponse struct {
    Success bool        `json:"success"`
    Data    interface{} `json:"data,omitempty"`
    Error   string      `json:"error,omitempty"`
    Count   int         `json:"count,omitempty"`
}

type PostsResponse struct {
    Posts      []*models.Post `json:"posts"`
    TotalCount int            `json:"total_count"`
    Page       int            `json:"page"`
    PageSize   int            `json:"page_size"`
}

type StatsResponse struct {
    TotalPosts       int     `json:"total_posts"`
    HighEngagement   int     `json:"high_engagement_posts"`
    AverageLikes     float64 `json:"average_likes"`
    TopGroup         string  `json:"top_group"`
    LastScrapedAt    string  `json:"last_scraped_at"`
    GroupsScraped    int     `json:"groups_scraped"`
}

func NewServer(db *database.DB, logger *logrus.Logger, port string) *Server {
    return &Server{
        db:     db,
        logger: logger,
        port:   port,
    }
}

func (s *Server) Start() error {
    s.setupRoutes()
    s.logger.Infof("Starting API server on port %s", s.port)
    return http.ListenAndServe(":"+s.port, nil)
}

func (s *Server) setupRoutes() {
    // Enable CORS
    http.HandleFunc("/", s.corsMiddleware(s.handleRoot))
    http.HandleFunc("/api/posts", s.corsMiddleware(s.handlePosts))
    http.HandleFunc("/api/posts/group/", s.corsMiddleware(s.handlePostsByGroup))
    http.HandleFunc("/api/stats", s.corsMiddleware(s.handleStats))
    http.HandleFunc("/api/export/csv", s.corsMiddleware(s.handleExportCSV))
    http.HandleFunc("/api/health", s.corsMiddleware(s.handleHealth))
    
    // Serve static files for web dashboard
    http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static/"))))
    http.HandleFunc("/dashboard", s.corsMiddleware(s.handleDashboard))
}

func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
        
        if r.Method == "OPTIONS" {
            w.WriteHeader(http.StatusOK)
            return
        }
        
        next(w, r)
    }
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
    response := APIResponse{
        Success: true,
        Data: map[string]string{
            "message": "Facebook Scraper API",
            "version": "1.0.0",
            "endpoints": "/api/posts, /api/stats, /api/export/csv, /dashboard",
        },
    }
    s.writeJSON(w, response)
}

func (s *Server) handlePosts(w http.ResponseWriter, r *http.Request) {
    // Parse query parameters
    page, _ := strconv.Atoi(r.URL.Query().Get("page"))
    if page < 1 {
        page = 1
    }
    
    pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
    if pageSize < 1 || pageSize > 100 {
        pageSize = 20
    }
    
    minLikes, _ := strconv.Atoi(r.URL.Query().Get("min_likes"))
    if minLikes < 1 {
        minLikes = 1000
    }

    posts, err := s.db.GetPostsWithPagination(page, pageSize, minLikes)
    if err != nil {
        s.writeError(w, fmt.Sprintf("Failed to fetch posts: %v", err), http.StatusInternalServerError)
        return
    }

    totalCount, err := s.db.GetPostsCount(minLikes)
    if err != nil {
        s.writeError(w, fmt.Sprintf("Failed to get total count: %v", err), http.StatusInternalServerError)
        return
    }

    response := APIResponse{
        Success: true,
        Data: PostsResponse{
            Posts:      posts,
            TotalCount: totalCount,
            Page:       page,
            PageSize:   pageSize,
        },
        Count: len(posts),
    }

    s.writeJSON(w, response)
}

func (s *Server) handlePostsByGroup(w http.ResponseWriter, r *http.Request) {
    groupID := r.URL.Path[len("/api/posts/group/"):]
    if groupID == "" {
        s.writeError(w, "Group ID is required", http.StatusBadRequest)
        return
    }

    limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
    if limit < 1 || limit > 100 {
        limit = 50
    }

    posts, err := s.db.GetPostsByGroup(groupID, limit)
    if err != nil {
        s.writeError(w, fmt.Sprintf("Failed to fetch posts for group: %v", err), http.StatusInternalServerError)
        return
    }

    response := APIResponse{
        Success: true,
        Data:    posts,
        Count:   len(posts),
    }

    s.writeJSON(w, response)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
    stats, err := s.db.GetScrapingStats()
    if err != nil {
        s.writeError(w, fmt.Sprintf("Failed to fetch stats: %v", err), http.StatusInternalServerError)
        return
    }

    response := APIResponse{
        Success: true,
        Data:    stats,
    }

    s.writeJSON(w, response)
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
    minLikes, _ := strconv.Atoi(r.URL.Query().Get("min_likes"))
    if minLikes < 1 {
        minLikes = 1000
    }

    posts, err := s.db.GetPostsForExport(minLikes)
    if err != nil {
        s.writeError(w, fmt.Sprintf("Failed to fetch posts for export: %v", err), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "text/csv")
    w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=facebook_posts_%s.csv", time.Now().Format("2006-01-02")))

    // Write CSV header
    w.Write([]byte("Group Name,Author,Content,Likes,Comments,Shares,Post Type,Timestamp,URL\n"))

    // Write CSV data
    for _, post := range posts {
        content := fmt.Sprintf("\"%s\"", strings.ReplaceAll(post.Content, "\"", "\"\""))
        line := fmt.Sprintf("%s,%s,%s,%d,%d,%d,%s,%s,%s\n",
            post.GroupName,
            post.AuthorName,
            content,
            post.Likes,
            post.Comments,
            post.Shares,
            post.PostType,
            post.Timestamp.Format("2006-01-02 15:04:05"),
            post.PostURL,
        )
        w.Write([]byte(line))
    }
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
    // Check database connection
    if err := s.db.Ping(); err != nil {
        s.writeError(w, "Database connection failed", http.StatusServiceUnavailable)
        return
    }

    response := APIResponse{
        Success: true,
        Data: map[string]interface{}{
            "status":    "healthy",
            "timestamp": time.Now().Format(time.RFC3339),
            "database":  "connected",
        },
    }

    s.writeJSON(w, response)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
    html := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Facebook Scraper Dashboard</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        .header { background: white; padding: 20px; border-radius: 8px; margin-bottom: 20px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 20px; margin-bottom: 20px; }
        .stat-card { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .stat-number { font-size: 2em; font-weight: bold; color: #1877f2; }
        .stat-label { color: #666; margin-top: 5px; }
        .posts-section { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .post-item { border-bottom: 1px solid #eee; padding: 15px 0; }
        .post-author { font-weight: bold; color: #1877f2; }
        .post-content { margin: 10px 0; color: #333; }
        .post-stats { display: flex; gap: 20px; color: #666; font-size: 0.9em; }
        .loading { text-align: center; padding: 40px; color: #666; }
        .error { background: #fee; color: #c33; padding: 15px; border-radius: 4px; margin: 10px 0; }
        .controls { margin-bottom: 20px; }
        .btn { background: #1877f2; color: white; border: none; padding: 10px 20px; border-radius: 4px; cursor: pointer; }
        .btn:hover { background: #166fe5; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Facebook Scraper Dashboard</h1>
            <p>Monitor your Facebook scraping results and analytics</p>
        </div>

        <div class="stats-grid" id="stats-grid">
            <div class="loading">Loading statistics...</div>
        </div>

        <div class="controls">
            <button class="btn" onclick="refreshData()">Refresh Data</button>
            <button class="btn" onclick="exportCSV()">Export CSV</button>
        </div>

        <div class="posts-section">
            <h2>Recent High-Engagement Posts</h2>
            <div id="posts-container">
                <div class="loading">Loading posts...</div>
            </div>
        </div>
    </div>

    <script>
        async function loadStats() {
            try {
                const response = await fetch('/api/stats');
                const data = await response.json();
                
                if (data.success) {
                    const statsGrid = document.getElementById('stats-grid');
                    statsGrid.innerHTML = ` + "`" + `
                        <div class="stat-card">
                            <div class="stat-number">${data.data.total_posts || 0}</div>
                            <div class="stat-label">Total Posts</div>
                        </div>
                        <div class="stat-card">
                            <div class="stat-number">${data.data.high_engagement_posts || 0}</div>
                            <div class="stat-label">High Engagement Posts</div>
                        </div>
                        <div class="stat-card">
                            <div class="stat-number">${Math.round(data.data.average_likes || 0)}</div>
                            <div class="stat-label">Average Likes</div>
                        </div>
                        <div class="stat-card">
                            <div class="stat-number">${data.data.groups_scraped || 0}</div>
                            <div class="stat-label">Groups Scraped</div>
                        </div>
                    ` + "`" + `;
                } else {
                    throw new Error(data.error);
                }
            } catch (error) {
                document.getElementById('stats-grid').innerHTML = ` + "`" + `<div class="error">Failed to load statistics: ${error.message}</div>` + "`" + `;
            }
        }

        async function loadPosts() {
            try {
                const response = await fetch('/api/posts?page_size=10');
                const data = await response.json();
                
                if (data.success) {
                    const container = document.getElementById('posts-container');
                    if (data.data.posts.length === 0) {
                        container.innerHTML = '<p>No posts found. Try running the scraper first.</p>';
                        return;
                    }
                    
                    container.innerHTML = data.data.posts.map(post => ` + "`" + `
                        <div class="post-item">
                            <div class="post-author">${post.author_name} • ${post.group_name}</div>
                            <div class="post-content">${post.content.substring(0, 200)}${post.content.length > 200 ? '...' : ''}</div>
                            <div class="post-stats">
                                <span>👍 ${post.likes}</span>
                                <span>💬 ${post.comments}</span>
                                <span>🔄 ${post.shares}</span>
                                <span>📅 ${new Date(post.timestamp).toLocaleDateString()}</span>
                            </div>
                        </div>
                    ` + "`" + `).join('');
                } else {
                    throw new Error(data.error);
                }
            } catch (error) {
                document.getElementById('posts-container').innerHTML = ` + "`" + `<div class="error">Failed to load posts: ${error.message}</div>` + "`" + `;
            }
        }

        function refreshData() {
            loadStats();
            loadPosts();
        }

        function exportCSV() {
            window.open('/api/export/csv', '_blank');
        }

        // Load data on page load
        document.addEventListener('DOMContentLoaded', function() {
            loadStats();
            loadPosts();
        });
    </script>
</body>
</html>
    `
    w.Header().Set("Content-Type", "text/html")
    w.Write([]byte(html))
}

func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, message string, statusCode int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    response := APIResponse{
        Success: false,
        Error:   message,
    }
    json.NewEncoder(w).Encode(response)
}