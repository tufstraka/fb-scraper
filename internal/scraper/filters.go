package scraper

import (
    "strings"
    "time"
    "facebook-scraper/pkg/types"
)

// ApplyFilter applies the filter to a single post
func ApplyFilter(post types.ScrapedPost, filter *types.PostFilter) bool {
    // Check likes threshold
    if filter.MinLikes > 0 && post.LikesCount < filter.MinLikes {
        return false
    }
    
    if filter.MaxLikes > 0 && post.LikesCount > filter.MaxLikes {
        return false
    }
    
    // Check comments threshold
    if filter.MinComments > 0 && post.CommentsCount < filter.MinComments {
        return false
    }
    
    // Check shares threshold
    if filter.MinShares > 0 && post.SharesCount < filter.MinShares {
        return false
    }
    
    // Check time range
    if filter.DaysBack > 0 {
        cutoffTime := time.Now().AddDate(0, 0, -filter.DaysBack)
        if post.PostTime.Before(cutoffTime) {
            return false
        }
    }
    
    // Check custom date range
    if !filter.StartDate.IsZero() && post.PostTime.Before(filter.StartDate) {
        return false
    }
    
    if !filter.EndDate.IsZero() && post.PostTime.After(filter.EndDate) {
        return false
    }
    
    // Check keywords (include)
    if len(filter.Keywords) > 0 {
        hasKeyword := false
        contentLower := strings.ToLower(post.Content)
        for _, keyword := range filter.Keywords {
            if strings.Contains(contentLower, strings.ToLower(keyword)) {
                hasKeyword = true
                break
            }
        }
        if !hasKeyword {
            return false
        }
    }
    
    // Check excluded keywords
    if len(filter.ExcludeKeywords) > 0 {
        contentLower := strings.ToLower(post.Content)
        for _, keyword := range filter.ExcludeKeywords {
            if strings.Contains(contentLower, strings.ToLower(keyword)) {
                return false
            }
        }
    }
    
    // Check group IDs
    if len(filter.GroupIDs) > 0 {
        found := false
        for _, groupID := range filter.GroupIDs {
            if post.GroupID == groupID {
                found = true
                break
            }
        }
        if !found {
            return false
        }
    }
    
    // Check author names
    if len(filter.AuthorNames) > 0 {
        found := false
        for _, authorName := range filter.AuthorNames {
            if strings.EqualFold(post.AuthorName, authorName) {
                found = true
                break
            }
        }
        if !found {
            return false
        }
    }
    
    return true
}

// BatchFilter applies filters to multiple posts and returns statistics
func BatchFilter(posts []types.ScrapedPost, filter *types.PostFilter) ([]types.ScrapedPost, types.FilterStats) {
    var filtered []types.ScrapedPost
    stats := types.FilterStats{
        TotalPosts: len(posts),
    }
    
    cutoffTime := time.Now().AddDate(0, 0, -filter.DaysBack)
    
    for _, post := range posts {
        // Track individual filter reasons
        passedLikes := filter.MinLikes == 0 || post.LikesCount >= filter.MinLikes
        passedTime := filter.DaysBack == 0 || post.PostTime.After(cutoffTime)
        passedKeywords := len(filter.Keywords) == 0 || containsAnyKeyword(post.Content, filter.Keywords)
        
        if !passedLikes {
            stats.LikesFiltered++
        }
        if !passedTime {
            stats.TimeFiltered++
        }
        if !passedKeywords {
            stats.KeywordFiltered++
        }
        
        // Apply full filter
        if ApplyFilter(post, filter) {
            filtered = append(filtered, post)
        }
    }
    
    stats.FilteredPosts = len(filtered)
    return filtered, stats
}

func containsAnyKeyword(content string, keywords []string) bool {
    if len(keywords) == 0 {
        return true
    }
    
    contentLower := strings.ToLower(content)
    for _, keyword := range keywords {
        if strings.Contains(contentLower, strings.ToLower(keyword)) {
            return true
        }
    }
    return false
}