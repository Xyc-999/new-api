package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type wechatMiniAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type wechatMiniLoginData struct {
	Mode               string `json:"mode"`
	UserID             int    `json:"user_id"`
	AccessToken        string `json:"access_token"`
	Created            bool   `json:"created"`
	PendingWechatToken string `json:"pending_wechat_token"`
}

func setupWechatMiniAuthTest(t *testing.T) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	originalRegisterEnabled := common.RegisterEnabled
	originalQuotaForNewUser := common.QuotaForNewUser
	originalWeChatAuthEnabled := common.WeChatAuthEnabled
	common.RegisterEnabled = true
	common.QuotaForNewUser = 0
	common.WeChatAuthEnabled = true
	t.Cleanup(func() {
		common.RegisterEnabled = originalRegisterEnabled
		common.QuotaForNewUser = originalQuotaForNewUser
		common.WeChatAuthEnabled = originalWeChatAuthEnabled
	})

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	model.DB = db
	model.LOG_DB = db
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("failed to migrate user table: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	originalFetch := WechatMiniFetchCode2SessionForTest()
	SetWechatMiniFetchCode2SessionForTest(func(code string) (*WechatMiniCode2SessionResponse, error) {
		return &WechatMiniCode2SessionResponse{OpenID: code}, nil
	})
	t.Cleanup(func() {
		SetWechatMiniFetchCode2SessionForTest(originalFetch)
	})

	engine := gin.New()
	wechatMiniRoute := engine.Group("/wechatmini")
	wechatMiniRoute.POST("/auth/login", WechatMiniLogin)
	wechatMiniRoute.POST("/auth/create-account", WechatMiniCreateAccount)
	wechatMiniRoute.POST("/auth/bind-existing", WechatMiniBindExisting)
	return engine
}

func performWechatMiniJSONRequest(t *testing.T, engine *gin.Engine, method string, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var requestBody []byte
	if body != nil {
		payload, err := common.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		requestBody = payload
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(requestBody))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeWechatMiniAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) wechatMiniAPIResponse {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code %d with body %s", recorder.Code, recorder.Body.String())
	}

	var response wechatMiniAPIResponse
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode api response: %v (body=%s)", err, recorder.Body.String())
	}
	return response
}

func decodeWechatMiniLoginData(t *testing.T, raw []byte) wechatMiniLoginData {
	t.Helper()
	var data wechatMiniLoginData
	if len(raw) == 0 {
		return data
	}
	if err := common.Unmarshal(raw, &data); err != nil {
		t.Fatalf("failed to decode login data: %v", err)
	}
	return data
}

func createWechatMiniAuthUser(t *testing.T, user *model.User) *model.User {
	t.Helper()
	if user.Role == 0 {
		user.Role = common.RoleCommonUser
	}
	if user.Status == 0 {
		user.Status = common.UserStatusEnabled
	}
	if err := user.Insert(0); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	return user
}

func countWechatMiniUsers(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := model.DB.Model(&model.User{}).Count(&count).Error; err != nil {
		t.Fatalf("failed to count users: %v", err)
	}
	return count
}

func findUserByWechatID(t *testing.T, wechatID string) model.User {
	t.Helper()
	var user model.User
	if err := model.DB.Where("wechat_id = ?", wechatID).First(&user).Error; err != nil {
		t.Fatalf("failed to find user by wechat id %s: %v", wechatID, err)
	}
	return user
}

func findUserByUsername(t *testing.T, username string) model.User {
	t.Helper()
	var user model.User
	if err := model.DB.Where("username = ?", username).First(&user).Error; err != nil {
		t.Fatalf("failed to find user by username %s: %v", username, err)
	}
	return user
}

