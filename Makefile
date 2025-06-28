# Facebook Scraper Makefile

.PHONY: help build clean setup test run dev docker-build docker-run api monitor

# Default target
help:
	@echo "Facebook Scraper - Available Commands"
	@echo "===================================="
	@echo ""
	@echo "Setup & Build:"
	@echo "  setup          - Complete system setup"
	@echo "  build          - Build all applications"
	@echo "  clean          - Clean build artifacts"
	@echo ""
	@echo "Development:"
	@echo "  dev            - Start development environment"
	@echo "  test           - Run tests"
	@echo "  test-cookies   - Test cookie authentication"
	@echo ""
	@echo "Running:"
	@echo "  run            - Run the scraper"
	@echo "  api            - Start API server"
	@echo "  monitor        - Show monitoring report"
	@echo "  run-all        - Start complete system"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build   - Build Docker images"
	@echo "  docker-run     - Run with Docker"
	@echo "  docker-dev     - Start Docker development environment"
	@echo ""
	@echo "Data & Maintenance:"
	@echo "  view-results   - View scraping results"
	@echo "  export-csv     - Export data to CSV"
	@echo "  backup-db      - Backup database"
	@echo "  logs           - View recent logs"

# Setup and build targets
setup:
	@echo "Running complete setup..."
	@chmod +x scripts/*.sh
	@./scripts/setup.sh

build:
	@echo "Building all applications..."
	@mkdir -p bin logs data
	@go build -o bin/facebook-scraper cmd/scraper/main.go
	@go build -o bin/api-server cmd/api/main.go
	@go build -o bin/monitor cmd/monitor/main.go
	@go build -o bin/test-cookies cmd/test-cookies/main.go
	@echo "✅ Build completed"

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -f logs/*.log
	@docker-compose down --volumes --remove-orphans 2>/dev/null || true
	@echo "✅ Cleanup completed"

# Development targets
dev: build
	@echo "Starting development environment..."
	@./scripts/docker_dev.sh

test:
	@echo "Running tests..."
	@go test ./... -v

test-cookies: build
	@echo "Testing cookie authentication..."
	@./bin/test-cookies

# Running targets
run: build
	@echo "Running Facebook scraper..."
	@./bin/facebook-scraper -config=configs/config.yaml

api: build
	@echo "Starting API server..."
	@./bin/api-server -port=8080

monitor: build
	@echo "Generating monitoring report..."
	@./bin/monitor -report

run-all:
	@echo "Starting complete system..."
	@./scripts/run_all.sh

# Docker targets
docker-build:
	@echo "Building Docker images..."
	@docker-compose build

docker-run:
	@echo "Running with Docker..."
	@./scripts/docker_run.sh

docker-dev:
	@echo "Starting Docker development environment..."
	@./scripts/docker_dev.sh

# Data and maintenance targets
view-results:
	@echo "Viewing scraping results..."
	@./scripts/view_results.sh

export-csv:
	@echo "Exporting data to CSV..."
	@curl -s http://localhost:8080/api/export/csv -o "data/export_$(shell date +%Y%m%d_%H%M%S).csv"
	@echo "✅ Data exported to data/ directory"

backup-db:
	@echo "Creating database backup..."
	@mkdir -p backups
	@docker-compose exec postgres pg_dump -U scraper_user facebook_scraper > "backups/backup_$(shell date +%Y%m%d_%H%M%S).sql"
	@echo "✅ Database backup created in backups/ directory"

logs:
	@echo "Recent logs:"
	@echo "============"
	@echo ""
	@echo "Scraper logs:"
	@tail -n 20 logs/scraper.log 2>/dev/null || echo "No scraper logs found"
	@echo ""
	@echo "API server logs:"
	@tail -n 20 logs/api-server.log 2>/dev/null || echo "No API server logs found"
	@echo ""
	@echo "Docker logs:"
	@docker-compose logs --tail=10 2>/dev/null || echo "No Docker logs found"

# Installation targets for different environments
install-dev:
	@echo "Installing development dependencies..."
	@go mod download
	@go install github.com/air-verse/air@latest
	@echo "✅ Development dependencies installed"

install-prod:
	@echo "Installing production dependencies..."
	@go mod download
	@echo "✅ Production dependencies installed"

# Health check targets
health:
	@echo "Checking system health..."
	@./bin/monitor -alerts 2>/dev/null || echo "Monitor not available"
	@curl -s http://localhost:8080/api/health 2>/dev/null | grep -q "healthy" && echo "✅ API server healthy" || echo "❌ API server not responding"
	@docker-compose ps 2>/dev/null || echo "Docker services not running"

# Quick start for new users
quick-start: setup build docker-dev
	@echo ""
	@echo "🎉 Quick start completed!"
	@echo ""
	@echo "Next steps:"
	@echo "1. Configure cookies: go run scripts/extract_cookies.go"
	@echo "2. Update groups: edit configs/groups.yaml"
	@echo "3. Run scraper: make run"
	@echo "4. View dashboard: http://localhost:8080/dashboard"

# Update dependencies
update-deps:
	@echo "Updating Go dependencies..."
	@go get -u ./...
	@go mod tidy
	@echo "✅ Dependencies updated"