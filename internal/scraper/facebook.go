// internal/scraper/facebook.go
package scraper

import (
    "compress/flate"
    "compress/gzip"
    "encoding/json"
    "fmt"
    "net/http"
    "regexp"
    "strconv"
    "strings"
    "time"
    "os"
    "io"

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
    
    // First, look for the feed container
    feedContainer := doc.Find("div[role='feed']").First()
    if feedContainer.Length() == 0 {
        fs.logger.Debug("No feed container found, trying fallback selectors")
        // Fallback to original extraction method if no feed found
        return fs.extractPostsFallback(doc, groupID, groupName)
    }
    
    fs.logger.Debugf("Found feed container, looking for posts...")
    
    // Look for post containers within the feed
    // Posts appear to be in divs with aria-posinset attribute within the feed
    postContainers := feedContainer.Find("div[aria-posinset]")
    
    fs.logger.Debugf("Found %d potential post containers", postContainers.Length())
    
    postContainers.Each(func(i int, postContainer *goquery.Selection) {
        post := fs.extractSinglePostFromFeed(postContainer, groupID, groupName)
        if post != nil {
            posts = append(posts, *post)
        }
    })
    
    // If no posts found with aria-posinset, try alternative selectors within feed
    if len(posts) == 0 {
        fs.logger.Debug("No posts found with aria-posinset, trying alternative selectors within feed")
        
        // Try looking for direct children that might be posts
        feedContainer.Children().Each(func(i int, child *goquery.Selection) {
            // Skip if this doesn't look like a post container
            if !fs.looksLikePostContainer(child) {
                return
            }
            
            post := fs.extractSinglePostFromFeed(child, groupID, groupName)
            if post != nil {
                posts = append(posts, *post)
            }
        })
    }
    
    fs.logger.Infof("Extracted %d posts from feed in group %s", len(posts), groupName)
    return posts
}

func (fs *FacebookScraper) looksLikePostContainer(s *goquery.Selection) bool {
    // Check for common post container indicators
    hasDataTestId := false
    if testId, exists := s.Attr("data-testid"); exists {
        hasDataTestId = strings.Contains(testId, "post") || strings.Contains(testId, "story")
    }
    
    // Check for nested author information
    hasAuthor := s.Find("h2, h3, strong").Length() > 0
    
    // Check for engagement elements
    hasEngagement := s.Find("[aria-label*='Like'], [aria-label*='Comment'], [aria-label*='Share']").Length() > 0
    
    // Check for post content areas
    hasContent := s.Find("[data-ad-rendering-role], [dir='auto']").Length() > 0
    
    return hasDataTestId || (hasAuthor && (hasEngagement || hasContent))
}

func (fs *FacebookScraper) extractSinglePostFromFeed(s *goquery.Selection, groupID, groupName string) *types.ScrapedPost {
    post := &types.ScrapedPost{
        GroupID:   groupID,
        PostTime:  time.Now(), // Default to current time
    }
    
    // Extract post ID from various possible locations
    post.ID = fs.extractPostID(s, groupID)
    
    // Extract author name - look for h2 with author links
    post.AuthorName = fs.extractAuthorFromFeed(s)
    
    // Extract post content - look for data-ad-rendering-role="story_message"
    post.Content = fs.extractContentFromFeed(s)
    
    // Extract post URL
    post.URL = fs.extractPostURL(s, groupID, post.ID)
    
    // Extract timestamp
    fs.extractTimestampFromFeed(s, post)
    
    // Extract engagement metrics
    fs.extractEngagementFromFeed(s, post)
    
    // Only return posts with sufficient information
    if post.Content != "" || post.AuthorName != "" || post.LikesCount > 0 {
        fs.logger.Debugf("Found post: ID=%s, Author=%s, Likes=%d, Comments=%d, Content=%s",
            post.ID, post.AuthorName, post.LikesCount, post.CommentsCount, 
            truncateString(post.Content, 50))
        return post
    }
    
    return nil
}

