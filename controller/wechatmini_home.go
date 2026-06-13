package controller

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type WechatMiniDailyCallCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

var wechatMiniNow = time.Now

func GetWechatMiniUsageOverview(c *gin.Context) {
	userID := c.GetInt("id")
	username := c.GetString("username")
	now := wechatMiniNow()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	sevenDaysAgo := startOfToday - 6*24*60*60
	statStart := now.Unix() - 3*24*60*60

	todayCount, todayQuota, err := getWechatMiniTodayConsumeSummary(userID, startOfToday, now.Unix())
	if err != nil {
		common.ApiError(c, err)
		return
	}

	dailyCounts, err := getWechatMiniDailyConsumeCounts(userID, sevenDaysAgo, now.Unix(), now.Location())
	if err != nil {
		common.ApiError(c, err)
		return
	}

	stat, err := model.SumUsedQuota(model.LogTypeConsume, statStart, now.Unix(), "", username, "", 0, "")
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"today": gin.H{
				"call_count": todayCount,
				"quota":      todayQuota,
			},
			"last_7_days_calls": dailyCounts,
			"stat": gin.H{
				"quota": stat.Quota,
				"rpm":   stat.Rpm,
				"tpm":   stat.Tpm,
			},
		},
	})
}

type wechatMiniUsageAnalyticsRange struct {
	Key             string `json:"key"`
	StartTimestamp  int64  `json:"start_timestamp"`
	EndTimestamp    int64  `json:"end_timestamp"`
	TimeGranularity string `json:"time_granularity"`
}

type wechatMiniUsageAnalyticsSummary struct {
	TotalCount  int     `json:"total_count"`
	TotalQuota  int     `json:"total_quota"`
	TotalTokens int     `json:"total_tokens"`
	AvgRPM      float64 `json:"avg_rpm"`
	AvgTPM      float64 `json:"avg_tpm"`
}

type wechatMiniUsageAnalyticsTrendPoint struct {
	Label     string `json:"label"`
	Timestamp int64  `json:"timestamp"`
	Count     int    `json:"count"`
	Quota     int    `json:"quota"`
	TokenUsed int    `json:"token_used"`
}

type wechatMiniUsageAnalyticsModelStat struct {
	ModelName string  `json:"model_name"`
	Count     int     `json:"count"`
	Quota     int     `json:"quota"`
	TokenUsed int     `json:"token_used"`
	Percent   float64 `json:"percent"`
}

type wechatMiniUsageAnalyticsStat struct {
	count  int
	quota  int
	tokens int
}

type wechatMiniUsageAnalyticsResponse struct {
	Range             wechatMiniUsageAnalyticsRange        `json:"range"`
	Summary           wechatMiniUsageAnalyticsSummary      `json:"summary"`
	CallTrend         []wechatMiniUsageAnalyticsTrendPoint `json:"call_trend"`
	QuotaTrend        []wechatMiniUsageAnalyticsTrendPoint `json:"quota_trend"`
	ModelRanking      []wechatMiniUsageAnalyticsModelStat  `json:"model_ranking"`
	CallDistribution  []wechatMiniUsageAnalyticsModelStat  `json:"call_distribution"`
	QuotaDistribution []wechatMiniUsageAnalyticsModelStat  `json:"quota_distribution"`
	Raw               []*model.QuotaData                   `json:"raw"`
}

func GetWechatMiniUsageAnalytics(c *gin.Context) {
	userID := c.GetInt("id")
	now := wechatMiniNow()
	rangeKey, startTimestamp, endTimestamp, granularity := resolveWechatMiniAnalyticsRange(c, now)

	data, err := model.GetQuotaDataByUserId(userID, startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    buildWechatMiniUsageAnalyticsResponse(data, rangeKey, startTimestamp, endTimestamp, granularity),
	})
}

func resolveWechatMiniAnalyticsRange(c *gin.Context, now time.Time) (string, int64, int64, string) {
	rangeKey := c.DefaultQuery("range", "7days")
	endTimestamp := now.Unix()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startTimestamp := startOfToday.AddDate(0, 0, -6).Unix()
	granularity := "day"

	switch rangeKey {
	case "today":
		startTimestamp = startOfToday.Unix()
		granularity = "hour"
	case "30days":
		startTimestamp = startOfToday.AddDate(0, 0, -29).Unix()
		granularity = "day"
	default:
		rangeKey = "7days"
	}

	if raw := c.Query("start_timestamp"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			startTimestamp = parsed
		}
	}
	if raw := c.Query("end_timestamp"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			endTimestamp = parsed
		}
	}
	if raw := c.Query("time_granularity"); raw == "hour" || raw == "day" || raw == "week" {
		granularity = raw
	}

	return rangeKey, startTimestamp, endTimestamp, granularity
}

