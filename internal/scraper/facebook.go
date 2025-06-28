package scraper

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net/http"
    "regexp"
    "strconv"
    "strings"
    "time"
    "net/url"

    "github.com/PuerkitoBio/goquery"
    "github.com/sirupsen/logrus"
    "facebook-scraper/internal/database"
    "facebook-scraper/internal/database/models"
    "facebook-scraper/pkg/types"
)

type FacebookScraper struct {
    authManager   *AuthManager
    client        *http.Client
    logger        *logrus.Logger
    db            *database.DB
    rateLimit     time.Duration
    userAgent     string
    baseURL       string
    mobileURL     string
}

type ScrapingStats struct {
    TotalPosts     int `json:"total_posts"`
    SavedPosts     int `json:"saved_posts"`
    SkippedPosts   int `json:"skipped_posts"`
    ErrorPosts     int `json:"error_posts"`
    ProcessingTime time.Duration `json:"processing_time"`
}

func NewFacebookScraper(cookiesFile, userAgent string, rateLimit time.Duration, logger *logrus.Logger, db *database.DB) (*FacebookScraper, error) {
    authManager, err := NewAuthManager(cookiesFile, userAgent, logger)
    if err != nil {
        return nil, fmt.Errorf("failed to create auth manager: %w", err)
    }

    return &FacebookScraper{
        authManager: authManager,
        client:      authManager.GetAuthenticatedClient(),
        logger:      logger,
        db:          db,
        rateLimit:   rateLimit,
        userAgent:   userAgent,
        baseURL:     "https://www.facebook.com",
        mobileURL:   "https://m.facebook.com",
    }, nil
}

func (fs *FacebookScraper) Initialize() error {
    fs.logger.Info("Initializing Facebook scraper...")

    // Load and validate cookies
    if err := fs.authManager.LoadCookies(); err != nil {
        return fmt.Errorf("failed to load cookies: %w", err)
    }

    // Validate authentication
    if err := fs.authManager.ValidateAuth(); err != nil {
        return fmt.Errorf("authentication validation failed: %w", err)
    }

    fs.logger.Info("Facebook scraper initialized successfully")
    return nil
}

func (fs *FacebookScraper) ScrapeGroup(groupID string) error {
    startTime := time.Now()
    stats := &ScrapingStats{}

    fs.logger.Infof("Starting to scrape group: %s", groupID)

    // Try multiple URL strategies for better success rate
    urls := []string{
        fmt.Sprintf("%s/groups/%s", fs.mobileURL, groupID),
        fmt.Sprintf("%s/groups/%s/posts", fs.mobileURL, groupID),
        fmt.Sprintf("%s/groups/%s", fs.baseURL, groupID),
    }

    var posts []types.ScrapedPost
    var lastError error

    for i, url := range urls {
        fs.logger.Infof("Attempting scrape with URL strategy %d: %s", i+1, url)
        
        groupPosts, err := fs.scrapeGroupURL(url, groupID)
        if err != nil {
            fs.logger.Warnf("URL strategy %d failed: %v", i+1, err)
            lastError = err
            continue
        }

        if len(groupPosts) > 0 {
            posts = groupPosts
            fs.logger.Infof("Successfully scraped %d posts using URL strategy %d", len(posts), i+1)
            break
        }
    }

    if len(posts) == 0 {
        return fmt.Errorf("all scraping strategies failed, last error: %v", lastError)
    }

    // Apply filters and save posts
    filter := &types.PostFilter{
        MinLikes: 1000,
        DaysBack: 5,
    }

    filteredPosts, filterStats := fs.applyFilters(posts, filter)
    fs.logger.Infof("Filter results: %s", filterStats.String())

    // Save to database
    for _, post := range filteredPosts {
        dbPost := fs.convertToDBPost(post, groupID)
        if err := fs.db.SavePost(dbPost); err != nil {
            fs.logger.Errorf("Failed to save post %s: %v", post.ID, err)
            stats.ErrorPosts++
        } else {
            stats.SavedPosts++
        }
    }

    stats.TotalPosts = len(posts)
    stats.SkippedPosts = len(posts) - len(filteredPosts)
    stats.ProcessingTime = time.Since(startTime)

    fs.logger.Infof("Scraping completed for group %s: %+v", groupID, stats)
    return nil
}

