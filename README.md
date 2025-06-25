# Facebook Scraper

A robust Go-based Facebook scraper with Docker support, designed to extract and process Facebook data efficiently while maintaining proper authentication and data persistence.

## 🚀 Features

- **Docker-first architecture** with PostgreSQL and PgAdmin integration
- **Cookie-based authentication** for reliable Facebook access
- **Configurable scraping** with YAML-based configuration files
- **Database persistence** with automatic migrations
- **Comprehensive logging** for monitoring and debugging
- **Group-based scraping** with customizable filters
- **Health checks** and container monitoring

## 📋 Prerequisites

- Docker and Docker Compose installed
- Git for cloning the repository
- Basic understanding of Facebook's terms of service

## 🛠️ Installation & Setup

### 1. Clone the Repository

```bash
git clone <repository-url>
cd facebook-scraper
```

### 2. Environment Configuration

Copy the example environment file and configure it:

```bash
cp .env.example .env
```

Edit the `.env` file with your settings:

```env
# Database Configuration
DB_HOST=postgres
DB_PORT=5432
DB_USER=scraper_user
DB_PASSWORD=scraper_password
DB_NAME=facebook_scraper
DB_SSL_MODE=disable

# Redis Configuration (if needed)
REDIS_HOST=redis
REDIS_PORT=6379

# Application Configuration
LOG_LEVEL=info
ENVIRONMENT=docker

# PgAdmin Configuration
PGADMIN_DEFAULT_EMAIL=admin@example.com
PGADMIN_DEFAULT_PASSWORD=admin123
```

### 3. Configure Scraping Settings

#### Main Configuration (`configs/config.yaml`)
```yaml
# Customize your scraping parameters
# See config.yaml for detailed options
```

#### Groups Configuration (`configs/groups.yaml`)
```yaml
# Define Facebook groups to scrape
# Add group IDs and specific settings
```

#### Cookies Setup (`configs/cookies.json`)
```json
// Add your Facebook authentication cookies
// Use the provided extraction script
```

### 4. Extract Facebook Cookies

Use the provided script to extract authentication cookies:

```bash
./scripts/extractCookies.sh
```

This will help you obtain the necessary authentication cookies for Facebook access.

## 🐳 Docker Deployment

### Quick Start

```bash
# Build and start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Check service status
docker-compose ps
```

### Services Overview

| Service | Port | Description |
|---------|------|-------------|
| `postgres` | 5433 | PostgreSQL database |
| `pgadmin` | 8080 | Database administration interface |
| facebook-scraper | - | Main scraper application |

### Database Access

**Via PgAdmin (Recommended):**
1. Open http://localhost:8080
2. Login with credentials from `.env` file
3. Add server with connection details:
   - Host: `postgres`
   - Port: `5432`
   - Username: `scraper_user`
   - Password: `scraper_password`
   - Database: `facebook_scraper`

**Direct Connection:**
```bash
psql -h localhost -p 5433 -U scraper_user -d facebook_scraper
```

## 🔧 Development Setup

### Local Development

```bash
# Install Go dependencies
go mod download

# Build the application
go build -o bin/facebook-scraper cmd/scraper/main.go

# Run locally (ensure database is running)
./bin/facebook-scraper
```

### Available Scripts

```bash
# Development with Docker
./scripts/docker_dev.sh

# Production Docker run
./scripts/docker_run.sh

# Run scraper directly
./scripts/run_scraper.sh

# View scraping results
./scripts/view_results.sh

# Extract cookies from browser
./scripts/extractCookies.sh
```

## 📂 Project Structure

```
facebook-scraper/
├── cmd/                    # Application entry points
│   ├── scraper/           # Main scraper application
│   └── test-cookies/      # Cookie testing utility
├── internal/              # Private application code
│   ├── config/           # Configuration management
│   ├── database/         # Database operations & migrations
│   ├── scraper/          # Core scraping logic
│   └── utils/            # Utility functions
├── configs/              # Configuration files
├── scripts/              # Automation scripts
├── logs/                 # Application logs
├── data/                 # Scraped data storage
└── pkg/                  # Public packages
```

## 🔍 Usage

### Basic Scraping

1. **Configure your targets** in `configs/groups.yaml`
2. **Set up authentication** cookies in `configs/cookies.json`
3. **Start the scraper**:
   ```bash
   docker-compose up facebook-scraper
   ```

### Monitoring

- **Application logs**: `docker-compose logs facebook-scraper`
- **Database logs**: `docker-compose logs postgres`
- **Log files**: Check `logs/scraper.log`

### Data Access

Scraped data is stored in PostgreSQL and can be accessed via:
- PgAdmin web interface (http://localhost:8080)
- Direct database connection
- Application APIs (if implemented)

## 🛡️ Security & Compliance

- **Cookie Security**: Store authentication cookies securely
- **Rate Limiting**: Built-in delays to respect Facebook's rate limits
- **Data Privacy**: Ensure compliance with data protection regulations
- **Terms of Service**: Review and comply with Facebook's terms of service

## 🐛 Troubleshooting

### Common Issues

**Container Connection Issues:**
```bash
# Check container status
docker-compose ps

# Restart specific service
docker-compose restart postgres
```

**Database Connection Problems:**
```bash
# Check database health
docker inspect --format='{{.State.Health.Status}}' facebook_scraper_db

# Reset database
docker-compose down
docker volume rm facebook_scraper_postgres_data
docker-compose up -d
```

**Cookie Authentication Failures:**
1. Re-extract cookies using `./scripts/extractCookies.sh`
2. Verify cookie format in `configs/cookies.json`
3. Check if Facebook session is still valid

### Logs and Debugging

```bash
# View all logs
docker-compose logs

# Follow specific service logs
docker-compose logs -f facebook-scraper

# Check application log file
tail -f logs/scraper.log
```

## 📝 Configuration Reference

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `DB_HOST` | Database host | `postgres` |
| `DB_PORT` | Database port | `5432` |
| `LOG_LEVEL` | Logging level | `info` |
| `ENVIRONMENT` | Runtime environment | `docker` |

### Configuration Files

- **`config.yaml`**: Main application settings
- **`groups.yaml`**: Target groups and scraping rules
- **`cookies.json`**: Authentication cookies
