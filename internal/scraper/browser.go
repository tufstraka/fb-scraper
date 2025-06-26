package scraper

/*import (
    "context"
    "fmt"
    "strings"
    "time"

    "github.com/chromedp/cdproto/network"
    "github.com/chromedp/chromedp"
    "github.com/sirupsen/logrus"
)

func NewBrowserScraper(logger *logrus.Logger) *EnhancedBrowserScraper {
    opts := append(chromedp.DefaultExecAllocatorOptions[:],
        chromedp.Flag("headless", false), // Set to true for production
        chromedp.Flag("disable-gpu", true),
        chromedp.Flag("disable-web-security", true),
        chromedp.Flag("disable-features", "VizDisplayCompositor"),
        chromedp.Flag("no-sandbox", true),
        chromedp.Flag("disable-dev-shm-usage", true),
        chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
    )

    allocCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
    ctx, cancel := chromedp.NewContext(allocCtx)

    return &EnhancedBrowserScraper{
        logger: logger,
        ctx:    ctx,
        cancel: cancel,
    }
}

func (ebs *EnhancedBrowserScraper) ScrapeGroupWithScrolling(groupID string, cookies []Cookie) (string, error) {
    ebs.logger.Infof("Starting enhanced browser scraping for group %s", groupID)

    // Multiple URL strategies
    urls := []string{
        fmt.Sprintf("https://m.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://mbasic.facebook.com/groups/%s", groupID),
        fmt.Sprintf("https://www.facebook.com/groups/%s", groupID),
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
    
    err := chromedp.Run(ebs.ctx,
        chromedp.Navigate(url),
        chromedp.WaitVisible("body", chromedp.ByQuery),
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
        
        // Reload with cookies
        chromedp.Navigate(url),
        chromedp.Sleep(3*time.Second),
        
        // Handle different loading scenarios
        ebs.handleDynamicLoading(),
        
        // Get final HTML
        chromedp.OuterHTML("html", &html),
    )

    return html, err
}

func (ebs *EnhancedBrowserScraper) handleDynamicLoading() chromedp.Action {
    return chromedp.ActionFunc(func(ctx context.Context) error {
        ebs.logger.Info("Starting dynamic content loading...")

        // Strategy 1: Progressive scrolling for mobile Facebook
        if err := ebs.progressiveScroll(ctx); err == nil {
            return nil
        }

        // Strategy 2: Click "See More" buttons for mbasic
        if err := ebs.clickSeeMoreButtons(ctx); err == nil {
            return nil
        }

        // Strategy 3: Infinite scroll for desktop
        return ebs.infiniteScroll(ctx)
    })
}

func (ebs *EnhancedBrowserScraper) progressiveScroll(ctx context.Context) error {
    ebs.logger.Info("Trying progressive scroll strategy...")
    
    // Wait for initial content
    if err := chromedp.WaitVisible("div", chromedp.ByQuery).Do(ctx); err != nil {
        return err
    }

    initialPostCount := 0
    chromedp.Evaluate(`document.querySelectorAll('[data-ft], [id*="story"], article, [role="article"]').length`, &initialPostCount).Do(ctx)
    
    ebs.logger.Infof("Initial post count: %d", initialPostCount)
    
    //targetDays := 5
    maxScrolls := 20
    scrollDelay := 2 * time.Second
    
    for i := 0; i < maxScrolls; i++ {
        ebs.logger.Infof("Scroll attempt %d/%d", i+1, maxScrolls)
        
        // Scroll down gradually
        chromedp.Evaluate(`window.scrollBy(0, window.innerHeight * 0.8)`, nil).Do(ctx)
        chromedp.Sleep(scrollDelay).Do(ctx)
        
        // Check for "Load More" or "See More Posts" buttons
        var hasLoadMore bool
        chromedp.Evaluate(`
            document.querySelector('a[href*="more"], button[aria-label*="more"], a[href*="show_older"]') !== null
        `, &hasLoadMore).Do(ctx)
        
        if hasLoadMore {
            ebs.logger.Info("Found load more button, clicking...")
            chromedp.Click(`a[href*="more"], button[aria-label*="more"], a[href*="show_older"]`, chromedp.ByQuery).Do(ctx)
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
            return ebs.handlePagination(ctx)
        }
        
        initialPostCount = currentPostCount
    }
    
    return nil
}

func (ebs *EnhancedBrowserScraper) clickSeeMoreButtons(ctx context.Context) error {
    ebs.logger.Info("Trying click See More strategy...")
    
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
        
        clicked := false
        for _, selector := range seeMoreSelectors {
            var exists bool
            chromedp.Evaluate(fmt.Sprintf(`document.querySelector('%s') !== null`, selector), &exists).Do(ctx)
            
            if exists {
                ebs.logger.Infof("Clicking see more button with selector: %s", selector)
                if err := chromedp.Click(selector, chromedp.ByQuery).Do(ctx); err == nil {
                    clicked = true
                    chromedp.Sleep(3 * time.Second).Do(ctx)
                    break
                }
            }
        }
        
        if !clicked {
            ebs.logger.Info("No more 'See More' buttons found")
            break
        }
        
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

func (ebs *EnhancedBrowserScraper) infiniteScroll(ctx context.Context) error {
    ebs.logger.Info("Trying infinite scroll strategy...")
    
    maxScrolls := 15
    for i := 0; i < maxScrolls; i++ {
        // Scroll to bottom
        chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil).Do(ctx)
        chromedp.Sleep(2 * time.Second).Do(ctx)
        
        // Wait for new content to load
        chromedp.Sleep(3 * time.Second).Do(ctx)
        
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
    
    return nil
}

func (ebs *EnhancedBrowserScraper) handlePagination(ctx context.Context) error {
    ebs.logger.Info("Looking for pagination controls...")
    
    // Common pagination selectors
    paginationSelectors := []string{
        `a[href*="next"]`,
        `a[href*="page"]`,
        `button[aria-label*="Next"]`,
        `.pagination a`,
        `a[rel="next"]`,
    }
    
    for _, selector := range paginationSelectors {
        var exists bool
        chromedp.Evaluate(fmt.Sprintf(`document.querySelector('%s') !== null`, selector), &exists).Do(ctx)
        
        if exists {
            ebs.logger.Infof("Found pagination with selector: %s", selector)
            chromedp.Click(selector, chromedp.ByQuery).Do(ctx)
            chromedp.Sleep(3 * time.Second).Do(ctx)
            return nil
        }
    }
    
    return fmt.Errorf("no pagination found")
}

func (ebs *EnhancedBrowserScraper) Close() {
    if ebs.cancel != nil {
        ebs.cancel()
    }
}*/