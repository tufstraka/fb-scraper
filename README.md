# Facebook Scraper - Complete Production-Ready System

A comprehensive, production-ready Facebook scraper built with Go, featuring advanced parsing, real-time monitoring, web dashboard, API endpoints, and robust error handling.

## 🚀 Features

### Core Functionality
- **Advanced Facebook Scraping** with multiple parsing strategies
- **Cookie-based Authentication** with automatic validation
- **Multi-format Support** (mobile, desktop, mbasic Facebook layouts)
- **Intelligent Content Extraction** (posts, images, videos, engagement metrics)
- **Robust Error Handling** with retry mechanisms and fallback strategies

### Data Management
- **PostgreSQL Database** with optimized schema and indexes
- **Automatic Migrations** with comprehensive table structure
- **Data Deduplication** and validation
- **Flexible Filtering** (likes, time range, keywords, authors)
- **Export Capabilities** (CSV, JSON with API endpoints)

### Monitoring & Analytics
- **Real-time Monitoring** with metrics collection
- **Performance Tracking** (success rates, processing times, error rates)
- **Health Checks** and alerting system
- **Comprehensive Logging** with structured output
- **Statistical Analysis** and trend reporting

### Web Interface
- **Modern Web Dashboard** with real-time data visualization
- **RESTful API** with comprehensive endpoints
- **Interactive Statistics** and charts
- **Export Functionality** directly from web interface
- **Responsive Design** for mobile and desktop

### DevOps & Deployment
- **Docker-first Architecture** with multi-container setup
- **Development & Production** configurations
- **Automated Setup Scripts** and build system
- **Health Monitoring** and container orchestration
- **Backup & Recovery** procedures

## 📋 Prerequisites

- **Docker & Docker Compose** (recommended)
- **Go 1.19+** (for local development)
- **PostgreSQL** (handled by Docker)
- **Valid Facebook Account** with cookies

## 🛠️ Quick Start

### 1. Clone and Setup
```bash
git clone <repository-url>
cd facebook-scraper

# Complete automated setup
make quick-start
```

### 2. Configure Authentication
```bash
# Get cookie extraction instructions
go run scripts/extract_cookies.go

# Test your cookies
make test-cookies
```

### 3. Configure Target Groups
```yaml
# Edit configs/groups.yaml
groups:
  - id: "YOUR_GROUP_ID"
    name: "YOUR_GROUP_NAME"
```

### 4. Run the System
```bash
# Option 1: Complete system with dashboard
make run-all

# Option 2: Docker-only approach
make docker-run

# Option 3: Development mode
make dev
```

## 🎯 Usage Examples

### Basic Scraping
```bash
# Run scraper with default settings
make run

# Run with custom configuration
./bin/facebook-scraper -config=configs/config.yaml
```

### API Usage
```bash
# Get posts with pagination
curl "http://localhost:8080/api/posts?page=1&page_size=20&min_likes=1000"

# Get posts by group
curl "http://localhost:8080/api/posts/group/613870175328566"

# Get statistics
curl "http://localhost:8080/api/stats"

# Export to CSV
curl "http://localhost:8080/api/export/csv" -o posts.csv

# Health check
curl "http://localhost:8080/api/health"
```

### Monitoring
```bash
# View comprehensive report
make monitor

# Check system health
./bin/monitor -alerts

# View recent logs
make logs
```

## 🏗️ Architecture

### System Components
```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Web Dashboard │    │   API Server    │    │   Scraper Core  │
│   (React/HTML)  │◄──►│   (Go/HTTP)     │◄──►│   (Go/Parser)   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   PostgreSQL    │    │   Monitoring    │    │   Auth Manager  │
│   (Database)    │    │   (Metrics)     │    │   (Cookies)     │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### Data Flow
1. **Authentication**: Cookie validation and session management
2. **Scraping**: Multi-strategy content extraction from Facebook
3. **Processing**: Data parsing, validation, and filtering
4. **Storage**: PostgreSQL with optimized schema
5. **API**: RESTful endpoints for data access
6. **Monitoring**: Real-time metrics and health checks
7. **Dashboard**: Web interface for visualization and control

## 📊 Web Dashboard

Access the dashboard at `http://localhost:8080/dashboard`

### Features
- **Real-time Statistics** (total posts, engagement metrics, trends)
- **Post Browsing** with filtering and search
- **Export Functions** (CSV download)
- **System Health** monitoring
- **Interactive Charts** and visualizations