func (fs *FacebookScraper) extractPostID(s *goquery.Selection, groupID string) string {
    // Method 1: Look for aria-describedby attribute which might contain post references
    if describedBy, exists := s.Attr("aria-describedby"); exists {
        // Extract IDs from aria-describedby
        ids := strings.Split(describedBy, " ")
        for _, id := range ids {
            if strings.Contains(id, "_r_") && len(id) > 5 {
                return id
            }
        }
    }
    
    // Method 2: Look for href attributes that contain post IDs
    var postID string
    s.Find("a[href*='/photo/'], a[href*='/posts/'], a[href*='fbid=']").Each(func(i int, link *goquery.Selection) {
        href, exists := link.Attr("href")
        if !exists {
            return
        }
        
        // Extract from fbid parameter
        re := regexp.MustCompile(`fbid=(\d+)`)
        if matches := re.FindStringSubmatch(href); len(matches) > 1 {
            postID = matches[1]
            return
        }
        
        // Extract from posts URL
        re = regexp.MustCompile(`/posts/(\d+)`)
        if matches := re.FindStringSubmatch(href); len(matches) > 1 {
            postID = matches[1]
            return
        }
    })
    
    if postID != "" {
        return postID
    }
    
    // Method 3: Generate from aria-posinset or other unique identifier
    if posinset, exists := s.Attr("aria-posinset"); exists {
        return fmt.Sprintf("%s_pos_%s", groupID, posinset)
    }
    
    // Fallback: Generate a pseudo-random ID
    return fmt.Sprintf("%s_%d", groupID, time.Now().UnixNano())
}

func (fs *FacebookScraper) extractAuthorFromFeed(s *goquery.Selection) string {
    // Look for h2 elements that typically contain author names
    authorSelectors := []string{
        "h2 a strong span", // Based on the HTML structure provided
        "h2 strong span",
        "h2 a strong",
        "h2 strong",
        "[data-ad-rendering-role='profile_name'] h2 span",
        "[data-ad-rendering-role='profile_name'] span",
    }
    
    for _, selector := range authorSelectors {
        author := s.Find(selector).First()
        if author.Length() > 0 {
            name := strings.TrimSpace(author.Text())
            if name != "" && len(name) < 100 && !strings.Contains(strings.ToLower(name), "follow") {
                return name
            }
        }
    }
    
    return ""
}

func (fs *FacebookScraper) extractContentFromFeed(s *goquery.Selection) string {
    // Look for the story message container
    contentSelectors := []string{
        "[data-ad-rendering-role='story_message']",
        "[data-ad-rendering-role='message']",
        "[data-ad-preview='message']",
        "div[dir='auto'][style*='text-align: start']",
    }
    
    for _, selector := range contentSelectors {
        contentContainer := s.Find(selector).First()
        if contentContainer.Length() > 0 {
            // Remove any nested emoji images and get clean text
            contentContainer.Find("img").Remove()
            text := strings.TrimSpace(contentContainer.Text())
            if text != "" {
                return text
            }
        }
    }
    
    return ""
}

func (fs *FacebookScraper) extractPostURL(s *goquery.Selection, groupID, postID string) string {
    // Look for photo links or post links
    var postURL string
    s.Find("a[href*='/photo/'], a[href*='/posts/']").Each(func(i int, link *goquery.Selection) {
        href, exists := link.Attr("href")
        if exists && strings.Contains(href, "facebook.com") {
            postURL = href
            return
        }
    })
    
    if postURL != "" {
        return postURL
    }
    
    // Fallback: construct URL
    return fmt.Sprintf("https://www.facebook.com/groups/%s/posts/%s", groupID, postID)
}

func (fs *FacebookScraper) extractTimestampFromFeed(s *goquery.Selection, post *types.ScrapedPost) {
    // Look for timestamp in various locations
    timestampSelectors := []string{
        "a[href*='?'] span", // Timestamp links
        "span[data-utime]",
        "abbr[data-utime]",
        "time",
    }
    
    for _, selector := range timestampSelectors {
        timestamp := s.Find(selector).First()
        if timestamp.Length() > 0 {
            // Check for data-utime attribute
            if utime, exists := timestamp.Attr("data-utime"); exists {
                if ts, err := strconv.ParseInt(utime, 10, 64); err == nil {
                    post.PostTime = time.Unix(ts, 0)
                    return
                }
            }
            
            // Parse relative time text
            timeText := timestamp.Text()
            parsedTime := fs.parseTimeText(timeText)
            if !parsedTime.IsZero() {
                post.PostTime = parsedTime
                return
            }
        }
    }
}

