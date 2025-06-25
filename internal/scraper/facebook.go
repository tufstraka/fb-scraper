package scraper

import (
    "compress/flate"
    "compress/gzip"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
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

    // Create filter for posts with 10+ likes in past 5 days (realistic for testing)
    filter := &types.PostFilter{
        MinLikes:        1,
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

func (fs *FacebookScraper) ScrapeGroup(groupID string) error {
    fs.logger.Infof("Starting to scrape group: %s", groupID)
    
    // Get group name first
    groupName, err := fs.getGroupName(groupID)
    if err != nil {
        fs.logger.Warnf("Failed to get group name for %s: %v", groupID, err)
        groupName = fmt.Sprintf("Group_%s", groupID)
    }
    
    // Try static scraping first (existing method)
    allPosts := fs.scrapeWithStaticMethod(groupID, groupName)
    
    // If static scraping returns very few posts, try browser automation
    if len(allPosts) < 5 {
        fs.logger.Info("Static scraping found few posts, trying browser automation...")
        browserPosts, err := fs.scrapeWithBrowser(groupID, groupName)
        if err != nil {
            fs.logger.Warnf("Browser scraping failed: %v", err)
        } else {
            fs.logger.Infof("Browser scraping found %d posts", len(browserPosts))
            allPosts = append(allPosts, browserPosts...)
        }
    }
    
    // Remove duplicates
    allPosts = fs.removeDuplicatePosts(allPosts)
    
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

func (fs *FacebookScraper) scrapeWithStaticMethod(groupID, groupName string) []types.ScrapedPost {
    urls := []string{
        fmt.Sprintf("https://m.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://mbasic.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://www.facebook.com/groups/%s?sorting_setting=CHRONOLOGICAL", groupID),
    }
    
    var allPosts []types.ScrapedPost
    
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
        
        // Handle compressed response
        var reader io.Reader = resp.Body
        encoding := resp.Header.Get("Content-Encoding")
        switch encoding {
        case "gzip":
            gzReader, err := gzip.NewReader(resp.Body)
            if err != nil {
                fs.logger.Errorf("Failed to create gzip reader: %v", err)
                continue
            }
            defer gzReader.Close()
            reader = gzReader
        case "deflate":
            reader = flate.NewReader(resp.Body)
        }
        
        bodyBytes, err := io.ReadAll(reader)
        if err != nil {
            fs.logger.Errorf("Failed to read response body: %v", err)
            continue
        }
        
        bodyStr := string(bodyBytes)
        if !strings.Contains(strings.ToLower(bodyStr), "<html") {
            fs.logger.Warnf("Response doesn't appear to be HTML for %s", groupURL)
            continue
        }
        
        doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
        if err != nil {
            fs.logger.Errorf("Failed to parse HTML: %v", err)
            continue
        }
        
        // Save HTML for debugging (first URL only)
        debugFile := fmt.Sprintf("debug_static_facebook_%s.html", groupID)
        if err := os.WriteFile(debugFile, bodyBytes, 0644); err == nil {
            fs.logger.Debugf("Saved static HTML to %s for debugging", debugFile)
        }
        
        // Extract posts using existing method
        posts := fs.extractPosts(doc, groupID, groupName)
        fs.logger.Infof("Found %d posts from %s", len(posts), groupURL)
        allPosts = append(allPosts, posts...)
        
        time.Sleep(fs.rateLimit)
    }
    
    return allPosts
}

// Update the scrapeWithBrowser method to fix cookie conversion

func (fs *FacebookScraper) scrapeWithBrowser(groupID, groupName string) ([]types.ScrapedPost, error) {
    // Try chromedp with Firefox first
    browserScraper := NewBrowserScraper(fs.logger)
    defer browserScraper.Close()
    
        // Get cookies from auth manager and convert to slice
    cookieJar := fs.authManager.GetAuthenticatedClient().Jar
    var cookies []Cookie
    if cookieJar != nil {
        facebookURL, _ := url.Parse("https://www.facebook.com")
        httpCookies := cookieJar.Cookies(facebookURL)
        for _, cookie := range httpCookies {
            cookies = append(cookies, Cookie{
                Name:  cookie.Name,
                Value: cookie.Value,
            })
        }
    }
    
    // Fix cookie domains for browser automation
    for i := range cookies {
        if cookies[i].Domain == "" {
            cookies[i].Domain = ".facebook.com"
        }
        if cookies[i].Path == "" {
            cookies[i].Path = "/"
        }
    }
    
    htmlContent, err := browserScraper.ScrapeGroupWithBrowser(groupID, cookies)
    if err != nil {
        fs.logger.Warnf("ChromeDP browser scraping failed: %v", err)
        
        // Fallback to Selenium Firefox
        fs.logger.Info("Trying Selenium Firefox as fallback...")
        seleniumScraper, err := NewSeleniumBrowserScraper(fs.logger)
        if err != nil {
            return nil, fmt.Errorf("failed to create Selenium scraper: %w", err)
        }
        defer seleniumScraper.Close()
        
        htmlContent, err = seleniumScraper.ScrapeGroupWithBrowser(groupID, cookies)
        if err != nil {
            return nil, fmt.Errorf("both ChromeDP and Selenium browser scraping failed: %w", err)
        }
    }
    
    // Parse the HTML content
    doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
    if err != nil {
        return nil, fmt.Errorf("failed to parse browser HTML: %w", err)
    }
    
    // Save HTML for debugging
    debugFile := fmt.Sprintf("debug_browser_facebook_%s.html", groupID)
    if err := os.WriteFile(debugFile, []byte(htmlContent), 0644); err == nil {
        fs.logger.Debugf("Saved browser HTML to %s for debugging", debugFile)
    }
    
    // Extract posts using enhanced mobile extraction
    posts := fs.extractMobilePosts(doc, groupID, groupName)
    return posts, nil
}

func (fs *FacebookScraper) extractPosts(doc *goquery.Document, groupID, groupName string) []types.ScrapedPost {
    var posts []types.ScrapedPost
    
    fs.logger.Debugf("Extracting posts from group: %s", groupName)
    
    // First, look for the feed container
    feedContainer := doc.Find("div[role='feed']").First()
    if feedContainer.Length() == 0 {
        fs.logger.Debug("No feed container found, trying fallback selectors")
        return fs.extractPostsFallback(doc, groupID, groupName)
    }
    
    fs.logger.Debugf("Found feed container, looking for posts...")
    
    // Look for post containers within the feed
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
        
        feedContainer.Children().Each(func(i int, child *goquery.Selection) {
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

func (fs *FacebookScraper) extractPostsFallback(doc *goquery.Document, groupID, groupName string) []types.ScrapedPost {
    var posts []types.ScrapedPost
    
    // Look for content indicators that might be part of posts
    contentIndicators := []string{
        "p", "span[dir='auto']", "div[dir='auto']", 
        "[aria-label*='Like']", "[aria-label*='Comment']", "[aria-label*='Share']",
        "div.story_body_container", "div[data-ft]", "a[href*='/photo.php']",
    }
    
    processedSelectors := make(map[string]bool)
    
    for _, indicator := range contentIndicators {
        doc.Find(indicator).Each(func(i int, s *goquery.Selection) {
            postContainer := s.ParentsFiltered("div[id]").First()
            if postContainer.Length() == 0 {
                return
            }
            
            postId, exists := postContainer.Attr("id")
            if !exists || processedSelectors[postId] {
                return
            }
            processedSelectors[postId] = true
            
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

func (fs *FacebookScraper) extractMobilePosts(doc *goquery.Document, groupID, groupName string) []types.ScrapedPost {
    var posts []types.ScrapedPost
    
    fs.logger.Debug("Extracting mobile posts...")
    
    // Mobile Facebook selectors
    postSelectors := []string{
        "div[data-ft*='top_level_post_id']",
        "div[id*='story']",
        "article",
        "div.story_body_container",
        "div[role='article']",
        "div._55wo._5rgr._5gh8",
    }
    
    for _, selector := range postSelectors {
        elements := doc.Find(selector)
        if elements.Length() > 0 {
            fs.logger.Debugf("Found %d potential posts with selector '%s'", elements.Length(), selector)
            elements.Each(func(i int, s *goquery.Selection) {
                post := fs.extractMobilePost(s, groupID, groupName)
                if post != nil && fs.isValidPost(post) {
                    posts = append(posts, *post)
                }
            })
            
            if len(posts) > 0 {
                break
            }
        }
    }
    
    // If still no posts, try broader search
    if len(posts) == 0 {
        fs.logger.Debug("No posts found with specific selectors, trying broader search...")
        doc.Find("div").Each(func(i int, s *goquery.Selection) {
            if fs.looksLikeMobilePost(s) {
                post := fs.extractMobilePost(s, groupID, groupName)
                if post != nil && fs.isValidPost(post) {
                    posts = append(posts, *post)
                }
            }
        })
    }
    
    fs.logger.Infof("Extracted %d mobile posts", len(posts))
    return posts
}

// Helper functions
func (fs *FacebookScraper) looksLikePostContainer(s *goquery.Selection) bool {
    hasDataTestId := false
    if testId, exists := s.Attr("data-testid"); exists {
        hasDataTestId = strings.Contains(testId, "post") || strings.Contains(testId, "story")
    }
    
    hasAuthor := s.Find("h2, h3, strong").Length() > 0
    hasEngagement := s.Find("[aria-label*='Like'], [aria-label*='Comment'], [aria-label*='Share']").Length() > 0
    hasContent := s.Find("[data-ad-rendering-role], [dir='auto']").Length() > 0
    
    return hasDataTestId || (hasAuthor && (hasEngagement || hasContent))
}

func (fs *FacebookScraper) looksLikePost(s *goquery.Selection) bool {
    hasEngagement := s.Find("[aria-label*='Like'], [aria-label*='Comment'], [aria-label*='Share']").Length() > 0
    hasAuthor := s.Find("h3 a, strong a, a[data-hovercard]").Length() > 0
    hasContent := s.Find("p, span[dir='auto'], div[dir='auto']").Length() > 0
    hasTimestamp := s.Find("abbr, a[href*='permalink'], span[data-utime]").Length() > 0
    
    hasPostAttr := false
    if attr, exists := s.Attr("data-ft"); exists {
        hasPostAttr = strings.Contains(attr, "top_level_post_id") || 
                      strings.Contains(attr, "story_fbid")
    }
    
    return (hasEngagement && hasAuthor) || 
           (hasContent && hasTimestamp) || 
           hasPostAttr
}

func (fs *FacebookScraper) looksLikeMobilePost(s *goquery.Selection) bool {
    hasTimestamp := s.Find("time, abbr[data-utime], span[data-utime]").Length() > 0
    hasAuthor := s.Find("h3, strong, a[role='link']").Length() > 0
    hasContent := s.Find("p, span, div[dir='auto']").Length() > 0
    hasEngagement := s.Find("a[role='button'], button").Length() > 0
    
    hasStoryId := false
    if id, exists := s.Attr("id"); exists {
        hasStoryId = strings.Contains(id, "story") || strings.Contains(id, "post")
    }
    
    hasDataFt := false
    if dataFt, exists := s.Attr("data-ft"); exists {
        hasDataFt = strings.Contains(dataFt, "post_id") || strings.Contains(dataFt, "story")
    }
    
    return (hasTimestamp && hasAuthor && hasContent) || hasStoryId || hasDataFt || 
           (hasAuthor && hasContent && hasEngagement)
}


func (fs *FacebookScraper) extractSinglePost(s *goquery.Selection, groupID, groupName string) *types.ScrapedPost {
    post := &types.ScrapedPost{
        GroupID:   groupID,
        PostTime:  time.Now(),
    }
    
    // Extract post ID
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
    
    if post.ID == "" {
        post.ID = fmt.Sprintf("%s_%d", groupID, time.Now().UnixNano())
    }
    
    post.Content = fs.extractContent(s)
    post.AuthorName = fs.extractAuthor(s)
    post.URL = fmt.Sprintf("https://www.facebook.com/groups/%s/posts/%s", groupID, post.ID)
    
    fs.extractTimestamp(s, post)
    fs.extractEngagement(s, post)
    
    if post.Content != "" || post.LikesCount > 0 {
        fs.logger.Debugf("Found post: ID=%s, Author=%s, Likes=%d, Comments=%d, Content=%s",
            post.ID, post.AuthorName, post.LikesCount, post.CommentsCount, 
            truncateString(post.Content, 50))
        return post
    }
    
    return nil
}

func (fs *FacebookScraper) extractMobilePost(s *goquery.Selection, groupID, groupName string) *types.ScrapedPost {
    post := &types.ScrapedPost{
        GroupID:   groupID,
        PostTime:  time.Now(),
    }
    
    post.ID = fs.extractMobilePostID(s, groupID)
    post.AuthorName = fs.extractMobileAuthor(s)
    post.Content = fs.extractMobileContent(s)
    
    fs.extractMobileTimestamp(s, post)
    fs.extractMobileEngagement(s, post)
    
    post.URL = fs.extractMobilePostURL(s, groupID, post.ID)
    
    return post
}

// Extraction helper methods
func (fs *FacebookScraper) extractPostID(s *goquery.Selection, groupID string) string {
    if describedBy, exists := s.Attr("aria-describedby"); exists {
        ids := strings.Split(describedBy, " ")
        for _, id := range ids {
            if strings.Contains(id, "_r_") && len(id) > 5 {
                return id
            }
        }
    }
    
    var postID string
    s.Find("a[href*='/photo/'], a[href*='/posts/'], a[href*='fbid=']").Each(func(i int, link *goquery.Selection) {
        href, exists := link.Attr("href")
        if !exists {
            return
        }
        
        re := regexp.MustCompile(`fbid=(\d+)`)
        if matches := re.FindStringSubmatch(href); len(matches) > 1 {
            postID = matches[1]
            return
        }
        
        re = regexp.MustCompile(`/posts/(\d+)`)
        if matches := re.FindStringSubmatch(href); len(matches) > 1 {
            postID = matches[1]
            return
        }
    })
    
    if postID != "" {
        return postID
    }
    
    if posinset, exists := s.Attr("aria-posinset"); exists {
        return fmt.Sprintf("%s_pos_%s", groupID, posinset)
    }
    
    return fmt.Sprintf("%s_%d", groupID, time.Now().UnixNano())
}

func (fs *FacebookScraper) extractMobilePostID(s *goquery.Selection, groupID string) string {
    if dataFt, exists := s.Attr("data-ft"); exists {
        if postID := fs.extractPostIDFromDataFt(dataFt); postID != "" {
            return postID
        }
    }
    
    if id, exists := s.Attr("id"); exists && strings.Contains(id, "story") {
        return id
    }
    
    var postID string
    s.Find("a[href*='story_fbid'], a[href*='posts/'], a[href*='permalink']").Each(func(i int, link *goquery.Selection) {
        href, exists := link.Attr("href")
        if !exists {
            return
        }
        
        re := regexp.MustCompile(`story_fbid=(\d+)`)
        if matches := re.FindStringSubmatch(href); len(matches) > 1 {
            postID = matches[1]
            return
        }
        
        re = regexp.MustCompile(`posts/(\d+)`)
        if matches := re.FindStringSubmatch(href); len(matches) > 1 {
            postID = matches[1]
            return
        }
    })
    
    if postID != "" {
        return postID
    }
    
    return fs.generatePostID(s, groupID)
}

func (fs *FacebookScraper) extractPostIDFromDataFt(dataFt string) string {
    var ftData map[string]interface{}
    if err := json.Unmarshal([]byte(dataFt), &ftData); err == nil {
        if topLevelPostID, ok := ftData["top_level_post_id"].(string); ok {
            return topLevelPostID
        }
        if mfStoryKey, ok := ftData["mf_story_key"].(string); ok {
            return mfStoryKey
        }
    }
    return ""
}

func (fs *FacebookScraper) generatePostID(s *goquery.Selection, groupID string) string {
    content := s.Text()
    if len(content) > 100 {
        content = content[:100]
    }
    
    hash := fmt.Sprintf("%x", len(content)+int(time.Now().Unix()))
    return fmt.Sprintf("%s_%s_%d", groupID, hash, time.Now().Unix())
}

func (fs *FacebookScraper) extractAuthorFromFeed(s *goquery.Selection) string {
    authorSelectors := []string{
        "h2 a strong span",
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

func (fs *FacebookScraper) extractMobileAuthor(s *goquery.Selection) string {
    authorSelectors := []string{
        "h3 a",
        "strong a",
        "a[role='link'] strong",
        "span[dir='auto'] a",
        "div[data-testid='post_author'] a",
    }
    
    for _, selector := range authorSelectors {
        author := s.Find(selector).First()
        if author.Length() > 0 {
            name := strings.TrimSpace(author.Text())
            if name != "" && len(name) < 100 {
                return name
            }
        }
    }
    
    return ""
}

func (fs *FacebookScraper) extractAuthor(s *goquery.Selection) string {
    authorSelectors := []string{
        "h3 a", "h5 a", "strong a", 
        "header a strong", "header h3", 
        "[data-testid='story-subtitle'] a", 
        ".profileLink", "a.actor-link", "a[data-hovercard]",
    }
    
    for _, selector := range authorSelectors {
        author := s.Find(selector).First()
        if author.Length() > 0 {
            name := strings.TrimSpace(author.Text())
            
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

func (fs *FacebookScraper) extractContentFromFeed(s *goquery.Selection) string {
    contentSelectors := []string{
        "[data-ad-rendering-role='story_message']",
        "[data-ad-rendering-role='message']",
        "[data-ad-preview='message']",
        "div[dir='auto'][style*='text-align: start']",
    }
    
    for _, selector := range contentSelectors {
        contentContainer := s.Find(selector).First()
        if contentContainer.Length() > 0 {
            contentContainer.Find("img").Remove()
            text := strings.TrimSpace(contentContainer.Text())
            if text != "" {
                return text
            }
        }
    }
    
    return ""
}

func (fs *FacebookScraper) extractMobileContent(s *goquery.Selection) string {
    contentSelectors := []string{
        "div[data-testid='post_message']",
        "div.userContent",
        "p",
        "span[lang]",
        "div[dir='auto']",
    }
    
    for _, selector := range contentSelectors {
        contentElement := s.Find(selector).First()
        if contentElement.Length() > 0 {
            contentElement.Find("img").Remove()
            text := strings.TrimSpace(contentElement.Text())
            if text != "" && len(text) > 10 {
                return text
            }
        }
    }
    
    return ""
}

func (fs *FacebookScraper) extractContent(s *goquery.Selection) string {
    contentSelectors := []string{
        "[data-testid='post_message']", ".userContent", "._5pbx p", 
        ".story_body_container > div", "div[data-ft] > span", 
        "span[dir='auto']", "div[dir='auto']", "p",
    }
    
    for _, selector := range contentSelectors {
        content := s.Find(selector).FilterFunction(func(i int, s *goquery.Selection) bool {
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
    
    return ""
}

func (fs *FacebookScraper) extractPostURL(s *goquery.Selection, groupID, postID string) string {
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
    
    return fmt.Sprintf("https://www.facebook.com/groups/%s/posts/%s", groupID, postID)
}

func (fs *FacebookScraper) extractMobilePostURL(s *goquery.Selection, groupID, postID string) string {
    var postURL string
    s.Find("a[href*='story_fbid'], a[href*='posts/'], a[href*='permalink']").Each(func(i int, link *goquery.Selection) {
        href, exists := link.Attr("href")
        if exists {
            if strings.HasPrefix(href, "http") {
                postURL = href
            } else if strings.HasPrefix(href, "/") {
                postURL = "https://m.facebook.com" + href
            }
            return
        }
    })
    
    if postURL != "" {
        return postURL
    }
    
    return fmt.Sprintf("https://m.facebook.com/groups/%s/posts/%s", groupID, postID)
}

func (fs *FacebookScraper) extractTimestampFromFeed(s *goquery.Selection, post *types.ScrapedPost) {
    timestampSelectors := []string{
        "a[href*='?'] span",
        "span[data-utime]",
        "abbr[data-utime]",
        "time",
    }
    
    for _, selector := range timestampSelectors {
        timestamp := s.Find(selector).First()
        if timestamp.Length() > 0 {
            if utime, exists := timestamp.Attr("data-utime"); exists {
                if ts, err := strconv.ParseInt(utime, 10, 64); err == nil {
                    post.PostTime = time.Unix(ts, 0)
                    return
                }
            }
            
            timeText := timestamp.Text()
            parsedTime := fs.parseTimeText(timeText)
            if !parsedTime.IsZero() {
                post.PostTime = parsedTime
                return
            }
        }
    }
}

func (fs *FacebookScraper) extractMobileTimestamp(s *goquery.Selection, post *types.ScrapedPost) {
    timestampSelectors := []string{
        "time[datetime]",
        "abbr[data-utime]",
        "span[data-utime]",
        "a[data-utime]",
    }
    
    for _, selector := range timestampSelectors {
        element := s.Find(selector).First()
        if element.Length() > 0 {
            if datetime, exists := element.Attr("datetime"); exists {
                if parsedTime, err := time.Parse(time.RFC3339, datetime); err == nil {
                    post.PostTime = parsedTime
                    return
                }
            }
            
            if utime, exists := element.Attr("data-utime"); exists {
                if timestamp, err := strconv.ParseInt(utime, 10, 64); err == nil {
                    post.PostTime = time.Unix(timestamp, 0)
                    return
                }
            }
            
            timeText := element.Text()
            parsedTime := fs.parseTimeText(timeText)
            if !parsedTime.IsZero() {
                post.PostTime = parsedTime
                return
            }
        }
    }
}

func (fs *FacebookScraper) extractTimestamp(s *goquery.Selection, post *types.ScrapedPost) {
    timestampSelectors := []string{
        "abbr[data-utime]", "span[data-utime]", 
        "a span.timestampContent", 
        "[data-testid='story-subtitle'] abbr",
    }
    
    for _, selector := range timestampSelectors {
        timestamp := s.Find(selector).First()
        if timestamp.Length() > 0 {
            if utime, exists := timestamp.Attr("data-utime"); exists {
                if timestamp, err := strconv.ParseInt(utime, 10, 64); err == nil {
                    post.PostTime = time.Unix(timestamp, 0)
                    return
                }
            }
            
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
    engagementSection := s.Find("[aria-label*='reaction'], [aria-label*='All reactions']").First()
    if engagementSection.Length() > 0 {
        reactionText := engagementSection.Text()
        if count := fs.parseCountText(reactionText); count > 0 {
            post.LikesCount = count
        }
    }
    
    s.Find("span").Each(func(i int, span *goquery.Selection) {
        text := span.Text()
        if strings.Contains(text, "Like") || strings.Contains(text, "reaction") {
            if count := fs.parseCountText(text); count > 0 && post.LikesCount == 0 {
                post.LikesCount = count
            }
        }
    })
    
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

func (fs *FacebookScraper) extractMobileEngagement(s *goquery.Selection, post *types.ScrapedPost) {
    s.Find("a[role='button'], button").Each(func(i int, button *goquery.Selection) {
        text := strings.ToLower(button.Text())
        ariaLabel := ""
        if label, exists := button.Attr("aria-label"); exists {
            ariaLabel = strings.ToLower(label)
        }
        
        combinedText := text + " " + ariaLabel
        
        if strings.Contains(combinedText, "like") || strings.Contains(combinedText, "react") {
            count := fs.parseEngagementCount(combinedText)
            if count > 0 {
                post.LikesCount = count
            }
        } else if strings.Contains(combinedText, "comment") {
            count := fs.parseEngagementCount(combinedText)
            if count > 0 {
                post.CommentsCount = count
            }
        } else if strings.Contains(combinedText, "share") {
            count := fs.parseEngagementCount(combinedText)
            if count > 0 {
                post.SharesCount = count
            }
        }
    })
    
    s.Find("span").Each(func(i int, span *goquery.Selection) {
        text := strings.ToLower(span.Text())
        if strings.Contains(text, "like") && len(text) < 50 {
            count := fs.parseEngagementCount(text)
            if count > 0 && post.LikesCount == 0 {
                post.LikesCount = count
            }
        }
    })
}

func (fs *FacebookScraper) extractEngagement(s *goquery.Selection, post *types.ScrapedPost) {
    likeSelectors := []string{
        "a[aria-label*='Like']", "a[aria-label*='reaction']",
        "span[data-testid*='like']", "span.like_def",
        "span._81hb", "span._4arz",
    }
    
    for _, selector := range likeSelectors {
        likeElement := s.Find(selector).First()
        if likeElement.Length() > 0 {
            likeText := likeElement.Text()
            if likeCount := fs.parseCountText(likeText); likeCount > 0 {
                post.LikesCount = likeCount
                break
            }
            
            if ariaLabel, exists := likeElement.Attr("aria-label"); exists {
                if likeCount := fs.parseCountText(ariaLabel); likeCount > 0 {
                    post.LikesCount = likeCount
                    break
                }
            }
        }
    }
    
    // Similar logic for comments and shares...
}

func (fs *FacebookScraper) parseCountText(text string) int {
    text = strings.ToLower(strings.TrimSpace(text))
    
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
    
    pattern2 := regexp.MustCompile(`(\d+(?:,\d+)*)`)
    if matches := pattern2.FindStringSubmatch(text); len(matches) >= 2 {
        numStr := strings.ReplaceAll(matches[1], ",", "")
        if num, err := strconv.Atoi(numStr); err == nil {
            return num
        }
    }
    
    return 0
}

func (fs *FacebookScraper) parseEngagementCount(text string) int {
    text = strings.TrimSpace(text)
    text = regexp.MustCompile(`(?i)(like|likes|comment|comments|share|shares|reaction|reactions)`).ReplaceAllString(text, "")
    
    re := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([KMB]?)`)
    matches := re.FindStringSubmatch(text)
    
    if len(matches) >= 2 {
        if num, err := strconv.ParseFloat(matches[1], 64); err == nil {
            multiplier := 1.0
            switch strings.ToUpper(matches[2]) {
            case "K":
                multiplier = 1000
            case "M":
                multiplier = 1000000
            case "B":
                multiplier = 1000000000
            }
            return int(num * multiplier)
        }
    }
    
    re = regexp.MustCompile(`\d+`)
    if match := re.FindString(text); match != "" {
        if num, err := strconv.Atoi(match); err == nil {
            return num
        }
    }
    
    return 0
}

func (fs *FacebookScraper) parseTimeText(timeText string) time.Time {
    timeText = strings.ToLower(strings.TrimSpace(timeText))
    now := time.Now()
    
    re := regexp.MustCompile(`(\d+)\s*([hmds])\w*`)
    matches := re.FindStringSubmatch(timeText)
    
    if len(matches) >= 3 {
        if num, err := strconv.Atoi(matches[1]); err == nil {
            unit := matches[2]
            switch unit {
            case "m":
                return now.Add(-time.Duration(num) * time.Minute)
            case "h":
                return now.Add(-time.Duration(num) * time.Hour)
            case "d":
                return now.Add(-time.Duration(num) * 24 * time.Hour)
            case "s":
                return now.Add(-time.Duration(num) * time.Second)
            }
        }
    }
    
    re = regexp.MustCompile(`(\d+)\s+(minute|hour|day|week|month)s?\s+ago`)
    matches = re.FindStringSubmatch(timeText)
    
    if len(matches) >= 3 {
        if num, err := strconv.Atoi(matches[1]); err == nil {
            unit := matches[2]
            switch unit {
            case "minute":
                return now.Add(-time.Duration(num) * time.Minute)
            case "hour":
                return now.Add(-time.Duration(num) * time.Hour)
            case "day":
                return now.Add(-time.Duration(num) * 24 * time.Hour)
            case "week":
                return now.Add(-time.Duration(num) * 7 * 24 * time.Hour)
            case "month":
                return now.Add(-time.Duration(num) * 30 * 24 * time.Hour)
            }
        }
    }
    
    return time.Time{}
}

func (fs *FacebookScraper) removeDuplicatePosts(posts []types.ScrapedPost) []types.ScrapedPost {
    seen := make(map[string]bool)
    var unique []types.ScrapedPost
    
    for _, post := range posts {
        key := fmt.Sprintf("%s_%s", post.ID, post.AuthorName)
        if !seen[key] {
            seen[key] = true
            unique = append(unique, post)
        }
    }
    
    return unique
}

func (fs *FacebookScraper) isValidPost(post *types.ScrapedPost) bool {
    return post.ID != "" && (post.Content != "" || post.AuthorName != "") &&
           len(post.Content) > 5
}

func (fs *FacebookScraper) setHeaders(req *http.Request) {
    req.Header.Set("User-Agent", fs.userAgent)
    req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
    req.Header.Set("Accept-Language", "en-US,en;q=0.5")
    req.Header.Set("Accept-Encoding", "gzip, deflate")
    req.Header.Set("DNT", "1")
    req.Header.Set("Connection", "keep-alive")
    req.Header.Set("Upgrade-Insecure-Requests", "1")
    req.Header.Set("Sec-Fetch-Dest", "document")
    req.Header.Set("Sec-Fetch-Mode", "navigate")
    req.Header.Set("Sec-Fetch-Site", "none")
}

// Update the saveScrapedPost method

func (fs *FacebookScraper) saveScrapedPost(post *types.ScrapedPost, groupID, groupName string) error {
    // Convert media items to JSON
    imagesJSON, err := json.Marshal(post.Images)
    if err != nil {
        fs.logger.Warnf("Failed to marshal images for post %s: %v", post.ID, err)
        imagesJSON = []byte("[]")
    }
    
    videosJSON, err := json.Marshal(post.Videos)
    if err != nil {
        fs.logger.Warnf("Failed to marshal videos for post %s: %v", post.ID, err)
        videosJSON = []byte("[]")
    }
    
    dbPost := &models.Post{
        GroupID:     groupID,
        GroupName:   groupName,
        PostID:      post.ID,
        AuthorName:  post.AuthorName,
        AuthorID:    post.AuthorID,
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

    return fs.db.SavePost(dbPost)
}

func (fs *FacebookScraper) Close() error {
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

// Add these new extraction methods after the existing ones

func (fs *FacebookScraper) extractSinglePostFromFeed(s *goquery.Selection, groupID, groupName string) *types.ScrapedPost {
    post := &types.ScrapedPost{
        GroupID:   groupID,
        PostTime:  time.Now(),
        Images:    []types.MediaItem{},
        Videos:    []types.MediaItem{},
        Mentions:  []string{},
        Hashtags:  []string{},
        Links:     []string{},
    }
    
    post.ID = fs.extractPostID(s, groupID)
    post.AuthorName = fs.extractAuthorFromFeed(s)
    post.Content = fs.extractContentFromFeed(s)
    post.URL = fs.extractPostURL(s, groupID, post.ID)
    
    fs.extractTimestampFromFeed(s, post)
    fs.extractEngagementFromFeed(s, post)
    
    // Extract media content
    fs.extractImages(s, post)
    fs.extractVideos(s, post)
    fs.extractMentions(s, post)
    fs.extractHashtags(s, post)
    fs.extractLinks(s, post)
    
    // Set post type and media count
    fs.setPostTypeAndMediaCount(post)
    
    if post.Content != "" || post.AuthorName != "" || post.LikesCount > 0 || len(post.Images) > 0 || len(post.Videos) > 0 {
        fs.logger.Debugf("Found post: ID=%s, Author=%s, Likes=%d, Comments=%d, Images=%d, Videos=%d, Content=%s",
            post.ID, post.AuthorName, post.LikesCount, post.CommentsCount, 
            len(post.Images), len(post.Videos), truncateString(post.Content, 50))
        return post
    }
    
    return nil
}

func (fs *FacebookScraper) extractImages(s *goquery.Selection, post *types.ScrapedPost) {
    imageSelectors := []string{
        "img[src*='scontent']",                    // Facebook CDN images
        "img[src*='fbcdn.net']",                   // Facebook CDN images
        "img[data-src*='scontent']",               // Lazy-loaded images
        "img[data-src*='fbcdn.net']",              // Lazy-loaded Facebook images
        "div[style*='background-image'] img",      // Background images with img tags
        "a[href*='/photo/'] img",                  // Photo link images
        "div[data-testid*='photo'] img",           // Photo containers
    }
    
    for _, selector := range imageSelectors {
        s.Find(selector).Each(func(i int, img *goquery.Selection) {
            src, exists := img.Attr("src")
            if !exists {
                src, exists = img.Attr("data-src")
            }
            
            if exists && fs.isValidImageURL(src) {
                mediaItem := types.MediaItem{
                    URL:  src,
                    Type: "image",
                }
                
                // Extract alt text as description
                if alt, hasAlt := img.Attr("alt"); hasAlt {
                    mediaItem.Description = alt
                }
                
                // Extract dimensions
                if width, hasWidth := img.Attr("width"); hasWidth {
                    if w, err := strconv.Atoi(width); err == nil {
                        mediaItem.Width = w
                    }
                }
                if height, hasHeight := img.Attr("height"); hasHeight {
                    if h, err := strconv.Atoi(height); err == nil {
                        mediaItem.Height = h
                    }
                }
                
                // Check for duplicates
                if !fs.containsMediaItem(post.Images, mediaItem) {
                    post.Images = append(post.Images, mediaItem)
                }
            }
        })
    }
    
    // Extract images from style attributes (background images)
    s.Find("div[style*='background-image']").Each(func(i int, div *goquery.Selection) {
        style, exists := div.Attr("style")
        if exists {
            // Extract URL from background-image: url(...)
            re := regexp.MustCompile(`background-image:\s*url\(['"]?([^'"()]+)['"]?\)`)
            matches := re.FindStringSubmatch(style)
            if len(matches) > 1 && fs.isValidImageURL(matches[1]) {
                mediaItem := types.MediaItem{
                    URL:  matches[1],
                    Type: "image",
                }
                
                if !fs.containsMediaItem(post.Images, mediaItem) {
                    post.Images = append(post.Images, mediaItem)
                }
            }
        }
    })
}

func (fs *FacebookScraper) extractVideos(s *goquery.Selection, post *types.ScrapedPost) {
    videoSelectors := []string{
        "video[src]",                              // Direct video elements
        "video source[src]",                       // Video sources
        "div[data-testid*='video'] video",         // Video containers
        "a[href*='/video/']",                      // Video links
        "div[data-testid*='video-attachment']",    // Video attachments
    }
    
    for _, selector := range videoSelectors {
        s.Find(selector).Each(func(i int, video *goquery.Selection) {
            var src string
            var exists bool
            
            if video.Is("video") {
                src, exists = video.Attr("src")
                if !exists {
                    // Check for source elements within video
                    source := video.Find("source").First()
                    if source.Length() > 0 {
                        src, exists = source.Attr("src")
                    }
                }
            } else if video.Is("a") {
                src, exists = video.Attr("href")
            }
            
            if exists && fs.isValidVideoURL(src) {
                mediaItem := types.MediaItem{
                    URL:  src,
                    Type: "video",
                }
                
                // Extract thumbnail from poster attribute
                if poster, hasPoster := video.Attr("poster"); hasPoster {
                    mediaItem.Thumbnail = poster
                }
                
                // Extract dimensions
                if width, hasWidth := video.Attr("width"); hasWidth {
                    if w, err := strconv.Atoi(width); err == nil {
                        mediaItem.Width = w
                    }
                }
                if height, hasHeight := video.Attr("height"); hasHeight {
                    if h, err := strconv.Atoi(height); err == nil {
                        mediaItem.Height = h
                    }
                }
                
                if !fs.containsMediaItem(post.Videos, mediaItem) {
                    post.Videos = append(post.Videos, mediaItem)
                }
            }
        })
    }
    
    // Look for video indicators in data attributes
    s.Find("div[data-video-id], div[data-videoid]").Each(func(i int, div *goquery.Selection) {
        videoId, exists := div.Attr("data-video-id")
        if !exists {
            videoId, exists = div.Attr("data-videoid")
        }
        
        if exists && videoId != "" {
            // Construct Facebook video URL
            videoURL := fmt.Sprintf("https://www.facebook.com/video/%s", videoId)
            
            mediaItem := types.MediaItem{
                URL:  videoURL,
                Type: "video",
            }
            
            // Look for thumbnail in the same container
            thumbnail := div.Find("img").First()
            if thumbnail.Length() > 0 {
                if thumbSrc, hasSrc := thumbnail.Attr("src"); hasSrc {
                    mediaItem.Thumbnail = thumbSrc
                }
            }
            
            if !fs.containsMediaItem(post.Videos, mediaItem) {
                post.Videos = append(post.Videos, mediaItem)
            }
        }
    })
}

func (fs *FacebookScraper) extractMentions(s *goquery.Selection, post *types.ScrapedPost) {
    mentionSelectors := []string{
        "a[data-hovercard*='user']",               // User mentions
        "a[href*='/profile.php?id=']",             // Profile ID links
        "a[href*='facebook.com/'][data-hovercard]", // Facebook profile links
        "span[data-testid='post_message'] a",      // Links in post content
    }
    
    for _, selector := range mentionSelectors {
        s.Find(selector).Each(func(i int, mention *goquery.Selection) {
            text := strings.TrimSpace(mention.Text())
            href, hasHref := mention.Attr("href")
            
            // Extract username/mention
            if text != "" && (strings.HasPrefix(text, "@") || hasHref) {
                if !fs.containsString(post.Mentions, text) {
                    post.Mentions = append(post.Mentions, text)
                }
            }
            
            // Extract from href if it's a profile link
            if hasHref && strings.Contains(href, "facebook.com") {
                // Extract username from URL
                re := regexp.MustCompile(`facebook\.com/([^/?]+)`)
                matches := re.FindStringSubmatch(href)
                if len(matches) > 1 && matches[1] != "" {
                    username := "@" + matches[1]
                    if !fs.containsString(post.Mentions, username) {
                        post.Mentions = append(post.Mentions, username)
                    }
                }
            }
        })
    }
    
    // Extract mentions from post content using regex
    content := post.Content
    if content != "" {
        // Look for @username patterns
        re := regexp.MustCompile(`@([a-zA-Z0-9._]+)`)
        matches := re.FindAllStringSubmatch(content, -1)
        for _, match := range matches {
            if len(match) > 1 {
                mention := "@" + match[1]
                if !fs.containsString(post.Mentions, mention) {
                    post.Mentions = append(post.Mentions, mention)
                }
            }
        }
    }
}

func (fs *FacebookScraper) extractHashtags(s *goquery.Selection, post *types.ScrapedPost) {
    // Extract hashtags from links
    s.Find("a[href*='/hashtag/']").Each(func(i int, hashtag *goquery.Selection) {
        text := strings.TrimSpace(hashtag.Text())
        if strings.HasPrefix(text, "#") {
            if !fs.containsString(post.Hashtags, text) {
                post.Hashtags = append(post.Hashtags, text)
            }
        }
    })
    
    // Extract hashtags from content using regex
    content := post.Content
    if content != "" {
        re := regexp.MustCompile(`#([a-zA-Z0-9_]+)`)
        matches := re.FindAllStringSubmatch(content, -1)
        for _, match := range matches {
            if len(match) > 1 {
                hashtag := "#" + match[1]
                if !fs.containsString(post.Hashtags, hashtag) {
                    post.Hashtags = append(post.Hashtags, hashtag)
                }
            }
        }
    }
}

func (fs *FacebookScraper) extractLinks(s *goquery.Selection, post *types.ScrapedPost) {
    linkSelectors := []string{
        "a[href*='http']",                         // External links
        "a[href*='l.facebook.com']",               // Facebook redirect links
        "div[data-testid*='link'] a",              // Link attachments
    }
    
    for _, selector := range linkSelectors {
        s.Find(selector).Each(func(i int, link *goquery.Selection) {
            href, exists := link.Attr("href")
            if exists && fs.isValidExternalURL(href) {
                // Decode Facebook redirect URLs
                if strings.Contains(href, "l.facebook.com") {
                    if decodedURL := fs.decodeFacebookURL(href); decodedURL != "" {
                        href = decodedURL
                    }
                }
                
                if !fs.containsString(post.Links, href) {
                    post.Links = append(post.Links, href)
                }
            }
        })
    }
}

// Helper methods
func (fs *FacebookScraper) isValidImageURL(url string) bool {
    if url == "" {
        return false
    }
    
    // Check for Facebook CDN URLs
    if strings.Contains(url, "scontent") || strings.Contains(url, "fbcdn.net") {
        return true
    }
    
    // Check for common image extensions
    imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp"}
    lowerURL := strings.ToLower(url)
    for _, ext := range imageExts {
        if strings.Contains(lowerURL, ext) {
            return true
        }
    }
    
    return false
}

func (fs *FacebookScraper) isValidVideoURL(url string) bool {
    if url == "" {
        return false
    }
    
    // Check for Facebook video URLs
    if strings.Contains(url, "facebook.com/video") || strings.Contains(url, "fbcdn.net") {
        return true
    }
    
    // Check for common video extensions
    videoExts := []string{".mp4", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mkv"}
    lowerURL := strings.ToLower(url)
    for _, ext := range videoExts {
        if strings.Contains(lowerURL, ext) {
            return true
        }
    }
    
    return false
}

func (fs *FacebookScraper) isValidExternalURL(url string) bool {
    if url == "" {
        return false
    }
    
    // Must be HTTP/HTTPS
    if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
        return false
    }
    
    // Exclude Facebook internal links (except redirect links)
    if strings.Contains(url, "facebook.com") && !strings.Contains(url, "l.facebook.com") {
        return false
    }
    
    return true
}

func (fs *FacebookScraper) decodeFacebookURL(urlStr string) string {
    // Extract the actual URL from Facebook's l.facebook.com redirect
    re := regexp.MustCompile(`l\.facebook\.com/l\.php\?u=([^&]+)`)
    matches := re.FindStringSubmatch(urlStr)
    if len(matches) > 1 {
        decoded, err := url.QueryUnescape(matches[1])
        if err == nil {
            return decoded
        }
    }
    return ""
}

func (fs *FacebookScraper) containsMediaItem(items []types.MediaItem, item types.MediaItem) bool {
    for _, existing := range items {
        if existing.URL == item.URL {
            return true
        }
    }
    return false
}

func (fs *FacebookScraper) containsString(slice []string, item string) bool {
    for _, existing := range slice {
        if existing == item {
            return true
        }
    }
    return false
}

func (fs *FacebookScraper) setPostTypeAndMediaCount(post *types.ScrapedPost) {
    imageCount := len(post.Images)
    videoCount := len(post.Videos)
    linkCount := len(post.Links)
    
    post.MediaCount = imageCount + videoCount
    
    // Determine post type
    if imageCount > 0 && videoCount > 0 {
        post.PostType = "mixed"
    } else if imageCount > 0 {
        post.PostType = "image"
    } else if videoCount > 0 {
        post.PostType = "video"
    } else if linkCount > 0 {
        post.PostType = "link"
    } else {
        post.PostType = "text"
    }
}