### API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/posts` | GET | List posts with pagination |
| `/api/posts/group/{id}` | GET | Get posts by group ID |
| `/api/stats` | GET | Get scraping statistics |
| `/api/export/csv` | GET | Export posts to CSV |
| `/api/health` | GET | System health check |
| `/dashboard` | GET | Web dashboard |

## 🔧 Configuration

### Main Configuration (`configs/config.yaml`)
```yaml
facebook:
  base_url: "https://www.facebook.com"
  mobile_url: "https://m.facebook.com"
  timeout: 30
  rate_limit:
    requests_per_minute: 10
    delay_between_requests: 6
  auth:
    method: "cookies"
    cookies_file: "configs/cookies.json"
    user_agent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36"

scraper:
  concurrent_workers: 3
  retry_attempts: 3
  retry_delay: 5
  output_format: "json"

database:
  host: "postgres"
  port: 5432
  name: "facebook_scraper"
  user: "scraper_user"
  password: "scraper_password"
  ssl_mode: "disable"

logging:
  level: "info"
  file: "logs/scraper.log"
```

### Groups Configuration (`configs/groups.yaml`)
```yaml
groups:
  - id: "613870175328566"
    name: "NETFLIX RECOMMENDATIONS"
  - id: "YOUR_GROUP_ID"
    name: "YOUR_GROUP_NAME"
```

### Environment Variables (`.env`)
```env
DB_HOST=postgres
DB_PORT=5432
DB_USER=scraper_user
DB_PASSWORD=scraper_password
DB_NAME=facebook_scraper
DB_SSL_MODE=disable
LOG_LEVEL=info
ENVIRONMENT=docker
```

## 🐳 Docker Deployment

### Production Deployment
```bash
# Build and start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Scale services
docker-compose up -d --scale facebook-scraper=3
```

### Development Environment
```bash
# Start development services only
make docker-dev

# Run scraper locally with Docker database
export DB_HOST=localhost
make run
```

### Services Overview

| Service | Port | Description |
|---------|------|-------------|
| `postgres` | 5433 | PostgreSQL database |
| `pgadmin` | 8080 | Database admin interface |
| `api-server` | 8081 | API server (dev mode) |
| `redis` | 6379 | Caching (optional) |

## 📈 Monitoring & Alerting

### Built-in Monitoring
- **Performance Metrics**: Processing times, success rates, error rates
- **System Health**: Database connectivity, service status
- **Data Quality**: Post counts, engagement trends, group coverage
- **Alerting**: Automated alerts for failures and anomalies

### Monitoring Commands
```bash
# Generate comprehensive report
make monitor

# Check for alerts
./bin/monitor -alerts

# View system health
./bin/monitor

# Export metrics
./bin/monitor -report > monitoring_report.txt
```

### Log Files
- `logs/scraper.log` - Main scraper logs
- `logs/api-server.log` - API server logs
- `logs/monitoring-report.txt` - Monitoring reports
- `logs/cookie-test.log` - Authentication test logs

## 🔒 Security & Compliance

### Authentication Security
- **Secure Cookie Storage** with proper file permissions
- **Session Validation** with automatic renewal
- **Rate Limiting** to respect Facebook's terms
- **User-Agent Rotation** for better anonymity

### Data Privacy
- **GDPR Compliance** considerations
- **Data Minimization** - only collect necessary data
- **Secure Storage** with encrypted database options
- **Access Controls** with API authentication

### Best Practices
- **Respect Rate Limits** - built-in delays and throttling
- **Monitor Usage** - track requests and avoid abuse
- **Regular Updates** - keep cookies and configurations current
- **Error Handling** - graceful degradation and recovery

## 🛠️ Development

### Local Development Setup
```bash
# Install development dependencies
make install-dev

# Start development environment
make dev

# Run tests
make test

# Build applications
make build
```

### Project Structure
```
facebook-scraper/
├── cmd/                    # Application entry points
│   ├── scraper/           # Main scraper application
│   ├── api/               # API server
│   ├── monitor/           # Monitoring tools
│   └── test-cookies/      # Cookie testing utility
├── internal/              # Private application code
│   ├── api/               # API server implementation
│   ├── config/            # Configuration management
│   ├── database/          # Database operations & migrations
│   ├── monitoring/        # Monitoring and metrics
│   ├── scraper/           # Core scraping logic
│   └── utils/             # Utility functions
├── pkg/                   # Public packages
│   └── types/             # Shared type definitions
├── configs/               # Configuration files
├── scripts/               # Automation scripts
├── logs/                  # Application logs
├── data/                  # Data storage and exports
└── web/                   # Web dashboard assets
```

