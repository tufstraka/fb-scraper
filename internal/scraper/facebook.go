// internal/scraper/facebook.go
package scraper

import (
    "fmt"
    "net/http"
    "regexp"
    "strconv"
    "strings"
    "time"

    "facebook-scraper/internal/database"
    "facebook-scraper/internal/database/models"
    "facebook-scraper/pkg/types"
    
    "github.com/PuerkitoBio/goquery"
    "github.com/sirupsen/logrus"
)

type FacebookScraper struct {
    authManager *AuthManager
    client      *http.Client
    db          *database.DB
    filter      *types.PostFilter
    logger      *logrus.Logger
    userAgent   string
    rateLimit   time.Duration
}

func NewFacebookScraper(cookiesFile, userAgent string, rateLimit time.Duration, logger *logrus.Logger, db *database.DB) (*FacebookScraper, error) {
    authManager, err := NewAuthManager(cookiesFile, userAgent, logger)
    if err != nil {
        return nil, fmt.Errorf("failed to create auth manager: %w", err)
    }

    // Create filter for posts with 1000+ likes in past 5 days
    filter := &types.PostFilter{
        MinLikes:        50,
        MaxLikes:        0, // No upper limit
        MinComments:     0,
        MinShares:       0,
        DaysBack:        5,
        Keywords:        []string{},
        ExcludeKeywords: []string{},
        GroupIDs:        []string{},
        PageIDs:         []string{},
        AuthorNames:     []string{},
    }

    return &FacebookScraper{
        authManager: authManager,
        db:          db,
        filter:      filter,
        logger:      logger,
        userAgent:   userAgent,
        rateLimit:   rateLimit,
    }, nil
}

func (fs *FacebookScraper) Initialize() error {
    fs.logger.Info("Initializing Facebook scraper...")

    // Load cookies from file
    if err := fs.authManager.LoadCookies(); err != nil {
        return fmt.Errorf("failed to load cookies: %w", err)
    }

    // Validate authentication
    if err := fs.authManager.ValidateAuth(); err != nil {
        return fmt.Errorf("authentication validation failed: %w", err)
    }

    // Get authenticated client
    fs.client = fs.authManager.GetAuthenticatedClient()

    fs.logger.Info("Facebook scraper initialized successfully")
    return nil
}

func (fs *FacebookScraper) ScrapeGroup(groupID string) error {
    fs.logger.Infof("Starting to scrape group: %s", groupID)

    // Get group name first
    groupName, err := fs.getGroupName(groupID)
    if err != nil {
        fs.logger.Warnf("Failed to get group name for %s: %v", groupID, err)
        groupName = fmt.Sprintf("Group_%s", groupID)
    }

    // Scrape multiple pages to get more posts
    allPosts := []types.ScrapedPost{}
    
    // Try different URL patterns to get posts
    urls := []string{
        fmt.Sprintf("https://m.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://m.facebook.com/groups/%s/posts", groupID),
        fmt.Sprintf("https://www.facebook.com/groups/%s", groupID),
    }

    for _, groupURL := range urls {
        fs.logger.Infof("Scraping URL: %s", groupURL)
        
        req, err := http.NewRequest("GET", groupURL, nil)
        if err != nil {
            fs.logger.Errorf("Failed to create request for %s: %v", groupURL, err)
            continue
        }

        fs.setHeaders(req)

        resp, err := fs.client.Do(req)
        if err != nil {
            fs.logger.Errorf("Failed to fetch %s: %v", groupURL, err)
            continue
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
            fs.logger.Warnf("Unexpected status code %d for %s", resp.StatusCode, groupURL)
            continue
        }

        // Parse the response
        doc, err := goquery.NewDocumentFromReader(resp.Body)
        if err != nil {
            fs.logger.Errorf("Failed to parse HTML from %s: %v", groupURL, err)
            continue
        }

        // Extract posts
        posts := fs.extractPosts(doc, groupID, groupName)
        allPosts = append(allPosts, posts...)

        // Rate limiting between requests
        time.Sleep(fs.rateLimit)
    }

    // Filter and save posts
    fs.logger.Infof("Found %d total posts, applying filters...", len(allPosts))
    
    filteredPosts, stats := BatchFilter(allPosts, fs.filter)
    
    fs.logger.Infof("Filter results: %s", stats.String())

    // Save filtered posts to database
    savedCount := 0
    for _, post := range filteredPosts {
        if err := fs.saveScrapedPost(&post, groupID, groupName); err != nil {
            fs.logger.Errorf("Failed to save post %s: %v", post.ID, err)
        } else {
            savedCount++
        }
    }

    fs.logger.Infof("Successfully saved %d posts from group %s", savedCount, groupName)
    return nil
}

