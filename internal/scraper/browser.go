package scraper

import (
    "context"
    "fmt"
    "os/exec"
    "time"

    "github.com/chromedp/cdproto/network"
    "github.com/chromedp/chromedp"
    "github.com/sirupsen/logrus"
)

type BrowserScraper struct {
    ctx    context.Context
    cancel context.CancelFunc
    logger *logrus.Logger
}

func NewBrowserScraper(logger *logrus.Logger) *BrowserScraper {
    // Try Firefox first, fallback to Chrome
    opts := []chromedp.ExecAllocatorOption{}
    
    // Check if Firefox is available
    if isFirefoxAvailable() {
        logger.Info("Using Firefox for browser automation")
        opts = append(chromedp.DefaultExecAllocatorOptions[:],
            chromedp.ExecPath("firefox"),
            chromedp.Flag("headless", true),
            chromedp.Flag("disable-gpu", true),
            chromedp.Flag("no-sandbox", true),
            chromedp.Flag("disable-dev-shm-usage", true),
            chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64; rv:91.0) Gecko/20100101 Firefox/91.0"),
        )
    } else if isChromeAvailable() {
        logger.Info("Using Chrome for browser automation")
        opts = append(chromedp.DefaultExecAllocatorOptions[:],
            chromedp.Flag("headless", true),
            chromedp.Flag("disable-gpu", true),
            chromedp.Flag("disable-dev-shm-usage", true),
            chromedp.Flag("disable-extensions", true),
            chromedp.Flag("no-sandbox", true),
            chromedp.Flag("disable-web-security", true),
            chromedp.UserAgent("Mozilla/5.0 (Linux; Android 6.0; Nexus 5 Build/MRA58N) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Mobile Safari/537.36"),
        )
    } else {
        logger.Warn("Neither Firefox nor Chrome found for browser automation")
        // Return a scraper that will fail gracefully
        ctx, cancel := context.WithCancel(context.Background())
        return &BrowserScraper{
            ctx:    ctx,
            cancel: cancel,
            logger: logger,
        }
    }
    
    allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
        // Create context with longer timeout
        ctx, cancel := context.WithTimeout(allocCtx, 5*time.Minute)
        ctx, _ = chromedp.NewContext(ctx, 
            chromedp.WithLogf(logger.Printf),
        )    
    return &BrowserScraper{
        ctx:    ctx,
        cancel: cancel,
        logger: logger,
    }
}

func (bs *BrowserScraper) ScrapeGroupWithBrowser(groupID string, cookies []Cookie) (string, error) {
    // Check if browser automation is available
    if !isFirefoxAvailable() && !isChromeAvailable() {
        return "", fmt.Errorf("no suitable browser found for automation")
    }

    var htmlContent string
    
    err := chromedp.Run(bs.ctx,
        // Navigate to Facebook login
        chromedp.Navigate("https://www.facebook.com"),
        chromedp.Sleep(2*time.Second),
        
        // Set cookies
        bs.setCookies(cookies),
        
        // Navigate to mobile group page for better static content
        chromedp.Navigate(fmt.Sprintf("https://m.facebook.com/groups/%s", groupID)),
        chromedp.Sleep(3*time.Second),
        
        // Wait for content to start loading
        chromedp.WaitVisible("body", chromedp.ByQuery),
        chromedp.Sleep(5*time.Second),
        
        // Scroll to load more posts
        chromedp.Evaluate(`
            window.scrollTo(0, 500);
        `, nil),
        chromedp.Sleep(3*time.Second),
        
        chromedp.Evaluate(`
            window.scrollTo(0, 1000);
        `, nil),
        chromedp.Sleep(3*time.Second),
        
        chromedp.Evaluate(`
            window.scrollTo(0, 1500);
        `, nil),
        chromedp.Sleep(3*time.Second),
        
        // Get the final HTML
        chromedp.OuterHTML("html", &htmlContent),
    )
    
    if err != nil {
        return "", fmt.Errorf("failed to scrape with browser: %w", err)
    }
    
    return htmlContent, nil
}
func (bs *BrowserScraper) setCookies(cookies []Cookie) chromedp.Action {
    return chromedp.ActionFunc(func(ctx context.Context) error {
        for _, cookie := range cookies {
            err := network.SetCookie(cookie.Name, cookie.Value).
                WithDomain(cookie.Domain).
                WithPath(cookie.Path).
                Do(ctx)
            if err != nil {
                return err
            }
        }
        return nil
    })
}


func (bs *BrowserScraper) Close() {
    bs.cancel()
}
// Helper functions to check browser availability
func isFirefoxAvailable() bool {
    _, err := exec.LookPath("firefox")
    return err == nil
}

func isChromeAvailable() bool {
    paths := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
    for _, path := range paths {
        if _, err := exec.LookPath(path); err == nil {
            return true
        }
    }
    return false
}   