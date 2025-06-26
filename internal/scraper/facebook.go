package scraper

import (
    "compress/gzip"
    "context"
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
    "github.com/chromedp/cdproto/network"
    "github.com/chromedp/chromedp"
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

    // Create filter for posts with minimum likes in past 5 days
    filter := &types.PostFilter{
        MinLikes:        1,  // At least 1 like
        MaxLikes:        0,  // No upper limit
        MinComments:     0,  // No minimum comment requirement
        MinShares:       0,  // No minimum share requirement
        DaysBack:        5,  // Last 5 days
        Keywords:        []string{},
        ExcludeKeywords: []string{},
        GroupIDs:        []string{},
        PageIDs:         []string{},
        AuthorNames:     []string{},
    }

    return &FacebookScraper{
        authManager:   authManager,
        db:            db,
        filter:        filter,
        logger:        logger,
        userAgent:     userAgent,
        rateLimit:     rateLimit,
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

// ScrapeGroup is the main entry point for scraping a Facebook group
func (fs *FacebookScraper) ScrapeGroup(groupID string) error {
    fs.logger.Infof("Starting to scrape group: %s", groupID)
    
    // Get group name first - use static method for reliability
    groupName, err := fs.getGroupName(groupID)
    if err != nil {
        fs.logger.Warnf("Failed to get group name for %s: %v", groupID, err)
        groupName = fmt.Sprintf("Group_%s", groupID)
    }
    
    // Primary method: Browser-based scraping with enhanced browser scraper
    posts, err := fs.scrapeWithBrowserScraper(groupID, groupName)
    if err != nil || len(posts) < 5 {
        fs.logger.Warnf("Enhanced browser scraping had issues (%v) or found few posts (%d), trying static method as fallback", err, len(posts))
        
        // Fallback method: Static HTML scraping
        staticPosts := fs.scrapeWithStaticMethod(groupID, groupName)
        fs.logger.Infof("Static scraping found %d posts", len(staticPosts))
        
        // Combine posts from both methods
        posts = append(posts, staticPosts...)
    }
    
    // Remove duplicates
    uniquePosts := fs.removeDuplicatePosts(posts)
    
    // Apply filters
    fs.logger.Infof("Found %d total unique posts, applying filters...", len(uniquePosts))
    
    filteredPosts, stats := BatchFilter(uniquePosts, fs.filter)
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
    // Use only static method for reliability
    req, err := http.NewRequest("GET", fmt.Sprintf("https://m.facebook.com/groups/%s/about", groupID), nil)
    if err != nil {
        return "", err
    }

    fs.setHeaders(req)
    
    // Add cookie headers directly
    cookieJar := fs.client.Jar
    if cookieJar != nil {
        facebookURL, _ := url.Parse("https://m.facebook.com")
        cookies := cookieJar.Cookies(facebookURL)
        if len(cookies) > 0 {
            cookieStrings := make([]string, 0, len(cookies))
            for _, cookie := range cookies {
                cookieStrings = append(cookieStrings, cookie.Name+"="+cookie.Value)
            }
            req.Header.Set("Cookie", strings.Join(cookieStrings, "; "))
        }
    }
    
    resp, err := fs.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
    }

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

// scrapeWithBrowserScraper uses the EnhancedBrowserScraper for better reliability
func (fs *FacebookScraper) scrapeWithBrowserScraper(groupID, groupName string) ([]types.ScrapedPost, error) {
    fs.logger.Info("Starting enhanced browser scraping")
    
    // Create the enhanced browser scraper
    browserScraper := NewBrowserScraper(fs.logger)
    defer browserScraper.Close()
    
    // Convert cookies from auth manager to the format needed by browser scraper
    cookieJar := fs.authManager.GetAuthenticatedClient().Jar
    var cookies []Cookie
    if cookieJar != nil {
        facebookURL, _ := url.Parse("https://www.facebook.com")
        httpCookies := cookieJar.Cookies(facebookURL)
        for _, cookie := range httpCookies {
            cookies = append(cookies, Cookie{
                Name:     cookie.Name,
                Value:    cookie.Value,
                Domain:   ".facebook.com",
                Path:     "/",
                Secure:   cookie.Secure,
            })
        }
    }
    
    // Use the browser scraper to get HTML with scrolling
    html, err := browserScraper.ScrapeGroupWithScrolling(groupID, cookies)
    if err != nil {
        return nil, fmt.Errorf("browser scraping failed: %w", err)
    }
    
    // Save HTML for debugging
    debugFile := fmt.Sprintf("debug_browser_facebook_%s.html", groupID)
    if err := os.WriteFile(debugFile, []byte(html), 0644); err == nil {
        fs.logger.Debugf("Saved browser HTML to %s for debugging", debugFile)
    }
    
    // Parse HTML to extract posts
    doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
    if err != nil {
        return nil, fmt.Errorf("failed to parse HTML: %w", err)
    }
    
    // Extract posts
    posts := fs.extractPosts(doc, groupID, groupName)
    
    return posts, nil
}

// scrapeWithStaticMethod performs traditional HTTP-based scraping as a fallback
func (fs *FacebookScraper) scrapeWithStaticMethod(groupID, groupName string) []types.ScrapedPost {
    urls := []string{
        fmt.Sprintf("https://m.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://mbasic.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://www.facebook.com/groups/%s", groupID),
    }
    
    var allPosts []types.ScrapedPost
    
    for _, groupURL := range urls {
        fs.logger.Infof("Static scraping URL: %s", groupURL)
        
        req, err := http.NewRequest("GET", groupURL, nil)
        if err != nil {
            fs.logger.Errorf("Failed to create request for %s: %v", groupURL, err)
            continue
        }
        
        fs.setHeaders(req)
        
        // Add cookie headers directly
        cookieJar := fs.client.Jar
        if cookieJar != nil {
            parsedURL, _ := url.Parse(groupURL)
            cookies := cookieJar.Cookies(parsedURL)
            if len(cookies) > 0 {
                cookieStrings := make([]string, 0, len(cookies))
                for _, cookie := range cookies {
                    cookieStrings = append(cookieStrings, cookie.Name+"="+cookie.Value)
                }
                req.Header.Set("Cookie", strings.Join(cookieStrings, "; "))
            }
        }
        
        // Add more realistic browser headers
        req.Header.Set("Sec-Fetch-Dest", "document")
        req.Header.Set("Sec-Fetch-Mode", "navigate")
        req.Header.Set("Sec-Fetch-Site", "none")
        req.Header.Set("Sec-Fetch-User", "?1")
        req.Header.Set("Accept-Language", "en-US,en;q=0.9")
        
        // Use a client with longer timeout
        client := &http.Client{
            Timeout: 30 * time.Second,
            Jar:     cookieJar,
        }
        
        resp, err := client.Do(req)
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
        if encoding == "gzip" {
            gzReader, err := gzip.NewReader(resp.Body)
            if err != nil {
                fs.logger.Errorf("Failed to create gzip reader: %v", err)
                continue
            }
            defer gzReader.Close()
            reader = gzReader
        }
        
        bodyBytes, err := io.ReadAll(reader)
        if err != nil {
            fs.logger.Errorf("Failed to read response body: %v", err)
            continue
        }
        
        // Check if response is HTML
        bodyStr := string(bodyBytes)
        if !strings.Contains(strings.ToLower(bodyStr), "<html") {
            fs.logger.Warnf("Response doesn't appear to be HTML for %s", groupURL)
            continue
        }
        
        // Parse HTML
        doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
        if err != nil {
            fs.logger.Errorf("Failed to parse HTML: %v", err)
            continue
        }
        
        // Save HTML for debugging
        debugFile := fmt.Sprintf("debug_static_facebook_%s_%d.html", groupID, time.Now().Unix())
        if err := os.WriteFile(debugFile, bodyBytes, 0644); err == nil {
            fs.logger.Debugf("Saved static HTML to %s for debugging", debugFile)
        }
        
        // Extract posts
        posts := fs.extractPosts(doc, groupID, groupName)
        fs.logger.Infof("Found %d posts from %s", len(posts), groupURL)
        allPosts = append(allPosts, posts...)
        
        time.Sleep(fs.rateLimit)
    }
    
    return allPosts
}

// The EnhancedBrowserScraper implementation

type EnhancedBrowserScraper struct {
    logger *logrus.Logger
    ctx    context.Context
    cancel context.CancelFunc
}

func NewBrowserScraper(logger *logrus.Logger) *EnhancedBrowserScraper {
    opts := append(chromedp.DefaultExecAllocatorOptions[:],
        chromedp.Flag("headless", false), // Set to false for debugging, true for production
        chromedp.Flag("disable-gpu", true),
        chromedp.Flag("disable-web-security", false),
        chromedp.Flag("disable-features", "IsolateOrigins,site-per-process"),
        chromedp.Flag("disable-site-isolation-trials", false),
        chromedp.Flag("no-sandbox", false), // Use with caution, only if necessary
        chromedp.Flag("disable-dev-shm-usage", true),
        chromedp.Flag("start-maximized", true),
        // Additional options to improve stability
        chromedp.Flag("enable-automation", false),
        //chromedp.Flag("disable-blink-features", "AutomationControlled"),
        chromedp.WindowSize(1280, 900),
    )

    // Create a new allocator with a longer timeout
    allocCtx, cancelAllocator := chromedp.NewExecAllocator(context.Background(), opts...)    
    // Create a longer context timeout for the browser
    timeoutCtx, cancelTimeout := context.WithTimeout(allocCtx, 10*time.Minute)
    
    // Create the browser context
    ctx, cancel := chromedp.NewContext(
        timeoutCtx,
        chromedp.WithLogf(logger.Debugf),
    )

    return &EnhancedBrowserScraper{
        logger: logger,
        ctx:    ctx,
        cancel: func() {
            cancel()
            cancelTimeout()
            cancelAllocator()
        },
    }
}

func (ebs *EnhancedBrowserScraper) ScrapeGroupWithScrolling(groupID string, cookies []Cookie) (string, error) {
    ebs.logger.Infof("Starting enhanced browser scraping for group %s", groupID)

    // Multiple URL strategies
    urls := []string{
        fmt.Sprintf("https://m.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://mbasic.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://www.facebook.com/groups/%s?sorting_setting=CHRONOLOGICAL", groupID),
    }

    var finalHTML string
    var lastError error

    for _, url := range urls {
        ebs.logger.Infof("Trying URL: %s", url)
        
        html, err := ebs.scrapeURL(url, cookies)
        if err != nil {
            ebs.logger.Warnf("Failed to scrape %s: %v", url, err)
            lastError = err
            continue
        }

        // Check if we got meaningful content
        if strings.Contains(html, "story") || strings.Contains(html, "post") || 
           strings.Contains(html, "data-ft") || len(html) > 50000 {
            finalHTML = html
            ebs.logger.Infof("Successfully scraped %s with %d characters", url, len(html))
            break
        }
    }

    if finalHTML == "" {
        return "", fmt.Errorf("all URLs failed, last error: %v", lastError)
    }

    return finalHTML, nil
}

func (ebs *EnhancedBrowserScraper) scrapeURL(url string, cookies []Cookie) (string, error) {
    var html string
    
    // Use a fresh context for each URL to avoid timeout issues
    ctx, cancel := context.WithTimeout(ebs.ctx, 7*time.Minute)
    defer cancel()
    
    err := chromedp.Run(ctx,
        // Clear all existing cookies and storage for a fresh start
        chromedp.ActionFunc(func(ctx context.Context) error {
            // Clear cookies
            err := network.ClearBrowserCookies().Do(ctx)
            if err != nil {
                return err
            }
            // Clear localStorage
            err = chromedp.Evaluate(`localStorage.clear()`, nil).Do(ctx)
            if err != nil {
                ebs.logger.Warnf("Failed to clear localStorage: %v", err)
                // Continue anyway
            }
            return nil
        }),
        
        // Navigate to Facebook first (important for cookie setting)
        chromedp.Navigate("https://www.facebook.com"),
        
        // Set cookies
        chromedp.ActionFunc(func(ctx context.Context) error {
            for _, cookie := range cookies {
                exp := network.SetCookie(cookie.Name, cookie.Value).
                    WithDomain(".facebook.com").
                    WithPath("/").
                    WithHTTPOnly(false).
                    WithSecure(true)
                if err := exp.Do(ctx); err != nil {
                    ebs.logger.Warnf("Failed to set cookie %s: %v", cookie.Name, err)
                }
            }
            return nil
        }),
        
        // Small delay to ensure cookies are set
        chromedp.Sleep(2*time.Second),
        
        // Navigate to the target URL
        chromedp.Navigate(url),
        chromedp.Sleep(5*time.Second),
        
        // Handle cookie consent if it appears
        chromedp.ActionFunc(func(ctx context.Context) error {
            consentSelectors := []string{
                `button[data-cookiebanner="accept_button"]`,
                `button[data-testid="cookie-policy-manage-dialog-accept-button"]`,
                `button:contains("Accept All")`,
                `button:contains("Accept")`,
            }
            
            for _, selector := range consentSelectors {
                var visible bool
                if err := chromedp.Evaluate(`document.querySelector('`+selector+`') !== null`, &visible).Do(ctx); err != nil {
                    continue
                }
                
                if visible {
                    ebs.logger.Info("Found cookie consent button, clicking...")
                    if err := chromedp.Click(selector).Do(ctx); err == nil {
                        chromedp.Sleep(2*time.Second).Do(ctx)
                        break
                    }
                }
            }
            return nil
        }),
        
        // Handle login redirects if they happen
        chromedp.ActionFunc(func(ctx context.Context) error {
            var currentURL string
            if err := chromedp.Evaluate(`window.location.href`, &currentURL).Do(ctx); err != nil {
                return err
            }
            
            if strings.Contains(currentURL, "/login") {
                return fmt.Errorf("redirected to login page: %s", currentURL)
            }
            
            return nil
        }),
        
        // Perform scrolling to load content
        ebs.performScrolling(),
        
        // Get the final HTML after all scrolling
        chromedp.OuterHTML("html", &html),
    )

    if err != nil {
        ebs.logger.Errorf("chromedp.Run failed for URL %s: %v", url, err)
    }
    return html, err
}

// performScrolling is a combined scrolling function that tries multiple approaches
func (ebs *EnhancedBrowserScraper) performScrolling() chromedp.ActionFunc {
    return func(ctx context.Context) error {
        ebs.logger.Info("Starting scrolling strategy...")
        
        // First try progressive scrolling
        if err := ebs.progressiveScroll(ctx); err != nil {
            ebs.logger.Warnf("Progressive scroll had issues: %v, trying infinite scroll", err)
            
            // Fall back to infinite scroll
            if err := ebs.infiniteScroll(ctx); err != nil {
                ebs.logger.Warnf("Infinite scroll had issues: %v, trying to click see more buttons", err)
                
                // Last resort: click "See More" buttons
                return ebs.clickSeeMoreButtons(ctx)
            }
        }
        
        return nil
    }
}

func (ebs *EnhancedBrowserScraper) progressiveScroll(ctx context.Context) error {
    ebs.logger.Info("Using progressive scroll strategy...")
    
    // Wait for initial content
    if err := chromedp.WaitVisible("body", chromedp.ByQuery).Do(ctx); err != nil {
        return err
    }

    initialPostCount := 0
    chromedp.Evaluate(`document.querySelectorAll('[data-ft], [id*="story"], article, [role="article"]').length`, &initialPostCount).Do(ctx)
    
    ebs.logger.Infof("Initial post count: %d", initialPostCount)
    
    maxScrolls := 20
    scrollDelay := 2 * time.Second
    
    for i := 0; i < maxScrolls; i++ {
        ebs.logger.Infof("Scroll attempt %d/%d", i+1, maxScrolls)
        
        // Scroll down gradually
        chromedp.Evaluate(`window.scrollBy(0, window.innerHeight * 0.8)`, nil).Do(ctx)
        chromedp.Sleep(scrollDelay).Do(ctx)
        
        // Every few scrolls, try to expand "See more" links
        if i % 3 == 0 {
            ebs.expandSeeMoreContent(ctx)
        }
        
        // Check for "Load More" or "See More Posts" buttons
        var hasLoadMore bool
        chromedp.Evaluate(`
            document.querySelector('a[href*="more"], button[aria-label*="more"], a[href*="show_older"]') !== null
        `, &hasLoadMore).Do(ctx)
        
        if hasLoadMore {
            ebs.logger.Info("Found load more button, clicking...")
            chromedp.Evaluate(`
                const button = document.querySelector('a[href*="more"], button[aria-label*="more"], a[href*="show_older"]');
                if (button) button.click();
            `, nil).Do(ctx)
            chromedp.Sleep(3 * time.Second).Do(ctx)
        }
        
        // Check current post count
        currentPostCount := 0
        chromedp.Evaluate(`document.querySelectorAll('[data-ft], [id*="story"], article, [role="article"]').length`, &currentPostCount).Do(ctx)
        
        ebs.logger.Infof("Current post count: %d", currentPostCount)
        
        // Check if we have posts from 5 days ago
        var hasOldPosts bool
        chromedp.Evaluate(`
            Array.from(document.querySelectorAll('abbr[data-utime], time')).some(el => {
                const timeAttr = el.getAttribute('data-utime') || el.getAttribute('datetime');
                if (timeAttr) {
                    const postTime = new Date(parseInt(timeAttr) * 1000 || timeAttr);
                    const fiveDaysAgo = new Date(Date.now() - 5 * 24 * 60 * 60 * 1000);
                    return postTime <= fiveDaysAgo;
                }
                return false;
            })
        `, &hasOldPosts).Do(ctx)
        
        if hasOldPosts {
            ebs.logger.Info("Found posts older than 5 days, stopping scroll")
            break
        }
        
        // If no new posts loaded for 3 consecutive scrolls, try different approach
        if currentPostCount == initialPostCount && i > 3 {
            ebs.logger.Info("No new posts loading, trying to find pagination")
            
            // Try clicking pagination links
            paginationScript := `
                const paginationLinks = document.querySelectorAll('a[href*="show_more"], a[href*="page"], a[href*="next"]');
                if (paginationLinks.length > 0) {
                    paginationLinks[0].click();
                    return true;
                }
                return false;
            `
            var clicked bool
            chromedp.Evaluate(paginationScript, &clicked).Do(ctx)
            
            if clicked {
                chromedp.Sleep(3 * time.Second).Do(ctx)
            }
        }
        
        initialPostCount = currentPostCount
    }
    
    // Final expansion of "See more" content
    ebs.expandSeeMoreContent(ctx)
    
    return nil
}

func (ebs *EnhancedBrowserScraper) infiniteScroll(ctx context.Context) error {
    ebs.logger.Info("Using infinite scroll strategy...")
    
    maxScrolls := 15
    for i := 0; i < maxScrolls; i++ {
        // Scroll to bottom
        chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil).Do(ctx)
        chromedp.Sleep(2 * time.Second).Do(ctx)
        
        // Wait for new content to load
        chromedp.Sleep(3 * time.Second).Do(ctx)
        
        // Every few scrolls, try to expand "See more" links
        if i % 3 == 0 {
            ebs.expandSeeMoreContent(ctx)
        }
        
        // Check if we've reached old enough posts
        var hasOldPosts bool
        chromedp.Evaluate(`
            const timeElements = document.querySelectorAll('[data-utime], time[datetime]');
            return Array.from(timeElements).some(el => {
                const timeStr = el.getAttribute('data-utime') || el.getAttribute('datetime');
                if (timeStr) {
                    const postTime = timeStr.includes('-') ? new Date(timeStr) : new Date(parseInt(timeStr) * 1000);
                    const fiveDaysAgo = new Date(Date.now() - 5 * 24 * 60 * 60 * 1000);
                    return postTime <= fiveDaysAgo;
                }
                return false;
            });
        `, &hasOldPosts).Do(ctx)
        
        if hasOldPosts {
            ebs.logger.Info("Reached posts from 5+ days ago")
            break
        }
    }
    
    // Final expansion of "See more" content
    ebs.expandSeeMoreContent(ctx)
    
    return nil
}

func (ebs *EnhancedBrowserScraper) clickSeeMoreButtons(ctx context.Context) error {
    ebs.logger.Info("Using click See More strategy...")
    
    maxClicks := 10
    for i := 0; i < maxClicks; i++ {
        // Look for various "See More" button patterns
        seeMoreSelectors := []string{
            `a[href*="show_older"]`,
            `a[href*="bacr"]`,
            `a[href*="more"]`,
            `a:contains("See More")`,
            `a:contains("Show older")`,
            `a:contains("Load more")`,
            `button:contains("See more")`,
        }
        
        clickedSomething := false
        
        for _, selector := range seeMoreSelectors {
            var exists bool
            script := fmt.Sprintf(`document.querySelector('%s') !== null`, selector)
            
            if err := chromedp.Evaluate(script, &exists).Do(ctx); err != nil || !exists {
                continue
            }
            
            ebs.logger.Infof("Found '%s' button, clicking...", selector)
            
            // Use JavaScript to click as it's more reliable
            clickScript := fmt.Sprintf(`
                const el = document.querySelector('%s');
                if (el) {
                    el.click();
                    return true;
                }
                return false;
            `, selector)
            
            var clicked bool
            if err := chromedp.Evaluate(clickScript, &clicked).Do(ctx); err == nil && clicked {
                clickedSomething = true
                ebs.logger.Info("Successfully clicked button")
                chromedp.Sleep(3 * time.Second).Do(ctx)
                break
            }
        }
        
        if !clickedSomething {
            ebs.logger.Info("No more 'See More' buttons found")
            break
        }
        
        // Expand any "See more" content after clicking load more
        ebs.expandSeeMoreContent(ctx)
        
        // Check if we have enough old posts
        var hasOldPosts bool
        chromedp.Evaluate(`
            Array.from(document.querySelectorAll('abbr[data-utime]')).some(el => {
                const timestamp = parseInt(el.getAttribute('data-utime'));
                const postTime = new Date(timestamp * 1000);
                const fiveDaysAgo = new Date(Date.now() - 5 * 24 * 60 * 60 * 1000);
                return postTime <= fiveDaysAgo;
            })
        `, &hasOldPosts).Do(ctx)
        
        if hasOldPosts {
            ebs.logger.Info("Found posts from 5+ days ago")
            break
        }
    }
    
    return nil
}

func (ebs *EnhancedBrowserScraper) expandSeeMoreContent(ctx context.Context) {
    ebs.logger.Debug("Expanding 'See more' content...")
    
    // Find and click all "See more" links using JavaScript
    script := `
    function expandSeeMoreLinks() {
        const seeMoreSelectors = [
            'div[role="button"]:contains("See more")',
            'a:contains("See more")',
            'a:contains("See More")',
            'span:contains("See more")',
            'div[data-sigil="expose"]'
        ];
        
        let clicked = 0;
        
        for (const selector of seeMoreSelectors) {
            const elements = document.querySelectorAll(selector);
            for (const el of elements) {
                try {
                    el.click();
                    clicked++;
                } catch (e) {
                    // Ignore errors
                }
            }
        }
        
        return clicked;
    }
    
    expandSeeMoreLinks();
    `
    
    var clickCount int
    if err := chromedp.Evaluate(script, &clickCount).Do(ctx); err == nil && clickCount > 0 {
        ebs.logger.Infof("Expanded %d 'See more' links", clickCount)
        chromedp.Sleep(500 * time.Millisecond).Do(ctx)
    }
}

func (ebs *EnhancedBrowserScraper) Close() {
    if ebs.cancel != nil {
        ebs.cancel()
    }
}



func truncateString(s string, length int) string {
    if len(s) <= length {
        return s
    }
    return s[:length] + "..."
}


// extractPosts extracts all posts from the document
func (fs *FacebookScraper) extractPosts(doc *goquery.Document, groupID, groupName string) []types.ScrapedPost {
    var posts []types.ScrapedPost
    
    // List of all possible selectors for post containers in different Facebook versions
    postContainerSelectors := []string{
        "div[role='article']",                     // Standard Facebook article
        "div[data-testid='post_container']",       // Current Facebook post container
        "div[data-ft*='top_level_post_id']",       // Post with data-ft attribute
        "div.story_body_container",                // Mobile Facebook post container
        "div[id^='structured_composer_async_']",   // Another post container pattern
        "article._55wo._5rgr._5gh8",               // Facebook mobile articles
        "div.userContentWrapper",                  // Classic Facebook post container
        "div[id^='m_story_permalink_view']",       // Mobile story permalink
        "div[data-store*='story_fbid']",           // Story with data-store
    }
    
    // Try multiple methods of post extraction
    for _, selector := range postContainerSelectors {
        elements := doc.Find(selector)
        fs.logger.Debugf("Found %d potential posts with selector '%s'", elements.Length(), selector)
        
        // Skip if we found no posts with this selector
        if elements.Length() == 0 {
            continue
        }
        
        elements.Each(func(i int, s *goquery.Selection) {
            // Extract post data
            post := fs.extractSinglePost(s, groupID, groupName)
            if post != nil && fs.isValidPost(post) {
                posts = append(posts, *post)
            }
        })
        
        // If we found posts with this selector, no need to try others
        if len(posts) > 0 {
            break
        }
    }
    
    // If no posts found with main selectors, try fallback method
    if len(posts) == 0 {
        posts = fs.extractPostsFallback(doc, groupID, groupName)
    }
    
    fs.logger.Infof("Extracted %d posts total from group %s", len(posts), groupName)
    return posts
}

// extractPostsFallback attempts to find posts when main selectors fail
func (fs *FacebookScraper) extractPostsFallback(doc *goquery.Document, groupID, groupName string) []types.ScrapedPost {
    var posts []types.ScrapedPost
    processedIDs := make(map[string]bool)
    
    // Look for elements that might contain post content
    contentIndicators := []string{
        "div[data-ft]",                // Elements with Facebook tracking data
        "div[id^='feed_subtitle_']",   // Feed subtitle elements (often part of posts)
        "span[dir='auto']",            // Text direction auto spans (often content)
        "div.story_body_container",    // Mobile story containers
        "div._5rgt",                   // Common post text container class
    }
    
    for _, indicator := range contentIndicators {
        doc.Find(indicator).Each(func(i int, s *goquery.Selection) {
            // Find the parent element that might be a post container
            postContainer := s.ParentsFiltered("div[id]").First()
            if postContainer.Length() == 0 {
                return
            }
            
            id, exists := postContainer.Attr("id")
            if !exists || processedIDs[id] {
                return
            }
            processedIDs[id] = true
            
            // Check if this looks like a post
            if fs.looksLikePostContainer(postContainer) {
                post := fs.extractSinglePost(postContainer, groupID, groupName)
                if post != nil && fs.isValidPost(post) {
                    posts = append(posts, *post)
                }
            }
        })
    }
    
    return posts
}

// looksLikePostContainer checks if an element looks like it contains a post
func (fs *FacebookScraper) looksLikePostContainer(s *goquery.Selection) bool {
    // Check for post-specific attributes
    if dataFt, exists := s.Attr("data-ft"); exists {
        if strings.Contains(dataFt, "top_level_post_id") || 
           strings.Contains(dataFt, "story_fbid") ||
           strings.Contains(dataFt, "content_id") {
            return true
        }
    }
    
    // Check for post ID in element ID
    if id, exists := s.Attr("id"); exists {
        if strings.Contains(id, "story_") || 
           strings.Contains(id, "post_") || 
           strings.Contains(id, "feed_") {
            return true
        }
    }
    
    // Check for common post components
    hasAuthor := s.Find("h3 a, strong a, a[data-hovercard]").Length() > 0
    hasTimestamp := s.Find("abbr[data-utime], span[data-utime], a[href*='permalink']").Length() > 0
    hasContent := s.Find("span[dir='auto'], div[dir='auto'], p, div.userContent").Length() > 0
    hasEngagement := s.Find("[aria-label*='Like'], [aria-label*='Comment'], [aria-label*='Share']").Length() > 0
    
    return (hasAuthor && hasContent) || (hasTimestamp && hasEngagement)
}

// extractSinglePost extracts a single post from a post container element
func (fs *FacebookScraper) extractSinglePost(s *goquery.Selection, groupID, groupName string) *types.ScrapedPost {
    post := &types.ScrapedPost{
        GroupID:   groupID,
        Images:    []types.MediaItem{},
        Videos:    []types.MediaItem{},
        Mentions:  []string{},
        Hashtags:  []string{},
        Links:     []string{},
    }
    
    // Extract post ID using multiple methods
    post.ID = fs.extractPostID(s, groupID)
    
    // Extract post content (full text, not just "See more")
    post.Content = fs.extractPostContent(s)
    
    // Extract author name
    post.AuthorName = fs.extractAuthorName(s)
    
    // Extract post URL
    post.URL = fs.constructPostURL(groupID, post.ID)
    
    // Extract timestamp
    fs.extractTimestamp(s, post)
    
    // Extract engagement metrics (likes, comments, shares)
    fs.extractEngagementMetrics(s, post)
    
    // Extract media content
    fs.extractImages(s, post)
    fs.extractVideos(s, post)
    
    // Extract mentions, hashtags, links
    fs.extractTextualEntities(s, post)
    
    // Set post type and media count
    fs.determinePostTypeAndMedia(post)
    
    if fs.isValidPost(post) {
        fs.logger.Debugf("Found post: ID=%s, Author=%s, Likes=%d, Comments=%d, Time=%s, Content=%s",
            post.ID, post.AuthorName, post.LikesCount, post.CommentsCount, 
            post.PostTime.Format("2006-01-02"), truncateString(post.Content, 50))
        return post
    }
    
    return nil
}

// extractPostID extracts the post ID using various methods
func (fs *FacebookScraper) extractPostID(s *goquery.Selection, groupID string) string {
    // Method 1: Extract from data-ft JSON attribute
    if dataFt, exists := s.Attr("data-ft"); exists {
        var dataFtObj map[string]interface{}
        if err := json.Unmarshal([]byte(dataFt), &dataFtObj); err == nil {
            // Try different keys for post ID
            for _, key := range []string{"top_level_post_id", "content_id", "story_fbid"} {
                if postID, ok := dataFtObj[key].(string); ok && postID != "" {
                    return postID
                }
            }
        }
    }
    
    // Method 2: Extract from data-store attribute
    if dataStore, exists := s.Attr("data-store"); exists {
        var dataStoreObj map[string]interface{}
        if err := json.Unmarshal([]byte(dataStore), &dataStoreObj); err == nil {
            if storyID, ok := dataStoreObj["story_fbid"].(string); ok && storyID != "" {
                return storyID
            }
        }
    }
    
    // Method 3: Extract from permalink or post URL
    var postIDFromLink string
    s.Find("a[href*='/permalink/'], a[href*='/posts/'], a[href*='story_fbid=']").Each(func(i int, link *goquery.Selection) {
        if postIDFromLink != "" {
            return
        }
        href, exists := link.Attr("href")
        if !exists {
            return
        }
        
        // Try to extract post ID from different URL patterns
        patterns := []string{
            `permalink/(\d+)`,
            `posts/(\d+)`,
            `story_fbid=(\d+)`,
            `fbid=(\d+)`,
        }
        
        for _, pattern := range patterns {
            re := regexp.MustCompile(pattern)
            if matches := re.FindStringSubmatch(href); len(matches) > 1 {
                postIDFromLink = matches[1]
                return
            }
        }
    })
    if postIDFromLink != "" {
        return postIDFromLink
    }
    
    // Method 4: Extract from element ID
    if id, exists := s.Attr("id"); exists {
        // Common patterns: hyperfeed_story_id_5e8d7f6b9c6a1d3e7b5f2a1c
        re := regexp.MustCompile(`[a-z]+_[a-z]+_id_([a-f0-9]+)`)
        if matches := re.FindStringSubmatch(id); len(matches) > 1 {
            return matches[1]
        }
    }
    
    // Method 5: Generate a unique ID if all else fails
    return fmt.Sprintf("%s_%d", groupID, time.Now().UnixNano())
}

// extractPostContent gets the full content of a post, not just "See more" text
func (fs *FacebookScraper) extractPostContent(s *goquery.Selection) string {
    contentSelectors := []string{
        "div[data-testid='post_message']",         // Modern Facebook post message
        "div[data-ad-preview='message']",          // Ad preview message
        "span[dir='auto']",                        // Auto direction span (often contains post text)
        "div.userContent",                         // Classic Facebook user content
        "p",                                       // Paragraph tags
        "div.story_body_container > div",          // Mobile story body container
        "div[dir='auto']",                         // Auto direction div
    }
    
    var fullContent strings.Builder
    
    for _, selector := range contentSelectors {
        s.Find(selector).Each(func(i int, element *goquery.Selection) {
            // Skip if this is a reaction element or comment
            class := element.AttrOr("class", "")
            if strings.Contains(class, "comment") || 
               strings.Contains(class, "reaction") || 
               strings.Contains(class, "uiPopover") {
                return
            }
            
            // Get the text content
            text := strings.TrimSpace(element.Text())
            if text != "" && 
               !strings.Contains(text, "See more") && 
               !strings.Contains(text, "See More") {
                
                if fullContent.Len() > 0 {
                    fullContent.WriteString(" ")
                }
                fullContent.WriteString(text)
            }
        })
        
        // If we found content, break out of the loop
        if fullContent.Len() > 0 {
            break
        }
    }
    
    content := fullContent.String()
    
    // Clean up the content
    content = strings.ReplaceAll(content, "See more", "")
    content = strings.ReplaceAll(content, "See More", "")
    content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
    content = strings.TrimSpace(content)
    
    return content
}

// extractAuthorName extracts the post author's name
func (fs *FacebookScraper) extractAuthorName(s *goquery.Selection) string {
    authorSelectors := []string{
        "h3 a",                                    // Mobile Facebook profile link
        "strong a",                                // Another profile link pattern
        "[data-testid='story-subtitle'] a",        // Modern Facebook story subtitle
        "a[data-hovercard]",                       // Profile with hovercard
        "h5 a",                                    // Another header link
        ".profileLink",                            // Profile link class
    }
    
    for _, selector := range authorSelectors {
        author := s.Find(selector).First()
        if author.Length() == 0 {
            continue
        }
        
        name := strings.TrimSpace(author.Text())
        
        // Skip if this doesn't look like a name
        if name == "" || 
           len(name) > 100 || 
           strings.HasPrefix(name, "http") ||
           strings.Contains(strings.ToLower(name), "like") ||
           strings.Contains(strings.ToLower(name), "comment") {
            continue
        }
        
        return name
    }
    
    return ""
}

// constructPostURL creates a URL for the post
func (fs *FacebookScraper) constructPostURL(groupID, postID string) string {
    if postID == "" {
        return ""
    }
    
    return fmt.Sprintf("https://www.facebook.com/groups/%s/posts/%s", groupID, postID)
}

// extractTimestamp extracts and parses the post timestamp
func (fs *FacebookScraper) extractTimestamp(s *goquery.Selection, post *types.ScrapedPost) {
    // Find timestamp elements
    timestampSelectors := []string{
        "abbr[data-utime]",                        // Unix timestamp in data-utime
        "span[data-utime]",                        // Alternative unix timestamp
        "a[href*='permalink'] span",               // Permalink timestamp span
        "[data-testid='story-subtitle'] a",        // Modern timestamp in subtitle
        "span.timestampContent",                   // Classic timestamp content
        "time[datetime]",                          // HTML5 time element
    }
    
    for _, selector := range timestampSelectors {
        timestamp := s.Find(selector).First()
        if timestamp.Length() == 0 {
            continue
        }
        
        // Method 1: data-utime attribute (unix timestamp)
        if utime, exists := timestamp.Attr("data-utime"); exists {
            if ts, err := strconv.ParseInt(utime, 10, 64); err == nil {
                post.PostTime = time.Unix(ts, 0)
                return
            }
        }
        
        // Method 2: datetime attribute
        if datetime, exists := timestamp.Attr("datetime"); exists {
            if t, err := time.Parse(time.RFC3339, datetime); err == nil {
                post.PostTime = t
                return
            }
        }
        
        // Method 3: Parse relative time text
        timeText := timestamp.Text()
        if relativeParsed := fs.parseRelativeTime(timeText); !relativeParsed.IsZero() {
            post.PostTime = relativeParsed
            return
        }
    }
}

// extractEngagementMetrics extracts likes, comments, and shares
func (fs *FacebookScraper) extractEngagementMetrics(s *goquery.Selection, post *types.ScrapedPost) {
    // Extract likes
    likeSelectors := []string{
        "[aria-label*='Like']",                    // Like aria-label
        "[aria-label*='reaction']",                // Reaction aria-label
        "a[href*='reaction/profile']",             // Reaction profile link
        "span[data-testid*='reaction']",           // Reaction test ID
        "a[href*='ufi/reaction']",                 // Another reaction link format
    }
    
    for _, selector := range likeSelectors {
        likeElement := s.Find(selector).First()
        if likeElement.Length() == 0 {
            continue
        }
        
        // Try to get count from aria-label
        if ariaLabel, exists := likeElement.Attr("aria-label"); exists {
            if likeCount := fs.extractNumberFromText(ariaLabel); likeCount > 0 {
                post.LikesCount = likeCount
                break
            }
        }
        
        // Try to get count from text content
        likeText := likeElement.Text()
        if likeCount := fs.extractNumberFromText(likeText); likeCount > 0 {
            post.LikesCount = likeCount
            break
        }
    }
    
    // Extract comments - fixed to get accurate comment counts
    commentSelectors := []string{
        "[aria-label*='comment']",                 // Comment aria-label
        "a[href*='comment/']",                     // Comment link
        "a[href*='comment_id']",                   // Comment ID link
        "span[data-testid*='comment']",            // Comment test ID
        "a[href*='ufi/commenting']",               // Another comment link format
    }
    
    for _, selector := range commentSelectors {
        commentElement := s.Find(selector).First()
        if commentElement.Length() == 0 {
            continue
        }
        
        // Try to get count from aria-label
        if ariaLabel, exists := commentElement.Attr("aria-label"); exists {
            if commentCount := fs.extractNumberFromText(ariaLabel); commentCount > 0 {
                post.CommentsCount = commentCount
                break
            }
        }
        
        // Try to get count from text content
        commentText := commentElement.Text()
        if commentCount := fs.extractNumberFromText(commentText); commentCount > 0 {
            post.CommentsCount = commentCount
            break
        }
    }
    
    // Extract shares
    shareSelectors := []string{
        "[aria-label*='share']",                   // Share aria-label
        "a[href*='sharer/']",                      // Share link
        "span[data-testid*='share']",              // Share test ID
    }
    
    for _, selector := range shareSelectors {
        shareElement := s.Find(selector).First()
        if shareElement.Length() == 0 {
            continue
        }
        
        // Try to get count from aria-label
        if ariaLabel, exists := shareElement.Attr("aria-label"); exists {
            if shareCount := fs.extractNumberFromText(ariaLabel); shareCount > 0 {
                post.SharesCount = shareCount
                break
            }
        }
        
        // Try to get count from text content
        shareText := shareElement.Text()
        if shareCount := fs.extractNumberFromText(shareText); shareCount > 0 {
            post.SharesCount = shareCount
            break
        }
    }
}

// extractImages extracts image URLs from the post
func (fs *FacebookScraper) extractImages(s *goquery.Selection, post *types.ScrapedPost) {
    // Find image elements
    imageSelectors := []string{
        "img[src*='scontent']",                    // Facebook CDN images
        "img[src*='fbcdn']",                       // Facebook CDN images
        "a[href*='/photo/'] img",                  // Photo link images
        "div[data-testid*='photo'] img",           // Photo test ID images
    }
    
    for _, selector := range imageSelectors {
        s.Find(selector).Each(func(i int, img *goquery.Selection) {
            // Get image URL
            src, exists := img.Attr("src")
            if !exists {
                // Try data-src for lazy-loaded images
                src, exists = img.Attr("data-src")
                if !exists {
                    return
                }
            }
            
            // Skip small images, profile pics, etc.
            width, _ := strconv.Atoi(img.AttrOr("width", "0"))
            height, _ := strconv.Atoi(img.AttrOr("height", "0"))
            
            // Skip tiny images and emoticons
            if (width > 0 && width < 50) || (height > 0 && height < 50) {
                return
            }
            
            // Skip if URL contains avatar, profile, emoji indicators
            lowerSrc := strings.ToLower(src)
            if strings.Contains(lowerSrc, "avatar") || 
               strings.Contains(lowerSrc, "profile") || 
               strings.Contains(lowerSrc, "emoji") || 
               strings.Contains(lowerSrc, "emoticon") {
                return
            }
            
            // Create image media item
            mediaItem := types.MediaItem{
                URL:         src,
                Type:        "image",
                Width:       width,
                Height:      height,
                Description: img.AttrOr("alt", ""),
            }
            
            // Check for duplicates
            if !fs.mediaItemExists(post.Images, mediaItem) {
                post.Images = append(post.Images, mediaItem)
            }
        })
    }
}

// extractVideos extracts video URLs from the post
func (fs *FacebookScraper) extractVideos(s *goquery.Selection, post *types.ScrapedPost) {
    // Find video elements
    videoSelectors := []string{
        "video[src]",                              // Direct video elements
        "video source[src]",                       // Video sources
        "a[href*='/video/']",                      // Video links
        "div[data-testid*='video-attachment']",    // Video attachments
    }
    
    for _, selector := range videoSelectors {
        s.Find(selector).Each(func(i int, el *goquery.Selection) {
            var videoURL string
            
            // Get video URL depending on element type
            if el.Is("video") {
                videoURL = el.AttrOr("src", "")
            } else if el.Is("source") {
                videoURL = el.AttrOr("src", "")
            } else if el.Is("a") {
                href := el.AttrOr("href", "")
                if strings.Contains(href, "/video/") {
                    videoURL = href
                    if !strings.HasPrefix(videoURL, "http") {
                        videoURL = "https://www.facebook.com" + videoURL
                    }
                }
            } else {
                // Try to find video ID in data attributes
                dataStore := el.AttrOr("data-store", "")
                var dataObj map[string]interface{}
                if err := json.Unmarshal([]byte(dataStore), &dataObj); err == nil {
                    if videoID, ok := dataObj["videoID"].(string); ok && videoID != "" {
                        videoURL = fmt.Sprintf("https://www.facebook.com/video/video.php?v=%s", videoID)
                    }
                }
            }
            
            if videoURL != "" {
                // Create video media item
                mediaItem := types.MediaItem{
                    URL:  videoURL,
                    Type: "video",
                }
                
                // Try to get thumbnail
                thumbnail := el.Find("img").First()
                if thumbnail.Length() > 0 {
                    mediaItem.Thumbnail = thumbnail.AttrOr("src", "")
                }
                
                // Check for duplicates
                if !fs.mediaItemExists(post.Videos, mediaItem) {
                    post.Videos = append(post.Videos, mediaItem)
                }
            }
        })
    }
}

// extractTextualEntities extracts mentions, hashtags, and links from post content
func (fs *FacebookScraper) extractTextualEntities(s *goquery.Selection, post *types.ScrapedPost) {
    // Extract mentions
    s.Find("a[href*='/user/'], a[href*='/profile.php'], a[data-hovercard*='user']").Each(func(i int, mention *goquery.Selection) {
        mentionText := strings.TrimSpace(mention.Text())
        if mentionText != "" && !fs.stringInSlice(mentionText, post.Mentions) {
            post.Mentions = append(post.Mentions, mentionText)
        }
    })
    
    // Extract hashtags
    s.Find("a[href*='/hashtag/']").Each(func(i int, hashtag *goquery.Selection) {
        hashtagText := strings.TrimSpace(hashtag.Text())
        if hashtagText != "" && strings.HasPrefix(hashtagText, "#") {
            if !fs.stringInSlice(hashtagText, post.Hashtags) {
                post.Hashtags = append(post.Hashtags, hashtagText)
            }
        }
    })
    
    // Also extract hashtags from content using regex
    if post.Content != "" {
        hashtagPattern := regexp.MustCompile(`#(\w+)`)
        matches := hashtagPattern.FindAllString(post.Content, -1)
        for _, match := range matches {
            if !fs.stringInSlice(match, post.Hashtags) {
                post.Hashtags = append(post.Hashtags, match)
            }
        }
    }
    
    // Extract links
    s.Find("a[href*='http']:not([href*='facebook.com']):not([href*='profile']):not([href*='hashtag'])").Each(func(i int, link *goquery.Selection) {
        href := link.AttrOr("href", "")
        if href != "" && strings.HasPrefix(href, "http") {
            // Clean up Facebook's redirect links
            if strings.Contains(href, "l.facebook.com/l.php?u=") {
                u, err := url.Parse(href)
                if err == nil {
                    q := u.Query()
                    if redirectURL := q.Get("u"); redirectURL != "" {
                        decodedURL, err := url.QueryUnescape(redirectURL)
                        if err == nil {
                            href = decodedURL
                        }
                    }
                }
            }
            
            if !fs.stringInSlice(href, post.Links) {
                post.Links = append(post.Links, href)
            }
        }
    })
}

// determinePostTypeAndMedia sets the post type and counts media items
func (fs *FacebookScraper) determinePostTypeAndMedia(post *types.ScrapedPost) {
    imageCount := len(post.Images)
    videoCount := len(post.Videos)
    
    post.MediaCount = imageCount + videoCount
    
    // Determine post type based on content
    if videoCount > 0 && imageCount > 0 {
        post.PostType = "mixed"
    } else if videoCount > 0 {
        post.PostType = "video"
    } else if imageCount > 0 {
        post.PostType = "image"
    } else if len(post.Links) > 0 {
        post.PostType = "link"
    } else {
        post.PostType = "text"
    }
}

// isValidPost checks if the post has enough data to be considered valid
func (fs *FacebookScraper) isValidPost(post *types.ScrapedPost) bool {
    // Must have an ID and at least one of content, author, or engagement
    return post.ID != "" && 
           (post.Content != "" || post.AuthorName != "" || post.LikesCount > 0 || post.MediaCount > 0)
}

// parseRelativeTime converts relative time text like "2 hours ago" to a timestamp
func (fs *FacebookScraper) parseRelativeTime(text string) time.Time {
    text = strings.ToLower(strings.TrimSpace(text))
    now := time.Now()
    
    // Handle "X minutes/hours/days ago" format
    re := regexp.MustCompile(`(\d+)\s*(minute|hour|day|week|month|year)s?\s*ago`)
    matches := re.FindStringSubmatch(text)
    
    if len(matches) >= 3 {
        count, err := strconv.Atoi(matches[1])
        if err != nil {
            return time.Time{}
        }
        
        unit := matches[2]
        switch unit {
        case "minute":
            return now.Add(-time.Duration(count) * time.Minute)
        case "hour":
            return now.Add(-time.Duration(count) * time.Hour)
        case "day":
            return now.AddDate(0, 0, -count)
        case "week":
            return now.AddDate(0, 0, -count*7)
        case "month":
            return now.AddDate(0, -count, 0)
        case "year":
            return now.AddDate(-count, 0, 0)
        }
    }
    
    // Handle "yesterday", "today" format
    if strings.Contains(text, "yesterday") {
        return now.AddDate(0, 0, -1)
    } else if strings.Contains(text, "today") {
        return now
    }
    
    // Handle "Monday", "Tuesday", etc. (assume within the last week)
    daysOfWeek := map[string]int{
        "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, 
        "friday": 5, "saturday": 6, "sunday": 0,
    }
    
    for day, weekday := range daysOfWeek {
        if strings.Contains(text, day) {
            today := int(now.Weekday())
            diff := (today - weekday + 7) % 7
            if diff == 0 {
                diff = 7 // If it's the same day of the week, assume last week
            }
            return now.AddDate(0, 0, -diff)
        }
    }
    
    return time.Time{}
}

// extractNumberFromText parses numbers from text, handling formats like "1.2K", "5,300", etc.
func (fs *FacebookScraper) extractNumberFromText(text string) int {
    text = strings.ToLower(strings.TrimSpace(text))
    
    // Handle formats like "1.2K", "5.4M"
    reWithSuffix := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([km])`)
    if matches := reWithSuffix.FindStringSubmatch(text); len(matches) >= 3 {
        num, _ := strconv.ParseFloat(matches[1], 64)
        switch matches[2] {
        case "k":
            return int(num * 1000)
        case "m":
            return int(num * 1000000)
        }
    }
    
    // Handle formats like "5,300", "1,234"
    reWithComma := regexp.MustCompile(`(\d{1,3}(?:,\d{3})+)`)
    if matches := reWithComma.FindStringSubmatch(text); len(matches) >= 2 {
        numStr := strings.ReplaceAll(matches[1], ",", "")
        if num, err := strconv.Atoi(numStr); err == nil {
            return num
        }
    }
    
    // Handle simple numeric strings
    reSimple := regexp.MustCompile(`(\d+)`)
    if matches := reSimple.FindStringSubmatch(text); len(matches) >= 2 {
        if num, err := strconv.Atoi(matches[1]); err == nil {
            return num
        }
    }
    
    return 0
}

// mediaItemExists checks if an item exists in a media item slice
func (fs *FacebookScraper) mediaItemExists(items []types.MediaItem, item types.MediaItem) bool {
    for _, existing := range items {
        if existing.URL == item.URL {
            return true
        }
    }
    return false
}

// stringInSlice checks if a string exists in a slice
func (fs *FacebookScraper) stringInSlice(s string, slice []string) bool {
    for _, item := range slice {
        if item == s {
            return true
        }
    }
    return false
}

// removeDuplicatePosts removes duplicate posts by ID
func (fs *FacebookScraper) removeDuplicatePosts(posts []types.ScrapedPost) []types.ScrapedPost {
    seen := make(map[string]bool)
    var unique []types.ScrapedPost
    
    for _, post := range posts {
        key := post.ID
        if !seen[key] {
            seen[key] = true
            unique = append(unique, post)
        }
    }
    
    return unique
}

// saveScrapedPost saves a post to the database
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
    
    // Create database post model
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

// setHeaders sets HTTP headers for requests
func (fs *FacebookScraper) setHeaders(req *http.Request) {
    req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36")
    req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
    req.Header.Set("Accept-Language", "en-US,en;q=0.9")
    req.Header.Set("Accept-Encoding", "gzip, deflate, br")
    req.Header.Set("DNT", "1")
    req.Header.Set("Connection", "keep-alive")
    req.Header.Set("Upgrade-Insecure-Requests", "1")
    req.Header.Set("Sec-Fetch-Dest", "document")
    req.Header.Set("Sec-Fetch-Mode", "navigate")
    req.Header.Set("Sec-Fetch-Site", "none")
    req.Header.Set("Sec-Fetch-User", "?1")
    req.Header.Set("Cache-Control", "max-age=0")
}

// Close cleans up resources used by the scraper
func (fs *FacebookScraper) Close() error {
    // Save cookies for future sessions
    if err := fs.authManager.SaveCookies(); err != nil {
        fs.logger.Warnf("Failed to save cookies: %v", err)
    }

    // No browser context to close here; handled elsewhere if needed

    return nil
}
