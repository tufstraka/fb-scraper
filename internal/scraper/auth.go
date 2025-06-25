// internal/scraper/auth.go
package scraper

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "net/http"
    "net/http/cookiejar"
    "net/url"
    "os"
    "time"
    "strings"

    "github.com/sirupsen/logrus"
)

type Cookie struct {
    Name     string `json:"name"`
    Value    string `json:"value"`
    Domain   string `json:"domain"`
    Path     string `json:"path"`
    Secure   bool   `json:"secure"`
    HttpOnly bool   `json:"httpOnly"`
    Expires  string `json:"expires,omitempty"`
}

type CookieStore struct {
    Cookies map[string][]Cookie `json:"cookies"`
}

type AuthManager struct {
    client      *http.Client
    cookieJar   *cookiejar.Jar
    cookiesFile string
    userAgent   string
    logger      *logrus.Logger
}

func NewAuthManager(cookiesFile, userAgent string, logger *logrus.Logger) (*AuthManager, error) {
    jar, err := cookiejar.New(nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create cookie jar: %w", err)
    }

    client := &http.Client{
        Jar:     jar,
        Timeout: 30 * time.Second,
        Transport: &http.Transport{
            MaxIdleConns:        10,
            IdleConnTimeout:     30 * time.Second,
            DisableCompression:  false,
            TLSHandshakeTimeout: 10 * time.Second,
        },
    }

    return &AuthManager{
        client:      client,
        cookieJar:   jar,
        cookiesFile: cookiesFile,
        userAgent:   userAgent,
        logger:      logger,
    }, nil
}

func (am *AuthManager) LoadCookies() error {
    am.logger.Info("Loading cookies from file...")

    if _, err := os.Stat(am.cookiesFile); os.IsNotExist(err) {
        return fmt.Errorf("cookies file not found: %s", am.cookiesFile)
    }

    data, err := ioutil.ReadFile(am.cookiesFile)
    if err != nil {
        return fmt.Errorf("failed to read cookies file: %w", err)
    }

    var cookieStore map[string][]Cookie
    if err := json.Unmarshal(data, &cookieStore); err != nil {
        return fmt.Errorf("failed to parse cookies file: %w", err)
    }

    // Load cookies for facebook.com
    facebookCookies, exists := cookieStore["facebook.com"]
    if !exists {
        return fmt.Errorf("no Facebook cookies found in cookies file")
    }

    // Parse Facebook URL
    fbURL, err := url.Parse("https://www.facebook.com")
    if err != nil {
        return fmt.Errorf("failed to parse Facebook URL: %w", err)
    }

    // Convert and set cookies
    var httpCookies []*http.Cookie
    for _, cookie := range facebookCookies {
        httpCookie := &http.Cookie{
            Name:     cookie.Name,
            Value:    cookie.Value,
            Domain:   cookie.Domain,
            Path:     cookie.Path,
            Secure:   cookie.Secure,
            HttpOnly: cookie.HttpOnly,
        }

        // Parse expiry if provided
        if cookie.Expires != "" {
            if expires, err := time.Parse(time.RFC3339, cookie.Expires); err == nil {
                httpCookie.Expires = expires
            }
        }

        httpCookies = append(httpCookies, httpCookie)
    }

    am.cookieJar.SetCookies(fbURL, httpCookies)
    am.logger.Infof("Loaded %d cookies for Facebook", len(httpCookies))

    return nil
}