func (fs *FacebookScraper) getGroupName(groupID string) (string, error) {
    req, err := http.NewRequest("GET", fmt.Sprintf("https://m.facebook.com/groups/%s/about", groupID), nil)
    if err != nil {
        return "", err
    }

    fs.setHeaders(req)
    resp, err := fs.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    doc, err := goquery.NewDocumentFromReader(resp.Body)
    if err != nil {
        return "", err
    }

    // Try to find group name
    name := doc.Find("title").Text()
    if name != "" {
        return strings.TrimSpace(strings.Split(name, "|")[0]), nil
    }

    return "", fmt.Errorf("group name not found")
}

func (fs *FacebookScraper) extractPosts(doc *goquery.Document, groupID, groupName string) []types.ScrapedPost {
    var posts []types.ScrapedPost
    
    fs.logger.Debugf("Extracting posts from group: %s", groupName)
    
    // Multiple selectors to find posts (Facebook changes structure frequently)
    postSelectors := []string{
        "div[data-ft]",                    // Main post containers
        "div[role='article']",             // Article posts
        "div[data-testid='story-root']",   // Story containers
        "div[data-pagelet^='FeedUnit']",   // Feed units
        "div._4-u2",                       // Mobile post containers
        "div._5pcr",                       // Another mobile pattern
    }
    
    for _, selector := range postSelectors {
        doc.Find(selector).Each(func(i int, s *goquery.Selection) {
            post := fs.extractSinglePost(s, groupID, groupName, i)
            if post != nil {
                posts = append(posts, *post)
            }
        })
    }

    fs.logger.Infof("Extracted %d raw posts from group %s", len(posts), groupName)
    return posts
}

func (fs *FacebookScraper) extractSinglePost(s *goquery.Selection, groupID, groupName string, index int) *types.ScrapedPost {
    post := &types.ScrapedPost{
        GroupID: groupID,
        PostTime: time.Now(), // Default to now, will try to extract actual time
    }

    // Generate unique post ID
    if dataFt, exists := s.Attr("data-ft"); exists {
        post.ID = fmt.Sprintf("%s_%s_%d", groupID, dataFt, index)
    } else {
        post.ID = fmt.Sprintf("%s_%d_%d", groupID, time.Now().Unix(), index)
    }

    // Extract content using multiple selectors
    contentSelectors := []string{
        "[data-testid='post_message']",
        ".userContent",
        "._5pbx",
        "p",
        ".text_exposed_root",
        "span[dir]",
    }
    
    for _, selector := range contentSelectors {
        if content := s.Find(selector).First().Text(); content != "" {
            post.Content = strings.TrimSpace(content)
            break
        }
    }

    // Extract author name
    authorSelectors := []string{
        "strong a",
        "h3 a",
        "[data-testid='story-subtitle'] a",
        "._6qw4",
        ".profileLink",
    }
    
    for _, selector := range authorSelectors {
        if authorElement := s.Find(selector).First(); authorElement.Length() > 0 {
            post.AuthorName = strings.TrimSpace(authorElement.Text())
            break
        }
    }

    // Extract likes, comments, shares
    post.LikesCount = fs.extractEngagementCount(s, []string{
        "[aria-label*='reaction']",
        "[aria-label*='like']", 
        "[aria-label*='love']",
        "._81hb",
        "._4arz",
    })

    post.CommentsCount = fs.extractEngagementCount(s, []string{
        "[aria-label*='comment']",
        "._3hg-",
        "._1whp",
    })

    post.SharesCount = fs.extractEngagementCount(s, []string{
        "[aria-label*='share']",
        "._355t",
        "._15ko",
    })

    // Try to extract post timestamp
    timeSelectors := []string{
        "abbr[data-utime]",
        "[data-testid='story-subtitle'] abbr",
        "._5ptz",
        ".timestampContent",
    }

    for _, selector := range timeSelectors {
        if timeElement := s.Find(selector).First(); timeElement.Length() > 0 {
            if utime, exists := timeElement.Attr("data-utime"); exists {
                if timestamp, err := strconv.ParseInt(utime, 10, 64); err == nil {
                    post.PostTime = time.Unix(timestamp, 0)
                    break
                }
            }
            
            // Try parsing time text
            timeText := timeElement.Text()
            if parsedTime := fs.parseTimeText(timeText); !parsedTime.IsZero() {
                post.PostTime = parsedTime
                break
            }
        }
    }

    // Generate post URL
    post.URL = fmt.Sprintf("https://www.facebook.com/groups/%s/posts/%s", groupID, post.ID)

    // Only return posts with content and some engagement
    if post.Content != "" || post.LikesCount > 0 {
        fs.logger.Debugf("Extracted post: likes=%d, comments=%d, shares=%d, content=%s", 
            post.LikesCount, post.CommentsCount, post.SharesCount, 
            truncateString(post.Content, 50))
        return post
    }

    return nil
}

