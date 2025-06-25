#!/bin/bash

echo "Facebook Scraper Results - Posts with 1000+ likes in past 5 days"
echo "=================================================================="

# Check if PostgreSQL container is running
if ! docker compose ps postgres | grep -q "Up"; then
    echo "Starting PostgreSQL container..."
    docker compose up -d postgres
    sleep 10
fi

echo "Querying database for results..."

# Query high-engagement posts
docker compose exec postgres psql -U scraper_user -d facebook_scraper -c "
SELECT 
    group_name,
    author_name,
    LEFT(content, 100) as content_preview,
    likes,
    comments,
    shares,
    post_type,
    timestamp,
    scraped_at
FROM posts 
WHERE likes >= 1000 
    AND scraped_at >= NOW() - INTERVAL '5 days'
ORDER BY likes DESC, scraped_at DESC
LIMIT 20;
"

echo ""
echo "Summary statistics:"
docker compose exec postgres psql -U scraper_user -d facebook_scraper -c "
SELECT 
    COUNT(*) as total_posts,
    ROUND(AVG(likes), 2) as avg_likes,
    MAX(likes) as max_likes,
    COUNT(DISTINCT group_name) as groups_scraped
FROM posts 
WHERE likes >= 1000 
    AND scraped_at >= NOW() - INTERVAL '5 days';
"

echo ""
echo "Posts by group:"
docker compose exec postgres psql -U scraper_user -d facebook_scraper -c "
SELECT 
    group_name,
    COUNT(*) as post_count,
    ROUND(AVG(likes), 2) as avg_likes
FROM posts 
WHERE likes >= 1000 
    AND scraped_at >= NOW() - INTERVAL '5 days'
GROUP BY group_name
ORDER BY post_count DESC;
"

echo ""
echo "All posts (regardless of filters):"
docker compose exec postgres psql -U scraper_user -d facebook_scraper -c "
SELECT 
    COUNT(*) as total_all_posts,
    ROUND(AVG(likes), 2) as avg_all_likes,
    MAX(likes) as max_all_likes
FROM posts;
"