func (am *AuthManager) ValidateAuth() error {
    am.logger.Info("Validating Facebook authentication...")

    // Use a more realistic endpoint - try the notifications page
    req, err := http.NewRequest("GET", "https://www.facebook.com/notifications", nil)
    if err != nil {
        return fmt.Errorf("failed to create validation request: %w", err)
    }

    // Set realistic browser headers
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

    resp, err := am.client.Do(req)
    if err != nil {
        return fmt.Errorf("failed to validate authentication: %w", err)
    }
    defer resp.Body.Close()

    am.logger.Infof("Validation response: Status=%d, URL=%s", resp.StatusCode, resp.Request.URL.String())

    // Read response body for debugging
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        am.logger.Warnf("Failed to read response body: %v", err)
    } else {
        bodyStr := string(body)
        am.logger.Debugf("Response body length: %d", len(bodyStr))
        
        // Check for success indicators (notifications page would contain these)
        if strings.Contains(bodyStr, "notification") || 
           strings.Contains(bodyStr, "fb-notifications") || 
           strings.Contains(bodyStr, "\"viewer\"") ||
           strings.Contains(bodyStr, "\"USER_ID\"") {
            am.logger.Info("Authentication validated successfully")
            return nil
        }
        
        // Check for failure indicators
        if strings.Contains(bodyStr, "login") || strings.Contains(bodyStr, "Log In") {
            return fmt.Errorf("authentication failed: redirected to login page")
        }
        
        if strings.Contains(bodyStr, "checkpoint") {
            return fmt.Errorf("authentication failed: account requires checkpoint verification")
        }
        
        if strings.Contains(bodyStr, "captcha") {
            return fmt.Errorf("authentication failed: captcha challenge required")
        }

        // Log first 200 chars for debugging
        preview := bodyStr
        if len(preview) > 200 {
            preview = preview[:200] + "..."
        }
        am.logger.Debugf("Response preview: %s", preview)
    }

    switch resp.StatusCode {
    case http.StatusOK:
        // Even if 200, check if we got the real page
        if strings.Contains(resp.Request.URL.String(), "login") {
            return fmt.Errorf("authentication failed: redirected to login page")
        }
        am.logger.Info("Authentication validated successfully")
        return nil
    case 400:
        return fmt.Errorf("authentication failed: bad request (400) - cookies may be expired or invalid")
    case 401:
        return fmt.Errorf("authentication failed: unauthorized (401) - invalid credentials")
    case 403:
        return fmt.Errorf("authentication failed: forbidden (403) - account may be restricted")
    case 429:
        return fmt.Errorf("authentication failed: rate limited (429) - too many requests")
    default:
        return fmt.Errorf("authentication failed: status code %d", resp.StatusCode)
    }
}

func (am *AuthManager) GetAuthenticatedClient() *http.Client {
    return am.client
}

func (am *AuthManager) SaveCookies() error {
    am.logger.Info("Saving current cookies...")

    fbURL, _ := url.Parse("https://www.facebook.com")
    cookies := am.cookieJar.Cookies(fbURL)

    var cookieData []Cookie
    for _, cookie := range cookies {
        cookieData = append(cookieData, Cookie{
            Name:     cookie.Name,
            Value:    cookie.Value,
            Domain:   cookie.Domain,
            Path:     cookie.Path,
            Secure:   cookie.Secure,
            HttpOnly: cookie.HttpOnly,
            Expires:  cookie.Expires.Format(time.RFC3339),
        })
    }

    cookieStore := map[string][]Cookie{
        "facebook.com": cookieData,
    }

    data, err := json.MarshalIndent(cookieStore, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal cookies: %w", err)
    }

    err = ioutil.WriteFile(am.cookiesFile, data, 0600)
    if err != nil {
        return fmt.Errorf("failed to write cookies file: %w", err)
    }

    am.logger.Infof("Saved %d cookies to file", len(cookieData))
    return nil
}

// Helper function to extract cookies from browser
func ExtractCookiesFromBrowser() {
    fmt.Println(`
To extract cookies from your browser:

1. Open Facebook in your browser and log in
2. Open Developer Tools (F12)
3. Go to Application/Storage tab
4. Click on Cookies -> https://www.facebook.com
5. Copy the following cookie values:

Required cookies:
- c_user: Your user ID
- xs: Session token 
- datr: Device authentication token
- sb: Secure browsing token
- fr: Facebook request token

6. Update the configs/cookies.json file with these values`)
}

func (am *AuthManager) validateCookieFormat() error {
    fbURL, _ := url.Parse("https://www.facebook.com")
    cookies := am.cookieJar.Cookies(fbURL)
    
    requiredCookies := []string{"c_user", "xs", "datr"}
    cookieMap := make(map[string]*http.Cookie)
    
    for _, cookie := range cookies {
        cookieMap[cookie.Name] = cookie
    }
    
    for _, required := range requiredCookies {
        if cookie, exists := cookieMap[required]; !exists {
            return fmt.Errorf("missing required cookie: %s", required)
        } else if cookie.Value == "" {
            return fmt.Errorf("empty value for required cookie: %s", required)
        } else if required == "c_user" && !isNumeric(cookie.Value) {
            return fmt.Errorf("c_user cookie should be numeric, got: %s", cookie.Value)
        }
    }
    
    am.logger.Info("Cookie format validation passed")
    return nil
}

func isNumeric(s string) bool {
    for _, r := range s {
        if r < '0' || r > '9' {
            return false
        }
    }
    return len(s) > 0
}