func (fs *FacebookScraper) extractEngagementCount(s *goquery.Selection, selectors []string) int {
    for _, selector := range selectors {
        foundCount := 0
        s.Find(selector).Each(func(i int, element *goquery.Selection) {
            if foundCount > 0 {
                return // Already found a count, skip
            }
            
            text := element.Text()
            if text != "" {
                // Extract numbers from text like "1.2K", "150", "2.5M"
                if count := fs.parseCountText(text); count > 0 {
                    foundCount = count
                    return
                }
            }
            
            // Check aria-label
            if ariaLabel, exists := element.Attr("aria-label"); exists {
                if count := fs.parseCountText(ariaLabel); count > 0 {
                    foundCount = count
                    return
                }
            }
        })
        
        if foundCount > 0 {
            return foundCount
        }
    }
    return 0
}

func (fs *FacebookScraper) parseCountText(text string) int {
    // Handle formats like "1.2K", "2.5M", "150", etc.
    re := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([KMB]?)`)
    matches := re.FindStringSubmatch(text)
    
    if len(matches) >= 2 {
        num, err := strconv.ParseFloat(matches[1], 64)
        if err != nil {
            return 0
        }
        
        multiplier := 1.0
        if len(matches) > 2 {
            switch strings.ToUpper(matches[2]) {
            case "K":
                multiplier = 1000
            case "M":
                multiplier = 1000000
            case "B":
                multiplier = 1000000000
            }
        }
        
        return int(num * multiplier)
    }
    
    return 0
}

func (fs *FacebookScraper) parseTimeText(timeText string) time.Time {
    timeText = strings.ToLower(strings.TrimSpace(timeText))
    now := time.Now()
    
    // Handle relative times
    if strings.Contains(timeText, "min") {
        re := regexp.MustCompile(`(\d+)\s*min`)
        if matches := re.FindStringSubmatch(timeText); len(matches) > 1 {
            if mins, err := strconv.Atoi(matches[1]); err == nil {
                return now.Add(-time.Duration(mins) * time.Minute)
            }
        }
    }
    
    if strings.Contains(timeText, "hour") || strings.Contains(timeText, "hr") {
        re := regexp.MustCompile(`(\d+)\s*h`)
        if matches := re.FindStringSubmatch(timeText); len(matches) > 1 {
            if hours, err := strconv.Atoi(matches[1]); err == nil {
                return now.Add(-time.Duration(hours) * time.Hour)
            }
        }
    }
    
    if strings.Contains(timeText, "day") {
        re := regexp.MustCompile(`(\d+)\s*d`)
        if matches := re.FindStringSubmatch(timeText); len(matches) > 1 {
            if days, err := strconv.Atoi(matches[1]); err == nil {
                return now.AddDate(0, 0, -days)
            }
        }
    }
    
    // For "yesterday"
    if strings.Contains(timeText, "yesterday") {
        return now.AddDate(0, 0, -1)
    }
    
    return time.Time{} // Return zero time if parsing fails
}

func (fs *FacebookScraper) saveScrapedPost(post *types.ScrapedPost, groupID, groupName string) error {
    dbPost := &models.Post{
        GroupID:     groupID,
        GroupName:   groupName,
        PostID:      post.ID,
        AuthorName:  post.AuthorName,
        Content:     post.Content,
        PostURL:     post.URL,
        Timestamp:   post.PostTime,
        Likes:       post.LikesCount,
        Comments:    post.CommentsCount,
        Shares:      post.SharesCount,
        PostType:    "text",
        ScrapedAt:   time.Now(),
    }

    return fs.db.SavePost(dbPost)
}

func (fs *FacebookScraper) setHeaders(req *http.Request) {
    req.Header.Set("User-Agent", fs.userAgent)
    req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
    req.Header.Set("Accept-Language", "en-US,en;q=0.5")
    req.Header.Set("Accept-Encoding", "gzip, deflate, br")
    req.Header.Set("DNT", "1")
    req.Header.Set("Connection", "keep-alive")
    req.Header.Set("Upgrade-Insecure-Requests", "1")
    req.Header.Set("Sec-Fetch-Dest", "document")
    req.Header.Set("Sec-Fetch-Mode", "navigate")
    req.Header.Set("Sec-Fetch-Site", "none")
}

func (fs *FacebookScraper) Close() error {
    // Save updated cookies
    if err := fs.authManager.SaveCookies(); err != nil {
        fs.logger.Warnf("Failed to save cookies: %v", err)
    }
    return nil
}

func truncateString(s string, length int) string {
    if len(s) <= length {
        return s
    }
    return s[:length] + "..."
}