func (fs *FacebookScraper) extractEngagementFromFeed(s *goquery.Selection, post *types.ScrapedPost) {
    // Look for engagement section
    engagementSection := s.Find("[aria-label*='reaction'], [aria-label*='All reactions']").First()
    if engagementSection.Length() > 0 {
        // Extract total reactions
        reactionText := engagementSection.Text()
        if count := fs.parseCountText(reactionText); count > 0 {
            post.LikesCount = count
        }
    }
    
    // Look for specific reaction counts in spans
    s.Find("span").Each(func(i int, span *goquery.Selection) {
        text := span.Text()
        if strings.Contains(text, "Like") || strings.Contains(text, "reaction") {
            if count := fs.parseCountText(text); count > 0 && post.LikesCount == 0 {
                post.LikesCount = count
            }
        }
    })
    
    // Look for comment and share buttons/counts
    s.Find("[aria-label*='comment'], [aria-label*='Comment']").Each(func(i int, element *goquery.Selection) {
        if label, exists := element.Attr("aria-label"); exists {
            if count := fs.parseCountText(label); count > 0 {
                post.CommentsCount = count
            }
        }
    })
    
    s.Find("[aria-label*='share'], [aria-label*='Share']").Each(func(i int, element *goquery.Selection) {
        if label, exists := element.Attr("aria-label"); exists {
            if count := fs.parseCountText(label); count > 0 {
                post.SharesCount = count
            }
        }
    })
}

func (fs *FacebookScraper) extractPostsFallback(doc *goquery.Document, groupID, groupName string) []types.ScrapedPost {
    var posts []types.ScrapedPost
    
    // Save HTML for debugging if needed
    // htmlContent, _ := doc.Html()
    // ioutil.WriteFile("facebook_debug.html", []byte(htmlContent), 0644)
    
    // Look for content indicators that might be part of posts
    contentIndicators := []string{
        // Text containers
        "p", "span[dir='auto']", "div[dir='auto']", 
        // Engagement indicators
        "[aria-label*='Like']", "[aria-label*='Comment']", "[aria-label*='Share']",
        // Media containers 
        "div.story_body_container", "div[data-ft]", "a[href*='/photo.php']",
    }
    
    processedSelectors := make(map[string]bool)
    
    for _, indicator := range contentIndicators {
        doc.Find(indicator).Each(func(i int, s *goquery.Selection) {
            // Try to find the parent post container by traversing up
            postContainer := s.ParentsFiltered("div[id]").First()
            if postContainer.Length() == 0 {
                return
            }
            
            // Generate a unique ID for this potential post container to avoid duplicates
            postId, exists := postContainer.Attr("id")
            if !exists || processedSelectors[postId] {
                return
            }
            processedSelectors[postId] = true
            
            // Check if this looks like a post by searching for engagement elements
            if fs.looksLikePost(postContainer) {
                post := fs.extractSinglePost(postContainer, groupID, groupName)
                if post != nil {
                    posts = append(posts, *post)
                }
            }
        })
    }
    
    return posts
}

func (fs *FacebookScraper) looksLikePost(s *goquery.Selection) bool {
    // Check for common post indicators
    hasEngagement := s.Find("[aria-label*='Like'], [aria-label*='Comment'], [aria-label*='Share']").Length() > 0
    hasAuthor := s.Find("h3 a, strong a, a[data-hovercard]").Length() > 0
    hasContent := s.Find("p, span[dir='auto'], div[dir='auto']").Length() > 0
    hasTimestamp := s.Find("abbr, a[href*='permalink'], span[data-utime]").Length() > 0
    
    // Check for post attributes
    hasPostAttr := false
    if attr, exists := s.Attr("data-ft"); exists {
        hasPostAttr = strings.Contains(attr, "top_level_post_id") || 
                      strings.Contains(attr, "story_fbid")
    }
    
    // Combine indicators to determine if it's likely a post
    return (hasEngagement && hasAuthor) || 
           (hasContent && hasTimestamp) || 
           hasPostAttr
}