func (fs *FacebookScraper) scrapeGroupURL(url, groupID string) ([]types.ScrapedPost, error) {
    fs.logger.Debugf("Scraping URL: %s", url)

    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }

    // Set comprehensive headers to mimic real browser
    fs.setRequestHeaders(req)

    resp, err := fs.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to execute request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
    }

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read response body: %w", err)
    }

    // Parse HTML and extract posts
    posts, err := fs.parseGroupPosts(string(body), groupID)
    if err != nil {
        return nil, fmt.Errorf("failed to parse posts: %w", err)
    }

    // Rate limiting
    time.Sleep(fs.rateLimit)

    return posts, nil
}

func (fs *FacebookScraper) parseGroupPosts(html, groupID string) ([]types.ScrapedPost, error) {
    doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
    if err != nil {
        return nil, fmt.Errorf("failed to parse HTML: %w", err)
    }

    var posts []types.ScrapedPost

    // Multiple selectors for different Facebook layouts
    postSelectors := []string{
        "div[data-ft]",                    // Classic mobile posts
        "article[data-ft]",                // Article format posts
        "div[role='article']",             // Semantic article posts
        "div[id*='story']",                // Story format posts
        ".story_body_container",           // Story body containers
        "div[data-testid='story-subtitle']", // New format posts
    }

    for _, selector := range postSelectors {
        doc.Find(selector).Each(func(i int, s *goquery.Selection) {
            post := fs.extractPostData(s, groupID)
            if post.ID != "" && fs.isValidPost(post) {
                posts = append(posts, post)
            }
        })

        if len(posts) > 0 {
            fs.logger.Debugf("Found %d posts using selector: %s", len(posts), selector)
            break
        }
    }

    // If no posts found with standard selectors, try alternative parsing
    if len(posts) == 0 {
        posts = fs.parseAlternativeFormat(doc, groupID)
    }

    fs.logger.Infof("Extracted %d posts from HTML", len(posts))
    return fs.deduplicatePosts(posts), nil
}

func (fs *FacebookScraper) extractPostData(s *goquery.Selection, groupID string) types.ScrapedPost {
    post := types.ScrapedPost{
        GroupID: groupID,
    }

    // Extract post ID
    if dataFt, exists := s.Attr("data-ft"); exists {
        post.ID = fs.extractPostIDFromDataFt(dataFt)
    }
    if post.ID == "" {
        if id, exists := s.Attr("id"); exists {
            post.ID = fs.cleanPostID(id)
        }
    }

    // Extract author information
    post.AuthorName = fs.extractAuthorName(s)
    post.AuthorID = fs.extractAuthorID(s)

    // Extract post content
    post.Content = fs.extractPostContent(s)

    // Extract engagement metrics
    post.LikesCount = fs.extractLikesCount(s)
    post.CommentsCount = fs.extractCommentsCount(s)
    post.SharesCount = fs.extractSharesCount(s)

    // Extract timestamp
    post.PostTime = fs.extractTimestamp(s)

    // Extract media and links
    post.Images = fs.extractImages(s)
    post.Videos = fs.extractVideos(s)
    post.Links = fs.extractLinks(s)
    post.Mentions = fs.extractMentions(post.Content)
    post.Hashtags = fs.extractHashtags(post.Content)

    // Determine post type
    post.PostType = fs.determinePostType(post)
    post.MediaCount = len(post.Images) + len(post.Videos)

    // Generate URL
    post.URL = fs.generatePostURL(post.ID, groupID)

    return post
}

func (fs *FacebookScraper) extractPostIDFromDataFt(dataFt string) string {
    // Parse JSON-like data-ft attribute
    var ftData map[string]interface{}
    if err := json.Unmarshal([]byte(dataFt), &ftData); err == nil {
        if topLevelPostID, ok := ftData["top_level_post_id"].(string); ok {
            return topLevelPostID
        }
        if mfStoryKey, ok := ftData["mf_story_key"].(string); ok {
            return mfStoryKey
        }
    }

    // Fallback: extract using regex
    re := regexp.MustCompile(`"top_level_post_id":"(\d+)"`)
    if matches := re.FindStringSubmatch(dataFt); len(matches) > 1 {
        return matches[1]
    }

    return ""
}

func (fs *FacebookScraper) extractAuthorName(s *goquery.Selection) string {
    // Multiple selectors for author name
    selectors := []string{
        "h3 a",
        ".actor a",
        "[data-hovercard] strong",
        "strong a",
        ".profileLink",
    }

    for _, selector := range selectors {
        if name := s.Find(selector).First().Text(); name != "" {
            return strings.TrimSpace(name)
        }
    }

    return "Unknown Author"
}

