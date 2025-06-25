#!/bin/bash

echo "Facebook Scraper with Cookie Authentication"
echo "==========================================="

# Check if cookies file exists
if [ ! -f "configs/cookies.json" ]; then
    echo "Error: configs/cookies.json not found!"
    echo "Run: ./bin/facebook-scraper -extract-cookies for instructions"
    exit 1
fi

# Check if groups file exists
if [ ! -f "configs/groups.yaml" ]; then
    echo "Error: configs/groups.yaml not found!"
    echo "Please create the groups configuration file"
    exit 1
fi

# Build the scraper
echo "Building scraper..."
go build -o bin/facebook-scraper cmd/scraper/main.go

if [ $? -ne 0 ]; then
    echo "Build failed!"
    exit 1
fi

echo "Build successful!"

# Create logs directory if it doesn't exist
mkdir -p logs

# Run the scraper
echo "Starting scraper..."
./bin/facebook-scraper -config=configs/config.yaml

echo "Scraping completed!"