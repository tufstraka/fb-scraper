#!/bin/bash
# filepath: /home/tufstraka/Desktop/ris/facebook-scraper/scripts/docker_dev.sh

echo "Facebook Scraper - Development Mode"
echo "==================================="

# Start only database and redis for development
docker compose up -d postgres redis pgadmin

echo "Development services started!"
echo ""
echo "Database: postgresql://scraper_user:scraper_password@localhost:5432/facebook_scraper"
echo "PgAdmin: http://localhost:8080 (admin@scraper.local / admin123)"
echo "Redis: redis://localhost:6379"
echo ""
echo "To run scraper locally:"
echo "export DB_HOST=localhost"
echo "go run cmd/scraper/main.go -config=configs/config.yaml"