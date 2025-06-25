package models

import (
    "time"
    "database/sql/driver"
    "encoding/json"
    "errors"
)

type Post struct {
    ID          int64     `json:"id" db:"id"`
    GroupID     string    `json:"group_id" db:"group_id"`
    GroupName   string    `json:"group_name" db:"group_name"`
    PostID      string    `json:"post_id" db:"post_id"`
    AuthorID    string    `json:"author_id" db:"author_id"`
    AuthorName  string    `json:"author_name" db:"author_name"`
    Content     string    `json:"content" db:"content"`
    PostURL     string    `json:"post_url" db:"post_url"`
    Timestamp   time.Time `json:"timestamp" db:"timestamp"`
    Likes       int       `json:"likes" db:"likes"`
    Comments    int       `json:"comments" db:"comments"`
    Shares      int       `json:"shares" db:"shares"`
    Images      StringArray `json:"images" db:"images"`
    Videos      StringArray `json:"videos" db:"videos"`
    Links       StringArray `json:"links" db:"links"`
    Hashtags    StringArray `json:"hashtags" db:"hashtags"`
    Mentions    StringArray `json:"mentions" db:"mentions"`
    PostType    string    `json:"post_type" db:"post_type"`
    ScrapedAt   time.Time `json:"scraped_at" db:"scraped_at"`
    CreatedAt   time.Time `json:"created_at" db:"created_at"`
    UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// StringArray for handling JSON arrays in PostgreSQL
type StringArray []string

func (sa StringArray) Value() (driver.Value, error) {
    if len(sa) == 0 {
        return "[]", nil
    }
    return json.Marshal(sa)
}

func (sa *StringArray) Scan(value interface{}) error {
    if value == nil {
        *sa = StringArray{}
        return nil
    }
    
    bytes, ok := value.([]byte)
    if !ok {
        return errors.New("type assertion to []byte failed")
    }
    
    return json.Unmarshal(bytes, sa)
}