func (fs *FacebookScraper) extractAuthorID(s *goquery.Selection) string {
    // Look for profile links
    s.Find("a[href*='/profile.php'], a[href*='/user/']").Each(func(i int, link *goquery.Selection) {
        if href, exists := link.Attr("href"); exists {
            if id := fs.extractUserIDFromURL(href); id != "" {
                return
            }
        }
    })

    return ""
}

func (fs *FacebookScraper) extractPostContent(s *goquery.Selection) string {
    // Multiple selectors for post content
    selectors := []string{
        ".userContent",
        "[data-testid='post_message']",
        ".story_body_container p",
        ".text_exposed_root",
        "p",
    }

    for _, selector := range selectors {
        if content := s.Find(selector).First().Text(); content != "" {
            return strings.TrimSpace(content)
        }
    }

    return ""
}

func (fs *FacebookScraper) extractLikesCount(s *goquery.Selection) int {
    // Look for like counts in various formats
    patterns := []string{
        `(\d+)\s*likes?`,
        `(\d+)\s*reactions?`,
        `(\d+)\s*👍`,
        `(\d+)\s*❤️`,
    }

    text := s.Text()
    for _, pattern := range patterns {
        re := regexp.MustCompile(pattern)
        if matches := re.FindStringSubmatch(text); len(matches) > 1 {
            if count, err := strconv.Atoi(matches[1]); err == nil {
                return count
            }
        }
    }

    // Look for like count in specific elements
    s.Find("a[href*='reaction'], span[data-testid*='like']").Each(func(i int, elem *goquery.Selection) {
        text := elem.Text()
        if count := fs.extractNumberFromText(text); count > 0 {
            return
        }
    })

    return 0
}

func (fs *FacebookScraper) extractCommentsCount(s *goquery.Selection) int {
    patterns := []string{
        `(\d+)\s*comments?`,
        `(\d+)\s*replies?`,
        `(\d+)\s*💬`,
    }

    text := s.Text()
    for _, pattern := range patterns {
        re := regexp.MustCompile(pattern)
        if matches := re.FindStringSubmatch(text); len(matches) > 1 {
            if count, err := strconv.Atoi(matches[1]); err == nil {
                return count
            }
        }
    }

    return 0
}

func (fs *FacebookScraper) extractSharesCount(s *goquery.Selection) int {
    patterns := []string{
        `(\d+)\s*shares?`,
        `(\d+)\s*shared`,
        `(\d+)\s*🔄`,
    }

    text := s.Text()
    for _, pattern := range patterns {
        re := regexp.MustCompile(pattern)
        if matches := re.FindStringSubmatch(text); len(matches) > 1 {
            if count, err := strconv.Atoi(matches[1]); err == nil {
                return count
            }
        }
    }

    return 0
}

func (fs *FacebookScraper) extractTimestamp(s *goquery.Selection) time.Time {
    // Look for timestamp in various formats
    s.Find("abbr[data-utime], time, [data-testid='story-subtitle'] a").Each(func(i int, elem *goquery.Selection) {
        // Unix timestamp
        if utime, exists := elem.Attr("data-utime"); exists {
            if timestamp, err := strconv.ParseInt(utime, 10, 64); err == nil {
                return
            }
        }

        // ISO datetime
        if datetime, exists := elem.Attr("datetime"); exists {
            if t, err := time.Parse(time.RFC3339, datetime); err == nil {
                return
            }
        }

        // Relative time text
        text := elem.Text()
        if t := fs.parseRelativeTime(text); !t.IsZero() {
            return
        }
    })

    return time.Now() // Fallback to current time
}

func (fs *FacebookScraper) extractImages(s *goquery.Selection) []types.MediaItem {
    var images []types.MediaItem

    s.Find("img").Each(func(i int, img *goquery.Selection) {
        if src, exists := img.Attr("src"); exists && fs.isValidImageURL(src) {
            alt, _ := img.Attr("alt")
            images = append(images, types.MediaItem{
                URL:         src,
                Type:        "image",
                Description: alt,
            })
        }
    })

    return images
}

func (fs *FacebookScraper) extractVideos(s *goquery.Selection) []types.MediaItem {
    var videos []types.MediaItem

    s.Find("video, [data-testid='video']").Each(func(i int, video *goquery.Selection) {
        if src, exists := video.Attr("src"); exists {
            videos = append(videos, types.MediaItem{
                URL:  src,
                Type: "video",
            })
        }
    })

    return videos
}