func buildWechatMiniUsageAnalyticsResponse(data []*model.QuotaData, rangeKey string, startTimestamp int64, endTimestamp int64, granularity string) wechatMiniUsageAnalyticsResponse {
	timeStats := map[int64]wechatMiniUsageAnalyticsStat{}
	modelStats := map[string]wechatMiniUsageAnalyticsStat{}
	summary := wechatMiniUsageAnalyticsSummary{}

	for _, item := range data {
		if item == nil {
			continue
		}
		bucket := truncateWechatMiniAnalyticsTimestamp(item.CreatedAt, granularity)
		modelName := item.ModelName
		if modelName == "" {
			modelName = "Unknown"
		}
		count := item.Count
		quota := item.Quota
		tokens := item.TokenUsed

		timeStat := timeStats[bucket]
		timeStat.count += count
		timeStat.quota += quota
		timeStat.tokens += tokens
		timeStats[bucket] = timeStat

		modelStat := modelStats[modelName]
		modelStat.count += count
		modelStat.quota += quota
		modelStat.tokens += tokens
		modelStats[modelName] = modelStat

		summary.TotalCount += count
		summary.TotalQuota += quota
		summary.TotalTokens += tokens
	}

	minutes := float64(endTimestamp-startTimestamp) / 60
	if minutes > 0 {
		summary.AvgRPM = roundWechatMiniAnalyticsFloat(float64(summary.TotalCount)/minutes, 3)
		summary.AvgTPM = roundWechatMiniAnalyticsFloat(float64(summary.TotalTokens)/minutes, 3)
	}

	trend := buildWechatMiniAnalyticsTrend(timeStats, startTimestamp, endTimestamp, granularity)
	modelRanking := buildWechatMiniAnalyticsModelStats(modelStats, "count", summary.TotalCount)
	quotaDistribution := buildWechatMiniAnalyticsModelStats(modelStats, "quota", summary.TotalQuota)

	return wechatMiniUsageAnalyticsResponse{
		Range: wechatMiniUsageAnalyticsRange{
			Key:             rangeKey,
			StartTimestamp:  startTimestamp,
			EndTimestamp:    endTimestamp,
			TimeGranularity: granularity,
		},
		Summary:           summary,
		CallTrend:         trend,
		QuotaTrend:        trend,
		ModelRanking:      modelRanking,
		CallDistribution:  modelRanking,
		QuotaDistribution: quotaDistribution,
		Raw:               data,
	}
}

func truncateWechatMiniAnalyticsTimestamp(timestamp int64, granularity string) int64 {
	t := time.Unix(timestamp, 0).Local()
	switch granularity {
	case "week":
		weekday := int(t.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		return day.AddDate(0, 0, -(weekday - 1)).Unix()
	case "day":
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).Unix()
	default:
		return timestamp - timestamp%3600
	}
}

func buildWechatMiniAnalyticsTrend(timeStats map[int64]wechatMiniUsageAnalyticsStat, startTimestamp int64, endTimestamp int64, granularity string) []wechatMiniUsageAnalyticsTrendPoint {
	step := int64(3600)
	labelFormat := "15:04"
	if granularity == "day" {
		step = 24 * 3600
		labelFormat = "01-02"
	} else if granularity == "week" {
		step = 7 * 24 * 3600
		labelFormat = "01-02"
	}

	start := truncateWechatMiniAnalyticsTimestamp(startTimestamp, granularity)
	end := truncateWechatMiniAnalyticsTimestamp(endTimestamp, granularity)
	result := make([]wechatMiniUsageAnalyticsTrendPoint, 0)
	for ts := start; ts <= end; ts += step {
		current := time.Unix(ts, 0).Local()
		item := timeStats[ts]
		result = append(result, wechatMiniUsageAnalyticsTrendPoint{
			Label:     current.Format(labelFormat),
			Timestamp: ts,
			Count:     item.count,
			Quota:     item.quota,
			TokenUsed: item.tokens,
		})
	}
	return result
}

func buildWechatMiniAnalyticsModelStats(modelStats map[string]wechatMiniUsageAnalyticsStat, sortBy string, total int) []wechatMiniUsageAnalyticsModelStat {
	result := make([]wechatMiniUsageAnalyticsModelStat, 0, len(modelStats))
	for modelName, item := range modelStats {
		percentBase := item.count
		if sortBy == "quota" {
			percentBase = item.quota
		}
		percent := 0.0
		if total > 0 {
			percent = roundWechatMiniAnalyticsFloat(float64(percentBase)*100/float64(total), 2)
		}
		result = append(result, wechatMiniUsageAnalyticsModelStat{
			ModelName: modelName,
			Count:     item.count,
			Quota:     item.quota,
			TokenUsed: item.tokens,
			Percent:   percent,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if sortBy == "quota" && result[i].Quota != result[j].Quota {
			return result[i].Quota > result[j].Quota
		}
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].ModelName < result[j].ModelName
	})
	return result
}

func roundWechatMiniAnalyticsFloat(value float64, precision int) float64 {
	if precision <= 0 {
		return float64(int(value + 0.5))
	}
	factor := 1.0
	for i := 0; i < precision; i++ {
		factor *= 10
	}
	return float64(int(value*factor+0.5)) / factor
}

func getWechatMiniTodayConsumeSummary(userID int, startTimestamp int64, endTimestamp int64) (count int, quota int, err error) {
	type result struct {
		Count int `gorm:"column:count"`
		Quota int `gorm:"column:quota"`
	}
	var row result
	err = model.LOG_DB.Table("logs").
		Select("count(*) as count, coalesce(sum(quota), 0) as quota").
		Where("user_id = ? AND type = ? AND created_at >= ? AND created_at <= ?", userID, model.LogTypeConsume, startTimestamp, endTimestamp).
		Scan(&row).Error
	return row.Count, row.Quota, err
}

func getWechatMiniDailyConsumeCounts(userID int, startTimestamp int64, endTimestamp int64, loc *time.Location) ([]WechatMiniDailyCallCount, error) {
	logs, _, err := model.GetUserLogs(userID, model.LogTypeConsume, startTimestamp, endTimestamp, "", "", 0, 10000, "", "", "")
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int, 7)
	for _, entry := range logs {
		day := time.Unix(entry.CreatedAt, 0).In(loc).Format("01-02")
		counts[day] += 1
	}

	result := make([]WechatMiniDailyCallCount, 0, 7)
	start := time.Unix(startTimestamp, 0).In(loc)
	for i := 0; i < 7; i++ {
		current := start.AddDate(0, 0, i)
		day := current.Format("01-02")
		result = append(result, WechatMiniDailyCallCount{
			Date:  day,
			Count: counts[day],
		})
	}
	return result, nil
}
