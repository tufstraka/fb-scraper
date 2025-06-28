package scraper

import (
    "encoding/json"
    "regexp"
    "strconv"
    "strings"
    "time"

    "github.com/PuerkitoBio/goquery"
    "facebook-scraper/pkg/types"
)

// EnhancedParser provides advanced parsing capabilities for Facebook content
type EnhancedParser struct {
    logger interface{ Debugf(string, ...interface{}) }
}

func NewEnhancedParser(logger interface{ Debugf(string, ...interface{}) }) *EnhancedParser {
    return &EnhancedParser{logger: logger}
}

// ParseJSONFromScript extracts structured data from script tags
func (ep *EnhancedParser) ParseJSONFromScript(doc *goquery.Document) []types.ScrapedPost {
    var posts []types.ScrapedPost

    doc.Find("script").Each(func(i int, script *goquery.Selection) {
        content := script.Text()
        
        // Look for various JSON patterns that might contain post data
        patterns := []string{
            `"node":\s*({[^}]*"story"[^}]*})`,
            `"feedback":\s*({[^}]*"reaction_count"[^}]*})`,
            `"creation_story":\s*({[^}]*})`,
        }

        for _, pattern := range patterns {
            re := regexp.MustCompile(pattern)
            matches := re.FindAllStringSubmatch(content, -1)
            
            for _, match := range matches {
                if len(match) > 1 {
                    if post := ep.parseJSONPost(match[1]); post.ID != "" {
                        posts = append(posts, post)
                    }
                }
            }
        }
    })

    return posts
}

func (ep *EnhancedParser) parseJSONPost(jsonStr string) types.ScrapedPost {
    var data map[string]interface{}
    if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
        return types.ScrapedPost{}
    }

    post := types.ScrapedPost{}

    // Extract ID
    if id, ok := data["id"].(string); ok {
        post.ID = id
    }

    // Extract content
    if message, ok := data["message"].(string); ok {
        post.Content = message
    }

    // Extract engagement metrics
    if feedback, ok := data["feedback"].(map[string]interface{}); ok {
        if reactionCount, ok := feedback["reaction_count"].(float64); ok {
            post.LikesCount = int(reactionCount)
        }
        if commentCount, ok := feedback["comment_count"].(float64); ok {
            post.CommentsCount = int(commentCount)
        }
        if shareCount, ok := feedback["share_count"].(float64); ok {
            post.SharesCount = int(shareCount)
        }
    }

    return post
}

// ParseMobileFormat handles mobile-specific Facebook layouts
func (ep *EnhancedParser) ParseMobileFormat(doc *goquery.Document, groupID string) []types.ScrapedPost {
    var posts []types.ScrapedPost

    // Mobile-specific selectors
    selectors := []string{
        "div[data-ft]",
        "article",
        "div[id*='story']",
        ".story_body_container",
    }

    for _, selector := range selectors {
        doc.Find(selector).Each(func(i int, s *goquery.Selection) {
            post := ep.extractMobilePost(s, groupID)
            if post.ID != "" {
                posts = append(posts, post)
            }
        })
    }

    return posts
}

func (ep *EnhancedParser) extractMobilePost(s *goquery.Selection, groupID string) types.ScrapedPost {
    post := types.ScrapedPost{GroupID: groupID}

    // Extract post ID from data-ft attribute
    if dataFt, exists := s.Attr("data-ft"); exists {
        post.ID = ep.extractIDFromDataFt(dataFt)
    }

    // Extract author
    authorLink := s.Find("h3 a, .actor a").First()
    post.AuthorName = strings.TrimSpace(authorLink.Text())
    if href, exists := authorLink.Attr("href"); exists {
        post.AuthorID = ep.extractUserIDFromHref(href)
    }

    // Extract content
    post.Content = strings.TrimSpace(s.Find(".userContent, [data-testid='post_message']").Text())

    // Extract timestamp
    timeElem := s.Find("abbr[data-utime]").First()
    if utime, exists := timeElem.Attr("data-utime"); exists {
        if timestamp, err := strconv.ParseInt(utime, 10, 64); err == nil {
            post.PostTime = time.Unix(timestamp, 0)
        }
    }

    // Extract engagement metrics
    post.LikesCount = ep.extractEngagementCount(s, "like")
    post.CommentsCount = ep.extractEngagementCount(s, "comment")
    post.SharesCount = ep.extractEngagementCount(s, "share")

    // Extract media
    post.Images = ep.extractMobileImages(s)
    post.Videos = ep.extractMobileVideos(s)

    return post
}