func (fs *FacebookScraper) extractSinglePost(s *goquery.Selection, groupID, groupName string) *types.ScrapedPost {
    post := &types.ScrapedPost{
        GroupID:   groupID,
        PostTime:  time.Now(), // Default to current time
    }
    
    // Try to find a unique post ID
    // Method 1: data-ft attribute
    if dataFt, exists := s.Attr("data-ft"); exists {
        var dataFtObj map[string]interface{}
        if err := json.Unmarshal([]byte(dataFt), &dataFtObj); err == nil {
            if postID, ok := dataFtObj["top_level_post_id"].(string); ok {
                post.ID = postID
            } else if contentID, ok := dataFtObj["content_id"].(string); ok {
                post.ID = contentID
            }
        }
    }
    
    // Method 2: Check for permalink in hrefs
    if post.ID == "" {
        s.Find("a[href*='/permalink/'], a[href*='/posts/']").Each(func(i int, link *goquery.Selection) {
            href, exists := link.Attr("href")
            if exists {
                re := regexp.MustCompile(`/(?:permalink|posts)/(\d+)`)
                if matches := re.FindStringSubmatch(href); len(matches) > 1 {
                    post.ID = matches[1]
                    return
                }
            }
        })
    }
    
    // Method 3: Use a span with a data-sigil attribute containing "story"
    if post.ID == "" {
        if dataSignal, exists := s.Find("span[data-sigil*='story']").Attr("data-sigil"); exists {
            re := regexp.MustCompile(`story-(\d+)`)
            if matches := re.FindStringSubmatch(dataSignal); len(matches) > 1 {
                post.ID = matches[1]
            }
        }
    }
    
    // Fallback: Generate a pseudo-random ID
    if post.ID == "" {
        post.ID = fmt.Sprintf("%s_%d", groupID, time.Now().UnixNano())
    }
    
    // Extract content using multiple approaches
    post.Content = fs.extractContent(s)
    
    // Extract author
    post.AuthorName = fs.extractAuthor(s)
    
    // Extract post URL
    post.URL = fmt.Sprintf("https://www.facebook.com/groups/%s/posts/%s", groupID, post.ID)
    
    // Extract timestamp
    fs.extractTimestamp(s, post)
    
    // Extract engagement metrics
    fs.extractEngagement(s, post)
    
    // Only return posts with sufficient information
    if post.Content != "" || post.LikesCount > 0 {
        fs.logger.Debugf("Found post: ID=%s, Author=%s, Likes=%d, Comments=%d, Content=%s",
            post.ID, post.AuthorName, post.LikesCount, post.CommentsCount, 
            truncateString(post.Content, 50))
        return post
    }
    
    return nil
}

func (fs *FacebookScraper) extractContent(s *goquery.Selection) string {
    contentSelectors := []string{
        // Primary content selectors
        "[data-testid='post_message']", ".userContent", "._5pbx p", 
        // Mobile content selectors  
        ".story_body_container > div", "div[data-ft] > span", 
        // General content selectors
        "span[dir='auto']", "div[dir='auto']", "p",
    }
    
    for _, selector := range contentSelectors {
        content := s.Find(selector).FilterFunction(func(i int, s *goquery.Selection) bool {
            // Filter out UI elements that aren't post content
            classes := s.AttrOr("class", "")
            return !strings.Contains(classes, "uiPopover") &&
                   !strings.Contains(classes, "UFICommentContainer") &&
                   !strings.Contains(classes, "comment") &&
                   !strings.Contains(s.AttrOr("data-testid", ""), "react")
        })
        
        if content.Length() > 0 {
            var combinedText strings.Builder
            content.Each(func(i int, el *goquery.Selection) {
                text := strings.TrimSpace(el.Text())
                if text != "" {
                    if combinedText.Len() > 0 {
                        combinedText.WriteString(" ")
                    }
                    combinedText.WriteString(text)
                }
            })
            
            if combinedText.Len() > 0 {
                return combinedText.String()
            }
        }
    }
    
    // If we haven't found content yet, try getting the whole text content
    contentAreas := []string{
        "div._5pcr", "div.story_body_container", "div[role='article']", "div[data-ft]",
    }
    
    for _, selector := range contentAreas {
        contentArea := s.Find(selector).First()
        if contentArea.Length() > 0 {
            // Remove known UI elements before getting text
            contentArea.Find(".uiPopover, .UFICommentContainer, [data-testid*='react']").Remove()
            text := strings.TrimSpace(contentArea.Text())
            if text != "" {
                // If text is too long (likely containing UI elements), truncate and clean
                if len(text) > 2000 {
                    lines := strings.Split(text, "\n")
                    for _, line := range lines {
                        trimmed := strings.TrimSpace(line)
                        if len(trimmed) > 20 && !strings.HasPrefix(trimmed, "Like") && !strings.HasPrefix(trimmed, "Comment") {
                            return trimmed
                        }
                    }
                    // If no good line found, return a reasonable portion
                    return text[:1000]
                }
                return text
            }
        }
    }
    
    return ""
}

