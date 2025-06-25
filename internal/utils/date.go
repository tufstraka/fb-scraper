// internal/utils/date.go
package utils

import (
    "time"
)

func GetDateDaysAgo(days int) time.Time {
    return time.Now().AddDate(0, 0, -days)
}

func FormatTimestamp(t time.Time) string {
    return t.Format("2006-01-02 15:04:05")
}

func IsWithinDays(postTime time.Time, days int) bool {
    cutoff := GetDateDaysAgo(days)
    return postTime.After(cutoff)
}