func TestWechatMiniLoginReturnsBindOrCreateWithoutPendingToken(t *testing.T) {
	engine := setupWechatMiniAuthTest(t)

	response := decodeWechatMiniAPIResponse(t, performWechatMiniJSONRequest(t, engine, http.MethodPost, "/wechatmini/auth/login", map[string]string{"code": "wx-unbound"}))
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	data := decodeWechatMiniLoginData(t, response.Data)
	if data.Mode != "bind_or_create" {
		t.Fatalf("expected mode bind_or_create, got %q", data.Mode)
	}
	if data.PendingWechatToken != "" {
		t.Fatalf("expected no pending_wechat_token, got %q", data.PendingWechatToken)
	}
	if data.AccessToken != "" {
		t.Fatalf("expected access token to be empty, got %q", data.AccessToken)
	}
	if countWechatMiniUsers(t) != 0 {
		t.Fatalf("expected no users to be created for unbound wechat, got %d", countWechatMiniUsers(t))
	}
}

func TestWechatMiniLoginUsesWeChatAuthEnabledSwitch(t *testing.T) {
	engine := setupWechatMiniAuthTest(t)
	common.WeChatAuthEnabled = false

	response := decodeWechatMiniAPIResponse(t, performWechatMiniJSONRequest(t, engine, http.MethodPost, "/wechatmini/auth/login", map[string]string{"code": "wx-unbound"}))
	if response.Success {
		t.Fatal("expected login to be disabled when WeChatAuthEnabled is false")
	}
	if response.Message != "管理员未开启通过微信小程序登录以及注册" {
		t.Fatalf("expected disabled message, got %q", response.Message)
	}
}

func TestWechatMiniLoginStillLogsInBoundUser(t *testing.T) {
	engine := setupWechatMiniAuthTest(t)
	user := createWechatMiniAuthUser(t, &model.User{
		Username:     "bound_user",
		DisplayName:  "Bound User",
		WeChatId:     "wx-bound",
		WeChatOpenID: "wx-bound",
	})

	response := decodeWechatMiniAPIResponse(t, performWechatMiniJSONRequest(t, engine, http.MethodPost, "/wechatmini/auth/login", map[string]string{"code": "wx-bound"}))
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	data := decodeWechatMiniLoginData(t, response.Data)
	if data.Mode != "login" {
		t.Fatalf("expected mode login, got %q", data.Mode)
	}
	if data.UserID != user.Id {
		t.Fatalf("expected user id %d, got %d", user.Id, data.UserID)
	}
	if data.AccessToken == "" {
		t.Fatal("expected access token to be non-empty")
	}
}

func TestWechatMiniCreateAccountUsesCodeDirectly(t *testing.T) {
	engine := setupWechatMiniAuthTest(t)

	response := decodeWechatMiniAPIResponse(t, performWechatMiniJSONRequest(t, engine, http.MethodPost, "/wechatmini/auth/create-account", map[string]string{"code": "wx-create"}))
	if !response.Success {
		t.Fatalf("expected create-account success, got message: %s", response.Message)
	}

	data := decodeWechatMiniLoginData(t, response.Data)
	if data.Mode != "login" {
		t.Fatalf("expected mode login, got %q", data.Mode)
	}
	if data.UserID == 0 {
		t.Fatal("expected created user id to be non-zero")
	}
	if data.AccessToken == "" {
		t.Fatal("expected access token to be non-empty")
	}
	if !data.Created {
		t.Fatal("expected created flag to be true")
	}
	if countWechatMiniUsers(t) != 1 {
		t.Fatalf("expected one created user, got %d", countWechatMiniUsers(t))
	}

	createdUser := findUserByWechatID(t, "wx-create")
	if createdUser.GetAccessToken() != data.AccessToken {
		t.Fatalf("expected stored access token %q to match response %q", createdUser.GetAccessToken(), data.AccessToken)
	}

	reuseResponse := decodeWechatMiniAPIResponse(t, performWechatMiniJSONRequest(t, engine, http.MethodPost, "/wechatmini/auth/create-account", map[string]string{"code": "wx-create"}))
	if reuseResponse.Success {
		t.Fatal("expected create-account with already bound wechat identity to fail")
	}
	if reuseResponse.Message != "该微信已绑定其他账号" {
		t.Fatalf("expected already-bound message, got %q", reuseResponse.Message)
	}
	if countWechatMiniUsers(t) != 1 {
		t.Fatalf("expected user count to remain 1 after duplicate create-account, got %d", countWechatMiniUsers(t))
	}
}

