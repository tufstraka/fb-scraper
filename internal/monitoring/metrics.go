package monitoring

import (
    "encoding/json"
    "fmt"
    "os"
    "time"

    "github.com/sirupsen/logrus"
)

type Metrics struct {
    ScrapingRuns    int                    `json:"scraping_runs"`
    TotalPosts      int                    `json:"total_posts"`
    SuccessfulPosts int                    `json:"successful_posts"`
    FailedPosts     int                    `json:"failed_posts"`
    LastRun         time.Time              `json:"last_run"`
    AverageRunTime  time.Duration          `json:"average_run_time"`
    ErrorRate       float64                `json:"error_rate"`
    GroupMetrics    map[string]GroupMetric `json:"group_metrics"`
}

type GroupMetric struct {
    PostsScraped   int           `json:"posts_scraped"`
    LastScraped    time.Time     `json:"last_scraped"`
    AverageRunTime time.Duration `json:"average_run_time"`
    ErrorCount     int           `json:"error_count"`
}

type Monitor struct {
    metrics    *Metrics
    logger     *logrus.Logger
    metricsFile string
}

func NewMonitor(logger *logrus.Logger, metricsFile string) *Monitor {
    monitor := &Monitor{
        metrics: &Metrics{
            GroupMetrics: make(map[string]GroupMetric),
        },
        logger:      logger,
        metricsFile: metricsFile,
    }

    // Load existing metrics
    monitor.loadMetrics()
    return monitor
}

func (m *Monitor) RecordScrapingRun(groupID string, postsScraped int, duration time.Duration, errors int) {
    m.metrics.ScrapingRuns++
    m.metrics.TotalPosts += postsScraped
    m.metrics.SuccessfulPosts += postsScraped - errors
    m.metrics.FailedPosts += errors
    m.metrics.LastRun = time.Now()

    // Update average run time
    if m.metrics.ScrapingRuns > 1 {
        m.metrics.AverageRunTime = (m.metrics.AverageRunTime + duration) / 2
    } else {
        m.metrics.AverageRunTime = duration
    }

    // Calculate error rate
    if m.metrics.TotalPosts > 0 {
        m.metrics.ErrorRate = float64(m.metrics.FailedPosts) / float64(m.metrics.TotalPosts) * 100
    }

    // Update group metrics
    groupMetric := m.metrics.GroupMetrics[groupID]
    groupMetric.PostsScraped += postsScraped
    groupMetric.LastScraped = time.Now()
    groupMetric.ErrorCount += errors
    
    if groupMetric.AverageRunTime == 0 {
        groupMetric.AverageRunTime = duration
    } else {
        groupMetric.AverageRunTime = (groupMetric.AverageRunTime + duration) / 2
    }
    
    m.metrics.GroupMetrics[groupID] = groupMetric

    // Save metrics
    m.saveMetrics()

    m.logger.Infof("Recorded scraping run for group %s: %d posts, %v duration, %d errors", 
        groupID, postsScraped, duration, errors)
}

func (m *Monitor) GetMetrics() *Metrics {
    return m.metrics
}

func (m *Monitor) GetHealthStatus() map[string]interface{} {
    status := map[string]interface{}{
        "status":           "healthy",
        "last_run":         m.metrics.LastRun.Format(time.RFC3339),
        "total_runs":       m.metrics.ScrapingRuns,
        "error_rate":       fmt.Sprintf("%.2f%%", m.metrics.ErrorRate),
        "average_runtime":  m.metrics.AverageRunTime.String(),
    }

    // Check if last run was too long ago
    if time.Since(m.metrics.LastRun) > 24*time.Hour {
        status["status"] = "warning"
        status["warning"] = "No scraping runs in the last 24 hours"
    }

    // Check error rate
    if m.metrics.ErrorRate > 10 {
        status["status"] = "warning"
        status["warning"] = "High error rate detected"
    }

    return status
}

func (m *Monitor) GenerateReport() string {
    report := fmt.Sprintf(`
Facebook Scraper Monitoring Report
==================================
Generated: %s

Overall Statistics:
- Total Scraping Runs: %d
- Total Posts Processed: %d
- Successful Posts: %d
- Failed Posts: %d
- Error Rate: %.2f%%
- Average Run Time: %s
- Last Run: %s

Group Performance:
`, 
        time.Now().Format("2006-01-02 15:04:05"),
        m.metrics.ScrapingRuns,
        m.metrics.TotalPosts,
        m.metrics.SuccessfulPosts,
        m.metrics.FailedPosts,
        m.metrics.ErrorRate,
        m.metrics.AverageRunTime,
        m.metrics.LastRun.Format("2006-01-02 15:04:05"),
    )

    for groupID, metric := range m.metrics.GroupMetrics {
        report += fmt.Sprintf(`
- Group %s:
  Posts Scraped: %d
  Last Scraped: %s
  Average Runtime: %s
  Errors: %d
`, 
            groupID,
            metric.PostsScraped,
            metric.LastScraped.Format("2006-01-02 15:04:05"),
            metric.AverageRunTime,
            metric.ErrorCount,
        )
    }

    return report
}

func (m *Monitor) loadMetrics() {
    if _, err := os.Stat(m.metricsFile); os.IsNotExist(err) {
        m.logger.Info("No existing metrics file found, starting fresh")
        return
    }

    data, err := os.ReadFile(m.metricsFile)
    if err != nil {
        m.logger.Warnf("Failed to read metrics file: %v", err)
        return
    }

    if err := json.Unmarshal(data, m.metrics); err != nil {
        m.logger.Warnf("Failed to parse metrics file: %v", err)
        return
    }

    m.logger.Info("Loaded existing metrics from file")
}

func (m *Monitor) saveMetrics() {
    data, err := json.MarshalIndent(m.metrics, "", "  ")
    if err != nil {
        m.logger.Errorf("Failed to marshal metrics: %v", err)
        return
    }

    if err := os.WriteFile(m.metricsFile, data, 0644); err != nil {
        m.logger.Errorf("Failed to save metrics: %v", err)
        return
    }
}

// AlertManager handles alerting based on metrics
type AlertManager struct {
    monitor *Monitor
    logger  *logrus.Logger
}

func NewAlertManager(monitor *Monitor, logger *logrus.Logger) *AlertManager {
    return &AlertManager{
        monitor: monitor,
        logger:  logger,
    }
}

func (am *AlertManager) CheckAlerts() []string {
    var alerts []string
    metrics := am.monitor.GetMetrics()

    // Check if scraper hasn't run recently
    if time.Since(metrics.LastRun) > 25*time.Hour {
        alerts = append(alerts, "ALERT: Scraper hasn't run in over 24 hours")
    }

    // Check error rate
    if metrics.ErrorRate > 15 {
        alerts = append(alerts, fmt.Sprintf("ALERT: High error rate: %.2f%%", metrics.ErrorRate))
    }

    // Check if no posts were scraped recently
    if metrics.TotalPosts == 0 {
        alerts = append(alerts, "ALERT: No posts have been scraped")
    }

    return alerts
}

func (am *AlertManager) SendAlerts(alerts []string) {
    for _, alert := range alerts {
        am.logger.Warn(alert)
        // Here you could integrate with external alerting systems
        // like Slack, email, PagerDuty, etc.
    }
}