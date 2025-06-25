package scraper

import (
    "fmt"
    "strings"
    "time"

    "github.com/sirupsen/logrus"
    "github.com/tebeka/selenium"
    "github.com/tebeka/selenium/firefox"
)

type SeleniumBrowserScraper struct {
    driver  selenium.WebDriver
    service *selenium.Service
    logger  *logrus.Logger
}

func NewSeleniumBrowserScraper(logger *logrus.Logger) (*SeleniumBrowserScraper, error) {
    // Set up Firefox options
    firefoxCaps := selenium.Capabilities{
        "browserName": "firefox",
    }
    
    firefoxOptions := firefox.Capabilities{
        Args: []string{
            "--headless",
            "--no-sandbox",
            "--disable-dev-shm-usage",
            "--disable-gpu",
        },
        Prefs: map[string]interface{}{
            "general.useragent.override": "Mozilla/5.0 (X11; Linux x86_64; rv:91.0) Gecko/20100101 Firefox/91.0",
            "dom.webdriver.enabled":      false,
            "useAutomationExtension":     false,
        },
    }
    
    firefoxCaps.AddFirefox(firefoxOptions)
    
    // Start a WebDriver server instance on port 4444
    const port = 4444
    opts := []selenium.ServiceOption{}
    selenium.SetDebug(false)
    
    service, err := selenium.NewGeckoDriverService("geckodriver", port, opts...)
    if err != nil {
        return nil, fmt.Errorf("failed to start GeckoDriver service: %w", err)
    }
    
    // Connect to the WebDriver instance
    driver, err := selenium.NewRemote(firefoxCaps, fmt.Sprintf("http://localhost:%d", port))
    if err != nil {
        service.Stop()
        return nil, fmt.Errorf("failed to open session: %w", err)
    }
    
    return &SeleniumBrowserScraper{
        driver:  driver,
        service: service,
        logger:  logger,
    }, nil
}

func (sbs *SeleniumBrowserScraper) ScrapeGroupWithBrowser(groupID string, cookies []Cookie) (string, error) {
    // Navigate to Facebook first to establish domain
    err := sbs.driver.Get("https://www.facebook.com")
    if err != nil {
        return "", fmt.Errorf("failed to navigate to Facebook: %w", err)
    }
    
    // Wait for page to load
    time.Sleep(3 * time.Second)
    
    // Set cookies with proper domain handling
    for _, cookie := range cookies {
        // Skip cookies without proper domain or with empty domain
        domain := cookie.Domain
        if domain == "" {
            domain = ".facebook.com"
        }
        
        // Ensure domain starts with a dot for proper cookie handling
        if !strings.HasPrefix(domain, ".") && domain != "facebook.com" {
            domain = "." + domain
        }
        
        seleniumCookie := &selenium.Cookie{
            Name:   cookie.Name,
            Value:  cookie.Value,
            Domain: domain,
            Path:   "/",
        }
        
        err := sbs.driver.AddCookie(seleniumCookie)
        if err != nil {
            sbs.logger.Warnf("Failed to set cookie %s: %v", cookie.Name, err)
            // Continue with other cookies instead of failing completely
            continue
        } else {
            sbs.logger.Debugf("Successfully set cookie: %s for domain: %s", cookie.Name, domain)
        }
    }
    
    // Navigate to group after setting cookies
    groupURL := fmt.Sprintf("https://m.facebook.com/groups/%s", groupID)
    err = sbs.driver.Get(groupURL)
    if err != nil {
        return "", fmt.Errorf("failed to navigate to group: %w", err)
    }
    
    // Wait for page to load
    time.Sleep(5 * time.Second)
    
    // Scroll to load more content
    for i := 0; i < 3; i++ {
        _, err = sbs.driver.ExecuteScript("window.scrollTo(0, document.body.scrollHeight);", nil)
        if err != nil {
            sbs.logger.Warnf("Failed to scroll: %v", err)
        }
        time.Sleep(3 * time.Second)
    }
    
    // Get page source
    pageSource, err := sbs.driver.PageSource()
    if err != nil {
        return "", fmt.Errorf("failed to get page source: %w", err)
    }
    
    return pageSource, nil
}

func (sbs *SeleniumBrowserScraper) Close() {
    if sbs.driver != nil {
        sbs.driver.Quit()
    }
    if sbs.service != nil {
        sbs.service.Stop()
    }
}