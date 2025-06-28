#!/bin/bash

echo "Facebook Scraper - Complete Setup"
echo "=================================="

# Create necessary directories
echo "Creating directories..."
mkdir -p logs data web/static configs

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.19 or later."
    exit 1
fi

echo "✅ Go is installed: $(go version)"

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker."
    exit 1
fi

echo "✅ Docker is installed: $(docker --version)"

# Check if Docker Compose is installed
if ! command -v docker-compose &> /dev/null; then
    echo "❌ Docker Compose is not installed. Please install Docker Compose."
    exit 1
fi

echo "✅ Docker Compose is installed: $(docker-compose --version)"

# Create .env file if it doesn't exist
if [ ! -f ".env" ]; then
    echo "Creating .env file..."
    cp .env.example .env
    echo "✅ Created .env file from example"
else
    echo "✅ .env file already exists"
fi

# Check if cookies file exists
if [ ! -f "configs/cookies.json" ]; then
    echo "⚠️  Warning: configs/cookies.json not found!"
    echo "You'll need to create this file with your Facebook cookies."
    echo "Run: go run scripts/extract_cookies.go for instructions"
fi

# Check if groups file exists
if [ ! -f "configs/groups.yaml" ]; then
    echo "✅ Creating default groups.yaml file..."
    cat > configs/groups.yaml << 'EOF'
groups:
  - id: "613870175328566"
    name: "NETFLIX RECOMMENDATIONS"
  # Add more groups here
  # - id: "YOUR_GROUP_ID"
  #   name: "YOUR_GROUP_NAME"
EOF
fi

# Download Go dependencies
echo "Downloading Go dependencies..."
go mod download
if [ $? -ne 0 ]; then
    echo "❌ Failed to download Go dependencies"
    exit 1
fi

echo "✅ Go dependencies downloaded"

# Build the applications
echo "Building applications..."

# Build main scraper
go build -o bin/facebook-scraper cmd/scraper/main.go
if [ $? -ne 0 ]; then
    echo "❌ Failed to build main scraper"
    exit 1
fi

# Build API server
go build -o bin/api-server cmd/api/main.go
if [ $? -ne 0 ]; then
    echo "❌ Failed to build API server"
    exit 1
fi

# Build monitor
go build -o bin/monitor cmd/monitor/main.go
if [ $? -ne 0 ]; then
    echo "❌ Failed to build monitor"
    exit 1
fi

# Build cookie tester
go build -o bin/test-cookies cmd/test-cookies/main.go
if [ $? -ne 0 ]; then
    echo "❌ Failed to build cookie tester"
    exit 1
fi

echo "✅ All applications built successfully"

# Make scripts executable
chmod +x scripts/*.sh

echo ""
echo "🎉 Setup completed successfully!"
echo ""
echo "Next steps:"
echo "1. Configure your Facebook cookies in configs/cookies.json"
echo "   Run: go run scripts/extract_cookies.go for instructions"
echo ""
echo "2. Update configs/groups.yaml with your target groups"
echo ""
echo "3. Start the system:"
echo "   ./scripts/docker_run.sh    # Full Docker setup"
echo "   ./scripts/docker_dev.sh    # Development mode"
echo ""
echo "4. Available commands:"
echo "   ./bin/facebook-scraper      # Run scraper"
echo "   ./bin/api-server            # Start API server"
echo "   ./bin/monitor -report       # View monitoring report"
echo "   ./bin/test-cookies          # Test cookie authentication"
echo ""
echo "5. Access the dashboard:"
echo "   http://localhost:8080/dashboard (after starting API server)"
echo ""
echo "6. Database access:"
echo "   PgAdmin: http://localhost:8080 (admin@localhost.com / admin123)"
echo "   Direct: psql -h localhost -p 5433 -U scraper_user -d facebook_scraper"