func (fs *FacebookScraper) extractLinks(s *goquery.Selection) []string {
    var links []string
    linkMap := make(map[string]bool)

    s.Find("a[href]").Each(func(i int, link *goquery.Selection) {
        if href, exists := link.Attr("href"); exists {
            cleanURL := fs.cleanURL(href)
            if cleanURL != "" && !linkMap[cleanURL] {
                links = append(links, cleanURL)
                linkMap[cleanURL] = true
            }
        }
    })

    return links
}

func (fs *FacebookScraper) extractMentions(content string) []string {
    re := regexp.MustCompile(`@([a-zA-Z0-9._]+)`)
    matches := re.FindAllStringSubmatch(content, -1)
    
    var mentions []string
    for _, match := range matches {
        if len(match) > 1 {
            mentions = append(mentions, match[1])
        }
    }
    
    return mentions
}

func (fs *FacebookScraper) extractHashtags(content string) []string {
    re := regexp.MustCompile(`#([a-zA-Z0-9_]+)`)
    matches := re.FindAllStringSubmatch(content, -1)
    
    var hashtags []string
    for _, match := range matches {
        if len(match) > 1 {
            hashtags = append(hashtags, match[1])
        }
    }
    
    return hashtags
}

func (fs *FacebookScraper) determinePostType(post types.ScrapedPost) string {
    if len(post.Videos) > 0 {
        return "video"
    }
    if len(post.Images) > 0 {
        return "image"
    }
    if len(post.Links) > 0 {
        return "link"
    }
    if len(post.Images) > 0 && len(post.Videos) > 0 {
        return "mixed"
    }
    return "text"
}

// Helper functions
func (fs *FacebookScraper) setRequestHeaders(req *http.Request) {
    req.Header.Set("User-Agent", fs.userAgent)
    req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
    req.Header.Set("Accept-Language", "en-US,en;q=0.5")
    req.Header.Set("Accept-Encoding", "gzip, deflate, br")
    req.Header.Set("DNT", "1")
    req.Header.Set("Connection", "keep-alive")
    req.Header.Set("Upgrade-Insecure-Requests", "1")
    req.Header.Set("Sec-Fetch-Dest", "document")
    req.Header.Set("Sec-Fetch-Mode", "navigate")
    req.Header.Set("Sec-Fetch-Site", "none")
    req.Header.Set("Cache-Control", "max-age=0")
}

func (fs *FacebookScraper) cleanPostID(id string) string {
    // Remove common prefixes and clean the ID
    id = strings.TrimPrefix(id, "story_")
    id = strings.TrimPrefix(id, "post_")
    id = strings.TrimPrefix(id, "hyperfeed_story_id_")
    
    // Extract numeric part
    re := regexp.MustCompile(`\d+`)
    if match := re.FindString(id); match != "" {
        return match
    }
    
    return id
}

func (fs *FacebookScraper) extractUserIDFromURL(href string) string {
    // Extract user ID from various URL formats
    patterns := []string{
        `profile\.php\?id=(\d+)`,
        `/user/(\d+)`,
        `/profile/(\d+)`,
    }

    for _, pattern := range patterns {
        re := regexp.MustCompile(pattern)
        if matches := re.FindStringSubmatch(href); len(matches) > 1 {
            return matches[1]
        }
    }

    return ""
}

func (fs *FacebookScraper) extractNumberFromText(text string) int {
    re := regexp.MustCompile(`\d+`)
    if match := re.FindString(text); match != "" {
        if num, err := strconv.Atoi(match); err == nil {
            return num
        }
    }
    return 0
}

func (fs *FacebookScraper) parseRelativeTime(text string) time.Time {
    now := time.Now()
    text = strings.ToLower(strings.TrimSpace(text))

    // Handle various relative time formats
    patterns := map[string]time.Duration{
        `(\d+)\s*min`:   time.Minute,
        `(\d+)\s*hour`:  time.Hour,
        `(\d+)\s*day`:   24 * time.Hour,
        `(\d+)\s*week`:  7 * 24 * time.Hour,
        `(\d+)\s*month`: 30 * 24 * time.Hour,
    }

    for pattern, unit := range patterns {
        re := regexp.MustCompile(pattern)
        if matches := re.FindStringSubmatch(text); len(matches) > 1 {
            if num, err := strconv.Atoi(matches[1]); err == nil {
                return now.Add(-time.Duration(num) * unit)
            }
        }
    }

    return time.Time{}
}

