// scripts/extract_cookies.go
package main

import (
    "fmt"
    "os"
)

func main() {
    fmt.Println("Facebook Cookie Extraction Guide")
    fmt.Println("================================")
    fmt.Println()
    fmt.Println("1. Open Facebook in your browser and log in successfully")
    fmt.Println("2. Open Developer Tools (F12 or Ctrl+Shift+I)")
    fmt.Println("3. Go to the 'Application' tab (Chrome) or 'Storage' tab (Firefox)")
    fmt.Println("4. In the left sidebar, expand 'Cookies' and click on 'https://www.facebook.com'")
    fmt.Println("5. Find and copy the values for these essential cookies:")
    fmt.Println()
    fmt.Println("   Essential Cookies:")
    fmt.Println("   - c_user: Your Facebook user ID")
    fmt.Println("   - xs: Session authentication token") 
    fmt.Println("   - datr: Device authentication token")
    fmt.Println("   - sb: Secure browsing token")
    fmt.Println("   - fr: Facebook request token")
    fmt.Println()
    fmt.Println("6. Update the 'configs/cookies.json' file with these values")
    fmt.Println()
    fmt.Println("Example cookies.json structure:")
    fmt.Println(`{
  "facebook.com": [
    {
      "name": "c_user",
      "value": "YOUR_USER_ID_HERE",
      "domain": ".facebook.com",
      "path": "/",
      "secure": true,
      "httpOnly": false
    },
    {
      "name": "xs",
      "value": "YOUR_XS_TOKEN_HERE", 
      "domain": ".facebook.com",
      "path": "/",
      "secure": true,
      "httpOnly": true
    }
    // ... add other cookies
  ]
}`)
    fmt.Println()
    fmt.Println("7. Run the scraper: ./bin/facebook-scraper")
    fmt.Println()
    fmt.Println("Note: Cookies expire periodically. You may need to update them if authentication fails.")
}