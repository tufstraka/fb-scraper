-- Create extension for UUID generation (optional)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create the posts table
CREATE TABLE IF NOT EXISTS posts (
    id BIGSERIAL PRIMARY KEY,
    group_id VARCHAR(255) NOT NULL,
    group_name VARCHAR(500) NOT NULL,
    post_id VARCHAR(255) UNIQUE NOT NULL,
    author_id VARCHAR(255),
    author_name VARCHAR(255),
    content TEXT,
    post_url TEXT,
    timestamp TIMESTAMP WITH TIME ZONE,
    likes INTEGER DEFAULT 0,
    comments INTEGER DEFAULT 0,
    shares INTEGER DEFAULT 0,
    images JSONB DEFAULT '[]'::jsonb,
    videos JSONB DEFAULT '[]'::jsonb,
    links JSONB DEFAULT '[]'::jsonb,
    hashtags JSONB DEFAULT '[]'::jsonb,
    mentions JSONB DEFAULT '[]'::jsonb,
    post_type VARCHAR(50) DEFAULT 'text',
    scraped_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for better performance
CREATE INDEX IF NOT EXISTS idx_posts_group_id ON posts(group_id);
CREATE INDEX IF NOT EXISTS idx_posts_author_id ON posts(author_id);
CREATE INDEX IF NOT EXISTS idx_posts_timestamp ON posts(timestamp);
CREATE INDEX IF NOT EXISTS idx_posts_scraped_at ON posts(scraped_at);
CREATE INDEX IF NOT EXISTS idx_posts_likes ON posts(likes);
CREATE INDEX IF NOT EXISTS idx_posts_post_type ON posts(post_type);

-- Create trigger function for updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create trigger
DROP TRIGGER IF EXISTS update_posts_updated_at ON posts;
CREATE TRIGGER update_posts_updated_at 
    BEFORE UPDATE ON posts 
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Create useful views for high engagement posts (1000+ likes in past 5 days)
CREATE OR REPLACE VIEW high_engagement_posts AS
SELECT 
    id,
    group_name,
    author_name,
    LEFT(content, 150) as content_preview,
    likes,
    comments,
    shares,
    post_type,
    timestamp,
    scraped_at
FROM posts 
WHERE likes >= 50 
    AND scraped_at >= NOW() - INTERVAL '5 days'
ORDER BY likes DESC, scraped_at DESC;


-- Grant permissions to scraper_user
GRANT ALL PRIVILEGES ON TABLE posts TO scraper_user;
GRANT ALL PRIVILEGES ON SEQUENCE posts_id_seq TO scraper_user;
GRANT SELECT ON high_engagement_posts TO scraper_user;