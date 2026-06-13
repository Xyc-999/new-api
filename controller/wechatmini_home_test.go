package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type wechatMiniUsageOverviewAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Today struct {
			CallCount int `json:"call_count"`
			Quota     int `json:"quota"`
		} `json:"today"`
		Last7DaysCalls []struct {
			Date  string `json:"date"`
			Count int    `json:"count"`
		} `json:"last_7_days_calls"`
		Stat struct {
			Quota int `json:"quota"`
			Rpm   int `json:"rpm"`
			Tpm   int `json:"tpm"`
		} `json:"stat"`
	} `json:"data"`
}

type wechatMiniUsageAnalyticsAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Range struct {
			Key             string `json:"key"`
			TimeGranularity string `json:"time_granularity"`
		} `json:"range"`
		Summary struct {
			TotalCount  int     `json:"total_count"`
			TotalQuota  int     `json:"total_quota"`
			TotalTokens int     `json:"total_tokens"`
			AvgRPM      float64 `json:"avg_rpm"`
			AvgTPM      float64 `json:"avg_tpm"`
		} `json:"summary"`
		CallTrend []struct {
			Label     string `json:"label"`
			Count     int    `json:"count"`
			Quota     int    `json:"quota"`
			TokenUsed int    `json:"token_used"`
		} `json:"call_trend"`
		ModelRanking []struct {
			ModelName string  `json:"model_name"`
			Count     int     `json:"count"`
			Quota     int     `json:"quota"`
			TokenUsed int     `json:"token_used"`
			Percent   float64 `json:"percent"`
		} `json:"model_ranking"`
		QuotaDistribution []struct {
			ModelName string `json:"model_name"`
			Quota     int    `json:"quota"`
		} `json:"quota_distribution"`
		Raw []model.QuotaData `json:"raw"`
	} `json:"data"`
}

func setupWechatMiniUsageOverviewTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db
	if err := db.AutoMigrate(&model.Log{}, &model.QuotaData{}); err != nil {
		t.Fatalf("failed to migrate log table: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func insertWechatMiniQuotaData(t *testing.T, item model.QuotaData) {
	t.Helper()
	if err := model.DB.Table("quota_data").Create(&item).Error; err != nil {
		t.Fatalf("failed to insert quota data: %v", err)
	}
}

func insertWechatMiniConsumeLog(t *testing.T, userID int, createdAt int64, quota int) {
	t.Helper()
	entry := model.Log{
		UserId:    userID,
		Username:  "alice",
		CreatedAt: createdAt,
		Type:      model.LogTypeConsume,
		Quota:     quota,
	}
	if err := model.LOG_DB.Create(&entry).Error; err != nil {
		t.Fatalf("failed to insert log: %v", err)
	}
}

func TestGetWechatMiniUsageOverviewReturnsExpectedShape(t *testing.T) {
	setupWechatMiniUsageOverviewTest(t)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.Local)
	originalNow := wechatMiniNow
	wechatMiniNow = func() time.Time { return now }
	t.Cleanup(func() { wechatMiniNow = originalNow })
	userID := 7

	insertWechatMiniConsumeLog(t, userID, now.Add(-2*time.Hour).Unix(), 200)
	insertWechatMiniConsumeLog(t, userID, now.Add(-4*time.Hour).Unix(), 300)
	insertWechatMiniConsumeLog(t, userID, now.Add(-24*time.Hour).Unix(), 100)
	insertWechatMiniConsumeLog(t, userID, now.Add(-48*time.Hour).Unix(), 50)
	insertWechatMiniConsumeLog(t, userID, now.Add(-72*time.Hour).Unix(), 75)

	otherType := model.Log{UserId: userID, Username: "alice", CreatedAt: now.Add(-1 * time.Hour).Unix(), Type: model.LogTypeTopup, Quota: 999}
	if err := model.LOG_DB.Create(&otherType).Error; err != nil {
		t.Fatalf("failed to insert non-consume log: %v", err)
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("id", userID)
	ctx.Set("username", "alice")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/wechatmini/usage/overview", nil)

	GetWechatMiniUsageOverview(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var payload wechatMiniUsageOverviewAPIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !payload.Success {
		t.Fatalf("expected success=true")
	}
	if payload.Data.Today.CallCount != 2 {
		t.Fatalf("expected today call count 2, got %d", payload.Data.Today.CallCount)
	}
	if payload.Data.Today.Quota != 500 {
		t.Fatalf("expected today quota 500, got %d", payload.Data.Today.Quota)
	}
	if len(payload.Data.Last7DaysCalls) != 7 {
		t.Fatalf("expected 7 daily buckets, got %d", len(payload.Data.Last7DaysCalls))
	}
	if payload.Data.Stat.Quota != 725 {
		t.Fatalf("expected stat quota 725, got %d", payload.Data.Stat.Quota)
	}
}

func TestGetWechatMiniUsageAnalyticsReturnsModelDashboardData(t *testing.T) {
	setupWechatMiniUsageOverviewTest(t)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.Local)
	originalNow := wechatMiniNow
	wechatMiniNow = func() time.Time { return now }
	t.Cleanup(func() { wechatMiniNow = originalNow })
	userID := 7

	insertWechatMiniQuotaData(t, model.QuotaData{UserID: userID, Username: "alice", ModelName: "gpt-4.1", CreatedAt: now.Add(-2 * time.Hour).Unix(), Count: 3, Quota: 300, TokenUsed: 1200})
	insertWechatMiniQuotaData(t, model.QuotaData{UserID: userID, Username: "alice", ModelName: "claude-4", CreatedAt: now.Add(-24 * time.Hour).Unix(), Count: 2, Quota: 700, TokenUsed: 900})
	insertWechatMiniQuotaData(t, model.QuotaData{UserID: 99, Username: "bob", ModelName: "gpt-4.1", CreatedAt: now.Add(-2 * time.Hour).Unix(), Count: 10, Quota: 9999, TokenUsed: 9999})

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("id", userID)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/wechatmini/usage/analytics?range=7days", nil)

	GetWechatMiniUsageAnalytics(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var payload wechatMiniUsageAnalyticsAPIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !payload.Success {
		t.Fatalf("expected success=true")
	}
	if payload.Data.Range.Key != "7days" {
		t.Fatalf("expected 7days range, got %s", payload.Data.Range.Key)
	}
	if payload.Data.Range.TimeGranularity != "day" {
		t.Fatalf("expected day granularity, got %s", payload.Data.Range.TimeGranularity)
	}
	if payload.Data.Summary.TotalCount != 5 {
		t.Fatalf("expected total count 5, got %d", payload.Data.Summary.TotalCount)
	}
	if payload.Data.Summary.TotalQuota != 1000 {
		t.Fatalf("expected total quota 1000, got %d", payload.Data.Summary.TotalQuota)
	}
	if payload.Data.Summary.TotalTokens != 2100 {
		t.Fatalf("expected total tokens 2100, got %d", payload.Data.Summary.TotalTokens)
	}
	if len(payload.Data.CallTrend) != 7 {
		t.Fatalf("expected 7 trend buckets, got %d", len(payload.Data.CallTrend))
	}
	if len(payload.Data.ModelRanking) != 2 {
		t.Fatalf("expected 2 model ranking rows, got %d", len(payload.Data.ModelRanking))
	}
	if payload.Data.ModelRanking[0].ModelName != "gpt-4.1" {
		t.Fatalf("expected call ranking first model gpt-4.1, got %s", payload.Data.ModelRanking[0].ModelName)
	}
	if payload.Data.QuotaDistribution[0].ModelName != "claude-4" {
		t.Fatalf("expected quota distribution first model claude-4, got %s", payload.Data.QuotaDistribution[0].ModelName)
	}
	if len(payload.Data.Raw) != 2 {
		t.Fatalf("expected raw data to include only current user rows, got %d", len(payload.Data.Raw))
	}
}