func (fs *FacebookScraper) extractAuthor(s *goquery.Selection) string {
    authorSelectors := []string{
        // Primary author selectors
        "h3 a", "h5 a", "strong a", 
        // Mobile author selectors
        "header a strong", "header h3", 
        // From story subtitle
        "[data-testid='story-subtitle'] a", 
        // General author selectors
        ".profileLink", "a.actor-link", "a[data-hovercard]",
    }
    
    for _, selector := range authorSelectors {
        author := s.Find(selector).First()
        if author.Length() > 0 {
            name := strings.TrimSpace(author.Text())
            
            // Check if this looks like an author name (not a button or link text)
            if name != "" && len(name) < 100 && 
               !strings.HasPrefix(name, "http") &&
               !strings.Contains(strings.ToLower(name), "like") &&
               !strings.Contains(strings.ToLower(name), "comment") {
                return name
            }
        }
    }
    
    return ""
}

func (fs *FacebookScraper) extractTimestamp(s *goquery.Selection, post *types.ScrapedPost) {
    // Look for explicit timestamp elements
    timestampSelectors := []string{
        "abbr[data-utime]", "span[data-utime]", 
        "a span.timestampContent", 
        "[data-testid='story-subtitle'] abbr",
    }
    
    for _, selector := range timestampSelectors {
        timestamp := s.Find(selector).First()
        if timestamp.Length() > 0 {
            // Method 1: data-utime attribute
            if utime, exists := timestamp.Attr("data-utime"); exists {
                if timestamp, err := strconv.ParseInt(utime, 10, 64); err == nil {
                    post.PostTime = time.Unix(timestamp, 0)
                    return
                }
            }
            
            // Method 2: Parse from text
            timeText := timestamp.Text()
            parsedTime := fs.parseTimeText(timeText)
            if !parsedTime.IsZero() {
                post.PostTime = parsedTime
                return
            }
        }
    }
    
    // Look for links with timestamp info
    s.Find("a[href*='permalink'], a[href*='posts']").Each(func(i int, link *goquery.Selection) {
        linkText := link.Text()
        if strings.Contains(linkText, "hr") || 
           strings.Contains(linkText, "min") || 
           strings.Contains(linkText, "now") ||
           strings.Contains(linkText, "Yesterday") {
            parsedTime := fs.parseTimeText(linkText)
            if !parsedTime.IsZero() {
                post.PostTime = parsedTime
                return
            }
        }
    })
}