func (ep *EnhancedParser) extractIDFromDataFt(dataFt string) string {
    // Try to parse as JSON first
    var ftData map[string]interface{}
    if err := json.Unmarshal([]byte(dataFt), &ftData); err == nil {
        if id, ok := ftData["top_level_post_id"].(string); ok {
            return id
        }
        if id, ok := ftData["mf_story_key"].(string); ok {
            return id
        }
    }

    // Fallback to regex extraction
    patterns := []string{
        `"top_level_post_id":"(\d+)"`,
        `"mf_story_key":"(\d+)"`,
        `"story_fbid":"(\d+)"`,
    }

    for _, pattern := range patterns {
        re := regexp.MustCompile(pattern)
        if matches := re.FindStringSubmatch(dataFt); len(matches) > 1 {
            return matches[1]
        }
    }

    return ""
}

func (ep *EnhancedParser) extractUserIDFromHref(href string) string {
    patterns := []string{
        `profile\.php\?id=(\d+)`,
        `/user/(\d+)`,
        `/people/[^/]+/(\d+)`,
    }

    for _, pattern := range patterns {
        re := regexp.MustCompile(pattern)
        if matches := re.FindStringSubmatch(href); len(matches) > 1 {
            return matches[1]
        }
    }

    return ""
}

func (ep *EnhancedParser) extractEngagementCount(s *goquery.Selection, engagementType string) int {
    var patterns []string
    
    switch engagementType {
    case "like":
        patterns = []string{
            `(\d+)\s*likes?`,
            `(\d+)\s*reactions?`,
        }
    case "comment":
        patterns = []string{
            `(\d+)\s*comments?`,
            `(\d+)\s*replies?`,
        }
    case "share":
        patterns = []string{
            `(\d+)\s*shares?`,
            `(\d+)\s*shared`,
        }
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

func (ep *EnhancedParser) extractMobileImages(s *goquery.Selection) []types.MediaItem {
    var images []types.MediaItem

    s.Find("img").Each(func(i int, img *goquery.Selection) {
        if src, exists := img.Attr("src"); exists {
            if ep.isValidFacebookImage(src) {
                alt, _ := img.Attr("alt")
                images = append(images, types.MediaItem{
                    URL:         src,
                    Type:        "image",
                    Description: alt,
                })
            }
        }
    })

    return images
}

func (ep *EnhancedParser) extractMobileVideos(s *goquery.Selection) []types.MediaItem {
    var videos []types.MediaItem

    s.Find("video").Each(func(i int, video *goquery.Selection) {
        if src, exists := video.Attr("src"); exists {
            videos = append(videos, types.MediaItem{
                URL:  src,
                Type: "video",
            })
        }
    })

    // Also look for video thumbnails and data attributes
    s.Find("[data-video-id]").Each(func(i int, elem *goquery.Selection) {
        if videoID, exists := elem.Attr("data-video-id"); exists {
            videos = append(videos, types.MediaItem{
                URL:  ep.constructVideoURL(videoID),
                Type: "video",
            })
        }
    })

    return videos
}

func (ep *EnhancedParser) isValidFacebookImage(src string) bool {
    return strings.Contains(src, "facebook.com") || 
           strings.Contains(src, "fbcdn.net") ||
           strings.Contains(src, "fbsbx.com")
}

func (ep *EnhancedParser) constructVideoURL(videoID string) string {
    return "https://www.facebook.com/video.php?v=" + videoID
}

// ParseDesktopFormat handles desktop-specific Facebook layouts
func (ep *EnhancedParser) ParseDesktopFormat(doc *goquery.Document, groupID string) []types.ScrapedPost {
    var posts []types.ScrapedPost

    // Desktop-specific selectors
    doc.Find("div[role='article'], div[data-pagelet='FeedUnit']").Each(func(i int, s *goquery.Selection) {
        post := ep.extractDesktopPost(s, groupID)
        if post.ID != "" {
            posts = append(posts, post)
        }
    })

    return posts
}

func (ep *EnhancedParser) extractDesktopPost(s *goquery.Selection, groupID string) types.ScrapedPost {
    post := types.ScrapedPost{GroupID: groupID}

    // Desktop posts have different structure
    // Extract ID from various possible attributes
    if id, exists := s.Attr("data-ft"); exists {
        post.ID = ep.extractIDFromDataFt(id)
    }

    // Extract author from desktop layout
    authorElem := s.Find("h4 a, [data-testid='story-subtitle'] a").First()
    post.AuthorName = strings.TrimSpace(authorElem.Text())

    // Extract content from desktop layout
    post.Content = strings.TrimSpace(s.Find("[data-testid='post_message'], .userContent").Text())

    // Extract timestamp from desktop layout
    timeElem := s.Find("time, [data-testid='story-subtitle'] a").First()
    if datetime, exists := timeElem.Attr("datetime"); exists {
        if t, err := time.Parse(time.RFC3339, datetime); err == nil {
            post.PostTime = t
        }
    }

    return post
}