func TestWechatMiniBindExistingUsesCodeDirectlyAndPreservesConflicts(t *testing.T) {
	engine := setupWechatMiniAuthTest(t)
	alice := createWechatMiniAuthUser(t, &model.User{Username: "alice", Password: "secret123", DisplayName: "Alice"})
	createWechatMiniAuthUser(t, &model.User{Username: "bob", Password: "secret123", DisplayName: "Bob"})
	createWechatMiniAuthUser(t, &model.User{Username: "charlie", Password: "secret123", DisplayName: "Charlie", WeChatId: "wx-other", WeChatOpenID: "wx-other"})

	bindResponse := decodeWechatMiniAPIResponse(t, performWechatMiniJSONRequest(t, engine, http.MethodPost, "/wechatmini/auth/bind-existing", map[string]string{
		"code":     "wx-bind",
		"account":  "alice",
		"password": "secret123",
	}))
	if !bindResponse.Success {
		t.Fatalf("expected bind-existing success, got message: %s", bindResponse.Message)
	}

	bindData := decodeWechatMiniLoginData(t, bindResponse.Data)
	if bindData.Mode != "login" {
		t.Fatalf("expected mode login, got %q", bindData.Mode)
	}
	if bindData.UserID != alice.Id {
		t.Fatalf("expected bound user id %d, got %d", alice.Id, bindData.UserID)
	}
	if bindData.AccessToken == "" {
		t.Fatal("expected access token to be non-empty")
	}
	if bindData.Created {
		t.Fatal("expected created flag to be false for existing account bind")
	}
	if countWechatMiniUsers(t) != 3 {
		t.Fatalf("expected no duplicate user creation, got %d users", countWechatMiniUsers(t))
	}

	boundAlice := findUserByUsername(t, "alice")
	if boundAlice.WeChatId != "wx-bind" {
		t.Fatalf("expected alice to be bound to wx-bind, got %q", boundAlice.WeChatId)
	}
	if boundAlice.GetAccessToken() != bindData.AccessToken {
		t.Fatalf("expected stored access token %q to match response %q", boundAlice.GetAccessToken(), bindData.AccessToken)
	}

	alreadyBoundResponse := decodeWechatMiniAPIResponse(t, performWechatMiniJSONRequest(t, engine, http.MethodPost, "/wechatmini/auth/bind-existing", map[string]string{
		"code":     "wx-bind",
		"account":  "bob",
		"password": "secret123",
	}))
	if alreadyBoundResponse.Success {
		t.Fatal("expected binding an already-bound wechat identity to fail")
	}
	if alreadyBoundResponse.Message != "该微信已绑定其他账号" {
		t.Fatalf("expected already-bound message, got %q", alreadyBoundResponse.Message)
	}
	bob := findUserByUsername(t, "bob")
	if bob.WeChatId != "" {
		t.Fatalf("expected bob to remain unbound, got %q", bob.WeChatId)
	}

	targetConflictResponse := decodeWechatMiniAPIResponse(t, performWechatMiniJSONRequest(t, engine, http.MethodPost, "/wechatmini/auth/bind-existing", map[string]string{
		"code":     "wx-fresh",
		"account":  "charlie",
		"password": "secret123",
	}))
	if targetConflictResponse.Success {
		t.Fatal("expected binding to account with conflicting wechat binding to fail")
	}
	if targetConflictResponse.Message != "该账号已绑定其他微信" {
		t.Fatalf("expected target-account conflict message, got %q", targetConflictResponse.Message)
	}
	charlie := findUserByUsername(t, "charlie")
	if charlie.WeChatId != "wx-other" {
		t.Fatalf("expected charlie to keep original wechat binding, got %q", charlie.WeChatId)
	}
	if countWechatMiniUsers(t) != 3 {
		t.Fatalf("expected user count to remain 3 after conflicts, got %d", countWechatMiniUsers(t))
	}
}
