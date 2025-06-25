#!/bin/bash
# filepath: /home/tufstraka/Desktop/ris/facebook-scraper/scripts/docker_run.sh

echo "Facebook Scraper - Docker Setup"
echo "==============================="

# Check if .env file exists
if [ ! -f ".env" ]; then
    echo "Creating .env file from .env.example..."
    if [ -f ".env.example" ]; then
        cp .env.example .env
        echo "Please update .env file with your configuration"
    else
        echo "Warning: .env.example not found, creating basic .env file"
        cat > .env << 'EOF'
DB_HOST=postgres
DB_PORT=5432
DB_USER=scraper_user
DB_PASSWORD=scraper_password
DB_NAME=facebook_scraper
DB_SSL_MODE=disable
LOG_LEVEL=info
EOF
    fi
fi

# Check if cookies file exists
if [ ! -f "configs/cookies.json" ]; then
    echo "Error: configs/cookies.json not found!"
    echo "Please create your cookies file first using:"
    echo "./bin/facebook-scraper -extract-cookies"
    exit 1
fi

# Check if groups file exists
if [ ! -f "configs/groups.yaml" ]; then
    echo "Error: configs/groups.yaml not found!"
    echo "Please create the groups configuration file"
    exit 1
fi

# Create necessary directories
mkdir -p logs data

echo "Starting Docker services..."

# Clean up any existing containers
echo "Cleaning up existing containers..."
docker compose down

# Start the database first
echo "Starting PostgreSQL database..."
docker compose up -d postgres

echo "Waiting for database to be ready..."
# Wait for database to be healthy
max_attempts=30
attempt=1

while [ $attempt -le $max_attempts ]; do
    echo "Attempt $attempt/$max_attempts: Checking database health..."
    
    if docker compose exec postgres pg_isready -U scraper_user -d facebook_scraper > /dev/null 2>&1; then
        echo "✅ Database is ready!"
        break
    fi
    
    if [ $attempt -eq $max_attempts ]; then
        echo "❌ Database failed to start after $max_attempts attempts"
        echo "Checking database logs:"
        docker compose logs postgres
        exit 1
    fi
    
    echo "Database not ready yet, waiting 5 seconds..."
    sleep 5
    attempt=$((attempt + 1))
done

# Start PgAdmin (optional)
echo "Starting PgAdmin..."
docker compose up -d pgadmin

# Build and start the scraper
echo "Building and starting the scraper..."
docker compose up --build facebook-scraper

echo ""
echo "Scraper completed!"
echo ""
echo "📊 View your data:"
echo "  • PgAdmin: http://localhost:8080"
echo "  • Database: postgresql://scraper_user:scraper_password@localhost:5432/facebook_scraper"
echo ""
echo "📋 View logs:"
echo "  • docker compose logs facebook-scraper"
echo "  • docker compose logs postgres"