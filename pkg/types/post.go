package types

import (
    "fmt"
    "time"
)

type ScrapedPost struct {
    ID            string    `json:"id"`
    GroupID       string    `json:"group_id"`
    AuthorName    string    `json:"author_name"`
    AuthorID      string    `json:"author_id"`
    Content       string    `json:"content"`
    URL           string    `json:"url"`
    PostTime      time.Time `json:"post_time"`
    LikesCount    int       `json:"likes_count"`
    CommentsCount int       `json:"comments_count"`
    SharesCount   int       `json:"shares_count"`
    PostType      string    `json:"post_type"`
    Images        []string  `json:"images"`
    Videos        []string  `json:"videos"`
    Links         []string  `json:"links"`
    Hashtags      []string  `json:"hashtags"`
    Mentions      []string  `json:"mentions"`
}

type PostFilter struct {
    MinLikes        int       `json:"min_likes"`
    MaxLikes        int       `json:"max_likes"`
    MinComments     int       `json:"min_comments"`
    MinShares       int       `json:"min_shares"`
    DaysBack        int       `json:"days_back"`
    Keywords        []string  `json:"keywords"`
    ExcludeKeywords []string  `json:"exclude_keywords"`
    GroupIDs        []string  `json:"group_ids"`
    PageIDs         []string  `json:"page_ids"`
    AuthorNames     []string  `json:"author_names"`
    StartDate       time.Time `json:"start_date"`
    EndDate         time.Time `json:"end_date"`
}

type FilterStats struct {
    TotalPosts     int `json:"total_posts"`
    FilteredPosts  int `json:"filtered_posts"`
    LikesFiltered  int `json:"likes_filtered"`
    TimeFiltered   int `json:"time_filtered"`
    KeywordFiltered int `json:"keyword_filtered"`
}

func (fs FilterStats) String() string {
    return fmt.Sprintf("Total: %d, Filtered: %d, Likes: %d, Time: %d, Keywords: %d", 
        fs.TotalPosts, fs.FilteredPosts, fs.LikesFiltered, fs.TimeFiltered, fs.KeywordFiltered)
}