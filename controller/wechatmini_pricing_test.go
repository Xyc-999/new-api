package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type wechatMiniPricingAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items      []map[string]interface{} `json:"items"`
		Vendors    []map[string]interface{} `json:"vendors"`
		UserGroup  string                   `json:"user_group"`
		GroupRatio float64                  `json:"group_ratio"`
	} `json:"data"`
}

func setupWechatMiniPricingTest(t *testing.T) {
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
	if err := db.AutoMigrate(&model.Ability{}, &model.Model{}, &model.Vendor{}, &model.Channel{}, &model.User{}); err != nil {
		t.Fatalf("failed to migrate pricing tables: %v", err)
	}
	model.InvalidatePricingCache()
	t.Cleanup(func() {
		model.InvalidatePricingCache()
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestGetWechatMiniPricingModelsReturnsArray(t *testing.T) {
	setupWechatMiniPricingTest(t)
	channel := model.Channel{Id: 1, Type: 1, Key: "test-key", Name: "test-channel", Group: "default", Status: 1}
	if err := model.DB.Create(&channel).Error; err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	if err := model.DB.Create(&model.Ability{Group: "default", Model: "Claude 4 Sonnet", ChannelId: 1, Enabled: true}).Error; err != nil {
		t.Fatalf("insert ability: %v", err)
	}
	if err := model.DB.Create(&model.Vendor{Id: 3, Name: "Anthropic", Icon: "anthropic"}).Error; err != nil {
		t.Fatalf("insert vendor: %v", err)
	}
	if err := model.DB.Create(&model.Model{ModelName: "Claude 4 Sonnet", VendorID: 3}).Error; err != nil {
		t.Fatalf("insert model: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 7, Username: "alice", Group: "default"}).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("id", 7)
	ctx.Set("username", "alice")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/wechatmini/pricing/models", nil)

	GetWechatMiniPricingModels(ctx)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var payload wechatMiniPricingAPIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.Success {
		t.Fatalf("expected success=true")
	}
	if payload.Data.UserGroup != "default" {
		t.Fatalf("expected user_group=default, got %s", payload.Data.UserGroup)
	}
	if payload.Data.GroupRatio <= 0 {
		t.Fatalf("expected positive group_ratio, got %v", payload.Data.GroupRatio)
	}
	if len(payload.Data.Items) == 0 {
		t.Fatalf("expected at least one pricing model")
	}
	if payload.Data.Items[0]["model_name"] != "Claude 4 Sonnet" {
		t.Fatalf("expected first model name Claude 4 Sonnet, got %v", payload.Data.Items[0]["model_name"])
	}
	if payload.Data.Items[0]["vendor_name"] != "Anthropic" {
		t.Fatalf("expected vendor_name Anthropic, got %v", payload.Data.Items[0]["vendor_name"])
	}
	if len(payload.Data.Vendors) == 0 || payload.Data.Vendors[0]["name"] != "Anthropic" {
		t.Fatalf("expected vendors list to mirror web pricing vendors, got %v", payload.Data.Vendors)
	}
	if _, ok := payload.Data.Items[0]["quota_type"]; !ok {
		t.Fatalf("expected quota_type to be mirrored for mini pricing")
	}
	if _, ok := payload.Data.Items[0]["completion_ratio"]; !ok {
		t.Fatalf("expected completion_ratio to be mirrored for categorized pricing")
	}
	if _, ok := payload.Data.Items[0]["supported_endpoint_types"]; !ok {
		t.Fatalf("expected supported_endpoint_types to be mirrored for model classification")
	}
}

func TestGetWechatMiniPricingModelsFiltersByUserGroup(t *testing.T) {
	setupWechatMiniPricingTest(t)
	channel := model.Channel{Id: 1, Type: 1, Key: "test-key", Name: "test-channel", Group: "vip", Status: 1}
	if err := model.DB.Create(&channel).Error; err != nil {
		t.Fatalf("insert channel: %v", err)
	}
	if err := model.DB.Create(&model.Ability{Group: "vip", Model: "vip-only-model", ChannelId: 1, Enabled: true}).Error; err != nil {
		t.Fatalf("insert ability: %v", err)
	}
	if err := model.DB.Create(&model.Model{ModelName: "vip-only-model", VendorID: 0}).Error; err != nil {
		t.Fatalf("insert model: %v", err)
	}
	if err := model.DB.Create(&model.User{Id: 9, Username: "bob", Group: "default"}).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("id", 9)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/wechatmini/pricing/models", nil)

	GetWechatMiniPricingModels(ctx)

	var payload wechatMiniPricingAPIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	for _, item := range payload.Data.Items {
		if item["model_name"] == "vip-only-model" {
			t.Fatalf("default-group user should not see vip-only-model")
		}
	}
}