### Adding New Features
1. **Extend the scraper**: Add new parsing strategies in `internal/scraper/`
2. **Add API endpoints**: Extend `internal/api/server.go`
3. **Enhance monitoring**: Add metrics in `internal/monitoring/`
4. **Improve dashboard**: Modify the HTML in `handleDashboard()`

## 🚨 Troubleshooting

### Common Issues

**Authentication Failures**
```bash
# Test cookies
make test-cookies

# Re-extract cookies
go run scripts/extract_cookies.go

# Check cookie format
cat configs/cookies.json | jq .
```

**Database Connection Issues**
```bash
# Check database health
docker-compose ps postgres

# Reset database
docker-compose down
docker volume rm facebook_scraper_postgres_data
docker-compose up -d postgres
```

**Scraping Failures**
```bash
# Check logs
tail -f logs/scraper.log

# Test with single group
./bin/facebook-scraper -config=configs/config.yaml

# Verify group IDs
curl "https://www.facebook.com/groups/YOUR_GROUP_ID"
```

**Performance Issues**
```bash
# Monitor system resources
docker stats

# Check database performance
./scripts/view_results.sh

# Analyze logs for bottlenecks
grep "ERROR\|WARN" logs/scraper.log
```

### Debug Mode
```bash
# Enable debug logging
export LOG_LEVEL=debug

# Run with verbose output
./bin/facebook-scraper -config=configs/config.yaml -v

# Check API server debug info
curl "http://localhost:8080/api/health" | jq .
```

## 📚 Advanced Usage

### Custom Filters
```go
// Example: Custom post filter
filter := &types.PostFilter{
    MinLikes:        1000,
    DaysBack:        7,
    Keywords:        []string{"netflix", "movie", "series"},
    ExcludeKeywords: []string{"spam", "advertisement"},
    AuthorNames:     []string{"trusted_user1", "trusted_user2"},
}
```

### Batch Processing
```bash
# Process multiple groups
for group in group1 group2 group3; do
    ./bin/facebook-scraper -group=$group
done

# Scheduled runs with cron
0 */6 * * * /path/to/facebook-scraper/bin/facebook-scraper
```

### Data Analysis
```sql
-- Top posts by engagement
SELECT author_name, content, likes, comments, shares 
FROM posts 
WHERE likes >= 1000 
ORDER BY (likes + comments + shares) DESC 
LIMIT 10;

-- Engagement trends
SELECT DATE(timestamp) as date, AVG(likes) as avg_likes 
FROM posts 
WHERE timestamp >= NOW() - INTERVAL '30 days' 
GROUP BY DATE(timestamp) 
ORDER BY date;

-- Top authors
SELECT author_name, COUNT(*) as post_count, AVG(likes) as avg_likes 
FROM posts 
GROUP BY author_name 
ORDER BY post_count DESC 
LIMIT 10;
```

## 🤝 Contributing

1. **Fork the repository**
2. **Create a feature branch**: `git checkout -b feature/amazing-feature`
3. **Make your changes** with proper tests
4. **Commit your changes**: `git commit -m 'Add amazing feature'`
5. **Push to the branch**: `git push origin feature/amazing-feature`
6. **Open a Pull Request**

### Development Guidelines
- Follow Go best practices and conventions
- Add tests for new functionality
- Update documentation for API changes
- Ensure Docker compatibility
- Test with multiple Facebook group types

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## ⚠️ Disclaimer

This tool is for educational and research purposes only. Users are responsible for:
- Complying with Facebook's Terms of Service
- Respecting data privacy laws (GDPR, CCPA, etc.)
- Using the tool ethically and responsibly
- Not violating any applicable laws or regulations

The authors are not responsible for any misuse of this software.

## 🆘 Support

- **Documentation**: Check this README and inline code comments
- **Issues**: Open a GitHub issue for bugs or feature requests
- **Discussions**: Use GitHub Discussions for questions and ideas
- **Logs**: Always check log files for detailed error information

## 🔄 Updates & Maintenance

### Regular Maintenance
- **Update cookies** when authentication fails
- **Monitor rate limits** and adjust delays as needed
- **Update dependencies** regularly for security
- **Backup database** before major updates
- **Review logs** for performance optimization opportunities

### Version Updates
```bash
# Update dependencies
make update-deps

# Rebuild applications
make clean && make build

# Test new version
make test
```

---

**Built with ❤️ using Go, PostgreSQL, and Docker**