func (fs *FacebookScraper) extractEngagement(s *goquery.Selection, post *types.ScrapedPost) {
    // Extract likes
    likeSelectors := []string{
        "a[aria-label*='Like']", "a[aria-label*='reaction']",
        "span[data-testid*='like']", "span.like_def",
        "span._81hb", "span._4arz",
    }
    
    for _, selector := range likeSelectors {
        likeElement := s.Find(selector).First()
        if likeElement.Length() > 0 {
            // Check text directly
            likeText := likeElement.Text()
            if likeCount := fs.parseCountText(likeText); likeCount > 0 {
                post.LikesCount = likeCount
                break
            }
            
            // Check aria-label attribute
            if ariaLabel, exists := likeElement.Attr("aria-label"); exists {
                if likeCount := fs.parseCountText(ariaLabel); likeCount > 0 {
                    post.LikesCount = likeCount
                    break
                }
            }
        }
    }
    
    // Extract comments
    commentSelectors := []string{
        "a[aria-label*='comment']", "span[data-testid*='comment']", 
        "a._3hg-", "span._1whp",
    }
    
    for _, selector := range commentSelectors {
        commentElement := s.Find(selector).First()
        if commentElement.Length() > 0 {
            commentText := commentElement.Text()
            if commentCount := fs.parseCountText(commentText); commentCount > 0 {
                post.CommentsCount = commentCount
                break
            }
            
            if ariaLabel, exists := commentElement.Attr("aria-label"); exists {
                if commentCount := fs.parseCountText(ariaLabel); commentCount > 0 {
                    post.CommentsCount = commentCount
                    break
                }
            }
        }
    }
    
    // Extract shares
    shareSelectors := []string{
        "a[aria-label*='share']", "span[data-testid*='share']",
        "span._355t", "span._15ko",
    }
    
    for _, selector := range shareSelectors {
        shareElement := s.Find(selector).First()
        if shareElement.Length() > 0 {
            shareText := shareElement.Text()
            if shareCount := fs.parseCountText(shareText); shareCount > 0 {
                post.SharesCount = shareCount
                break
            }
            
            if ariaLabel, exists := shareElement.Attr("aria-label"); exists {
                if shareCount := fs.parseCountText(ariaLabel); shareCount > 0 {
                    post.SharesCount = shareCount
                    break
                }
            }
        }
    }
    
    // If still no engagement data, try looking for numeric patterns in all text
    if post.LikesCount == 0 && post.CommentsCount == 0 && post.SharesCount == 0 {
        s.Find("span, div").Each(func(i int, el *goquery.Selection) {
            text := el.Text()
            
            // Check for like indicators
            if strings.Contains(strings.ToLower(text), "like") {
                if count := fs.parseCountText(text); count > 0 {
                    post.LikesCount = count
                }
            }
            
            // Check for comment indicators
            if strings.Contains(strings.ToLower(text), "comment") {
                if count := fs.parseCountText(text); count > 0 {
                    post.CommentsCount = count
                }
            }
            
            // Check for share indicators
            if strings.Contains(strings.ToLower(text), "share") {
                if count := fs.parseCountText(text); count > 0 {
                    post.SharesCount = count
                }
            }
        })
    }
}

