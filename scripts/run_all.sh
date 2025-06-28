#!/bin/bash

echo "Facebook Scraper - Complete System Startup"
echo "=========================================="

# Function to check if a port is in use
check_port() {
    if lsof -Pi :$1 -sTCP:LISTEN -t >/dev/null ; then
        echo "⚠️  Port $1 is already in use"
        return 1
    fi
    return 0
}

# Function to wait for service to be ready
wait_for_service() {
    local url=$1
    local service_name=$2
    local max_attempts=30
    local attempt=1

    echo "Waiting for $service_name to be ready..."
    while [ $attempt -le $max_attempts ]; do
        if curl -s "$url" > /dev/null 2>&1; then
            echo "✅ $service_name is ready!"
            return 0
        fi
        echo "Attempt $attempt/$max_attempts: $service_name not ready yet..."
        sleep 2
        attempt=$((attempt + 1))
    done
    
    echo "❌ $service_name failed to start after $max_attempts attempts"
    return 1
}

# Check prerequisites
if [ ! -f "configs/cookies.json" ]; then
    echo "❌ configs/cookies.json not found!"
    echo "Please create your cookies file first."
    echo "Run: go run scripts/extract_cookies.go for instructions"
    exit 1
fi

if [ ! -f "configs/groups.yaml" ]; then
    echo "❌ configs/groups.yaml not found!"
    echo "Please create the groups configuration file"
    exit 1
fi

# Check if binaries exist
if [ ! -f "bin/facebook-scraper" ] || [ ! -f "bin/api-server" ]; then
    echo "Building applications..."
    ./scripts/setup.sh
fi

# Start Docker services (database, etc.)
echo "Starting Docker services..."
docker-compose up -d postgres pgadmin

# Wait for database to be ready
if ! wait_for_service "http://localhost:5433" "PostgreSQL"; then
    echo "❌ Database failed to start"
    exit 1
fi

# Run database migrations
echo "Running database migrations..."
docker-compose exec postgres psql -U scraper_user -d facebook_scraper -c "SELECT 1;" > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✅ Database is accessible"
else
    echo "❌ Database is not accessible"
    exit 1
fi

# Start API server in background
echo "Starting API server..."
if check_port 8080; then
    ./bin/api-server -port=8080 > logs/api-server.log 2>&1 &
    API_PID=$!
    echo "API server started with PID: $API_PID"
    
    # Wait for API server to be ready
    if wait_for_service "http://localhost:8080/api/health" "API Server"; then
        echo "✅ API server is running at http://localhost:8080"
        echo "✅ Dashboard available at http://localhost:8080/dashboard"
    else
        echo "❌ API server failed to start"
        kill $API_PID 2>/dev/null
        exit 1
    fi
else
    echo "❌ Cannot start API server - port 8080 is in use"
    exit 1
fi

# Test cookie authentication
echo "Testing cookie authentication..."
./bin/test-cookies > logs/cookie-test.log 2>&1
if [ $? -eq 0 ]; then
    echo "✅ Cookie authentication successful"
else
    echo "⚠️  Cookie authentication failed - check logs/cookie-test.log"
    echo "You may need to update your cookies"
fi

# Run the scraper
echo "Starting Facebook scraper..."
./bin/facebook-scraper -config=configs/config.yaml > logs/scraper-run.log 2>&1
SCRAPER_EXIT_CODE=$?

if [ $SCRAPER_EXIT_CODE -eq 0 ]; then
    echo "✅ Scraper completed successfully"
else
    echo "⚠️  Scraper completed with errors (exit code: $SCRAPER_EXIT_CODE)"
    echo "Check logs/scraper-run.log for details"
fi

# Generate monitoring report
echo "Generating monitoring report..."
./bin/monitor -report > logs/monitoring-report.txt 2>&1
echo "✅ Monitoring report saved to logs/monitoring-report.txt"

# Show final status
echo ""
echo "🎉 System startup completed!"
echo ""
echo "📊 Services Status:"
echo "  • PostgreSQL: http://localhost:5433"
echo "  • PgAdmin: http://localhost:8080 (admin@localhost.com / admin123)"
echo "  • API Server: http://localhost:8080"
echo "  • Dashboard: http://localhost:8080/dashboard"
echo ""
echo "📁 Log Files:"
echo "  • API Server: logs/api-server.log"
echo "  • Scraper: logs/scraper-run.log"
echo "  • Cookie Test: logs/cookie-test.log"
echo "  • Monitoring: logs/monitoring-report.txt"
echo ""
echo "🔧 Useful Commands:"
echo "  • View scraper results: ./scripts/view_results.sh"
echo "  • Check system health: ./bin/monitor -alerts"
echo "  • Export data: curl http://localhost:8080/api/export/csv"
echo "  • Stop services: docker-compose down && kill $API_PID"
echo ""

# Keep API server running
echo "API server is running in background (PID: $API_PID)"
echo "Press Ctrl+C to stop all services"

# Trap Ctrl+C to cleanup
trap 'echo "Stopping services..."; kill $API_PID 2>/dev/null; docker-compose down; exit 0' INT

# Wait for user to stop
wait $API_PID