func (fs *FacebookScraper) isValidImageURL(url string) bool {
    return strings.Contains(url, "facebook.com") && 
           (strings.Contains(url, ".jpg") || strings.Contains(url, ".png") || strings.Contains(url, ".gif"))
}

func (fs *FacebookScraper) cleanURL(href string) string {
    // Remove Facebook tracking parameters
    if u, err := url.Parse(href); err == nil {
        // Remove common tracking parameters
        q := u.Query()
        q.Del("fbclid")
        q.Del("__tn__")
        q.Del("__cft__")
        u.RawQuery = q.Encode()
        return u.String()
    }
    return href
}

func (fs *FacebookScraper) generatePostURL(postID, groupID string) string {
    if postID != "" {
        return fmt.Sprintf("%s/groups/%s/posts/%s", fs.baseURL, groupID, postID)
    }
    return fmt.Sprintf("%s/groups/%s", fs.baseURL, groupID)
}

func (fs *FacebookScraper) isValidPost(post types.ScrapedPost) bool {
    return post.ID != "" && 
           (post.Content != "" || len(post.Images) > 0 || len(post.Videos) > 0) &&
           post.AuthorName != ""
}

func (fs *FacebookScraper) deduplicatePosts(posts []types.ScrapedPost) []types.ScrapedPost {
    seen := make(map[string]bool)
    var unique []types.ScrapedPost

    for _, post := range posts {
        if !seen[post.ID] {
            seen[post.ID] = true
            unique = append(unique, post)
        }
    }

    return unique
}

func (fs *FacebookScraper) parseAlternativeFormat(doc *goquery.Document, groupID string) []types.ScrapedPost {
    var posts []types.ScrapedPost

    // Try to extract from script tags containing JSON data
    doc.Find("script").Each(func(i int, script *goquery.Selection) {
        content := script.Text()
        if strings.Contains(content, "story") && strings.Contains(content, "feedback") {
            // This might contain post data in JSON format
            // Implementation would depend on the specific JSON structure
            fs.logger.Debug("Found potential JSON post data in script tag")
        }
    })

    return posts
}

func (fs *FacebookScraper) applyFilters(posts []types.ScrapedPost, filter *types.PostFilter) ([]types.ScrapedPost, types.FilterStats) {
    var filtered []types.ScrapedPost
    stats := types.FilterStats{
        TotalPosts: len(posts),
    }

    cutoffTime := time.Now().AddDate(0, 0, -filter.DaysBack)

    for _, post := range posts {
        passedLikes := filter.MinLikes == 0 || post.LikesCount >= filter.MinLikes
        passedTime := filter.DaysBack == 0 || post.PostTime.After(cutoffTime)

        if !passedLikes {
            stats.LikesFiltered++
        }
        if !passedTime {
            stats.TimeFiltered++
        }

        if passedLikes && passedTime {
            filtered = append(filtered, post)
        }
    }

    stats.FilteredPosts = len(filtered)
    return filtered, stats
}

func (fs *FacebookScraper) convertToDBPost(post types.ScrapedPost, groupID string) *models.Post {
    // Convert images and videos to JSON strings
    imagesJSON, _ := json.Marshal(post.Images)
    videosJSON, _ := json.Marshal(post.Videos)

    return &models.Post{
        GroupID:     groupID,
        GroupName:   fs.getGroupName(groupID),
        PostID:      post.ID,
        AuthorID:    post.AuthorID,
        AuthorName:  post.AuthorName,
        Content:     post.Content,
        PostURL:     post.URL,
        Timestamp:   post.PostTime,
        Likes:       post.LikesCount,
        Comments:    post.CommentsCount,
        Shares:      post.SharesCount,
        PostType:    post.PostType,
        ScrapedAt:   time.Now(),
        Images:      string(imagesJSON),
        Videos:      string(videosJSON),
        Mentions:    post.Mentions,
        Hashtags:    post.Hashtags,
        Links:       post.Links,
        MediaCount:  post.MediaCount,
    }
}

func (fs *FacebookScraper) getGroupName(groupID string) string {
    // This could be enhanced to fetch actual group names
    // For now, return a placeholder
    return fmt.Sprintf("Group_%s", groupID)
}

func (fs *FacebookScraper) Close() error {
    if fs.authManager != nil {
        return fs.authManager.SaveCookies()
    }
    return nil
}