func (fs *FacebookScraper) parseCountText(text string) int {
    text = strings.ToLower(strings.TrimSpace(text))
    
    // Pattern for numbers with suffixes like 1.2K, 4.5M
    pattern1 := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([km])`)
    if matches := pattern1.FindStringSubmatch(text); len(matches) >= 3 {
        num, _ := strconv.ParseFloat(matches[1], 64)
        switch matches[2] {
        case "k":
            return int(num * 1000)
        case "m":
            return int(num * 1000000)
        }
    }
    
    // Pattern for numbers followed by text
    pattern2 := regexp.MustCompile(`(\d+(?:,\d+)*)`)
    if matches := pattern2.FindStringSubmatch(text); len(matches) >= 2 {
        // Remove commas
        numStr := strings.ReplaceAll(matches[1], ",", "")
        if num, err := strconv.Atoi(numStr); err == nil {
            return num
        }
    }
    
    return 0
}

func (fs *FacebookScraper) parseTimeText(timeText string) time.Time {
    timeText = strings.ToLower(strings.TrimSpace(timeText))
    now := time.Now()
    
    // Handle "X minutes/hours ago" format
    if matches := regexp.MustCompile(`(\d+)\s*min`).FindStringSubmatch(timeText); len(matches) > 1 {
        if mins, err := strconv.Atoi(matches[1]); err == nil {
            return now.Add(-time.Duration(mins) * time.Minute)
        }
    }
    
    if matches := regexp.MustCompile(`(\d+)\s*h`).FindStringSubmatch(timeText); len(matches) > 1 {
        if hours, err := strconv.Atoi(matches[1]); err == nil {
            return now.Add(-time.Duration(hours) * time.Hour)
        }
    }
    
    if matches := regexp.MustCompile(`(\d+)\s*d`).FindStringSubmatch(timeText); len(matches) > 1 {
        if days, err := strconv.Atoi(matches[1]); err == nil {
            return now.AddDate(0, 0, -days)
        }
    }
    
    // Handle "Yesterday at HH:MM" format
    if strings.Contains(timeText, "yesterday") {
        return now.AddDate(0, 0, -1)
    }
    
    // Handle "Monday at HH:MM" format (day of week)
    daysOfWeek := map[string]int{
        "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4,
        "friday": 5, "saturday": 6, "sunday": 0,
    }
    
    for day, dayNum := range daysOfWeek {
        if strings.Contains(timeText, day) {
            currentDay := int(now.Weekday())
            diff := currentDay - dayNum
            if diff < 0 {
                diff += 7
            }
            return now.AddDate(0, 0, -diff)
        }
    }
    
    // Handle specific date formats
    dateFormats := []string{
        "January 2 at 3:04 pm",
        "January 2",
        "Jan 2 at 3:04 pm",
        "Jan 2",
    }
    
    for _, format := range dateFormats {
        if t, err := time.Parse(format, timeText); err == nil {
            // Set year to current year since Facebook often omits it
            return time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
        }
    }
    
    return time.Time{} // Return zero time if parsing fails
}

func (fs *FacebookScraper) ScrapeGroup(groupID string) error {
    fs.logger.Infof("Starting to scrape group: %s", groupID)
    
    // Get group name first
    groupName, err := fs.getGroupName(groupID)
    if err != nil {
        fs.logger.Warnf("Failed to get group name for %s: %v", groupID, err)
        groupName = fmt.Sprintf("Group_%s", groupID)
    }
    
    // Set improved user agent for better compatibility
    originalUserAgent := fs.userAgent
    fs.userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36"
    
    // Try different URL patterns to get posts
    urls := []string{
        fmt.Sprintf("https://m.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://m.facebook.com/groups/%s/posts", groupID),
        fmt.Sprintf("https://www.facebook.com/groups/%s?sorting_setting=CHRONOLOGICAL", groupID),
    }
    
    allPosts := []types.ScrapedPost{}
    
    for urlIndex, groupURL := range urls {
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
        
        // Handle compressed response properly
        var reader io.Reader = resp.Body
        
        // Check for gzip compression
        if resp.Header.Get("Content-Encoding") == "gzip" {
            gzipReader, err := gzip.NewReader(resp.Body)
            if err != nil {
                fs.logger.Errorf("Failed to create gzip reader for %s: %v", groupURL, err)
                continue
            }
            defer gzipReader.Close()
            reader = gzipReader
        } else if resp.Header.Get("Content-Encoding") == "deflate" {
            // Handle deflate compression
            reader = flate.NewReader(resp.Body)
        }
        
        // Read the decompressed content
        bodyBytes, err := io.ReadAll(reader)
        if err != nil {
            fs.logger.Errorf("Failed to read response body from %s: %v", groupURL, err)
            continue
        }
        
        fs.logger.Debugf("Decompressed content length: %d bytes", len(bodyBytes))
        
        // Check if content looks like HTML
        bodyStr := string(bodyBytes)
        if !strings.Contains(strings.ToLower(bodyStr), "<html") && !strings.Contains(strings.ToLower(bodyStr), "<!doctype") {
            fs.logger.Warnf("Response doesn't appear to be HTML for %s", groupURL)
            // Save first 500 chars for debugging
            fs.logger.Debugf("Response preview: %s", bodyStr[:min(500, len(bodyStr))])
            continue
        }
        
        doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
        if err != nil {
            fs.logger.Errorf("Failed to parse HTML from %s: %v", groupURL, err)
            continue
        }
        
        // Debug: Save HTML for inspection (only for first URL)
        if urlIndex == 0 {
            debugFile := fmt.Sprintf("debug_facebook_%s.html", groupID)
            if err := os.WriteFile(debugFile, bodyBytes, 0644); err == nil {
                fs.logger.Debugf("Saved decompressed HTML to %s for debugging", debugFile)
            }
        }
        
        // Extract posts
        posts := fs.extractPosts(doc, groupID, groupName)
        fs.logger.Infof("Found %d posts from %s", len(posts), groupURL)
        allPosts = append(allPosts, posts...)
        
        // Rate limiting between requests
        time.Sleep(fs.rateLimit)
    }
    
    // Reset user agent
    fs.userAgent = originalUserAgent
    
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

func (fs *FacebookScraper) setHeaders(req *http.Request) {
    req.Header.Set("User-Agent", fs.userAgent)
    req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
    req.Header.Set("Accept-Language", "en-US,en;q=0.5")
    req.Header.Set("Accept-Encoding", "gzip, deflate") // This tells server we can handle compression
    req.Header.Set("DNT", "1")
    req.Header.Set("Connection", "keep-alive")
    req.Header.Set("Upgrade-Insecure-Requests", "1")
    req.Header.Set("Sec-Fetch-Dest", "document")
    req.Header.Set("Sec-Fetch-Mode", "navigate")
    req.Header.Set("Sec-Fetch-Site", "none")
}

// Helper function for min
func min(a, b int) int {
    if a < b {
        return a
    }
    return b
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