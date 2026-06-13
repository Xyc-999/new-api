package controller

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type wechatMiniLoginRequest struct {
	Code string `json:"code"`
}

type wechatMiniCreateAccountRequest struct {
	Code string `json:"code"`
}

type wechatMiniBindExistingRequest struct {
	Code     string `json:"code"`
	Account  string `json:"account"`
	Password string `json:"password"`
}

type wechatMiniLoginMode string

const (
	wechatMiniLoginModeLogin        wechatMiniLoginMode = "login"
	wechatMiniLoginModeBindOrCreate wechatMiniLoginMode = "bind_or_create"
)

type wechatMiniLoginResponse struct {
	Mode        wechatMiniLoginMode `json:"mode"`
	UserID      int                 `json:"user_id,omitempty"`
	AccessToken string              `json:"access_token,omitempty"`
	Created     bool                `json:"created,omitempty"`
}

type wechatMiniIdentity struct {
	WechatID     string
	WechatOpenID string
}

var wechatMiniLookupUser = func(wechatID string, user *model.User) error {
	return model.DB.Where("wechat_id = ? AND deleted_at IS NULL", wechatID).First(user).Error
}
var wechatMiniAuthenticateAccount = func(account string, password string, user *model.User) error {
	authenticatedUser := model.User{Username: strings.TrimSpace(account), Password: password}
	if err := authenticatedUser.ValidateAndFill(); err != nil {
		return err
	}
	*user = authenticatedUser
	return nil
}

func ensureWeChatUserAccessToken(user *model.User) error {
	if user.GetAccessToken() != "" {
		return nil
	}

	token, err := common.GenerateRandomKey(32)
	if err != nil {
		return err
	}
	user.SetAccessToken(token)
	if user.Id == 0 {
		return nil
	}
	return user.Update(false)
}

func resolveWechatMiniIdentity(code string) (*wechatMiniIdentity, error) {
	sessionInfo, err := wechatMiniFetchCode2Session(strings.TrimSpace(code))
	if err != nil {
		return nil, err
	}

	wechatID := strings.TrimSpace(sessionInfo.OpenID)
	wechatOpenID := strings.TrimSpace(sessionInfo.OpenID)
	if wechatID == "" {
		return nil, fmt.Errorf("openid missing")
	}

	return &wechatMiniIdentity{
		WechatID:     wechatID,
		WechatOpenID: wechatOpenID,
	}, nil
}

func findWechatMiniBoundUser(wechatID string) (*model.User, error) {
	user := model.User{}
	err := wechatMiniLookupUser(wechatID, &user)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func respondWechatMiniAuthError(c *gin.Context, message string) {
	c.JSON(http.StatusOK, gin.H{
		"message": message,
		"success": false,
	})
}

func respondWechatMiniLoginSuccess(c *gin.Context, user *model.User, created bool) {
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
		"data": wechatMiniLoginResponse{
			Mode:        wechatMiniLoginModeLogin,
			UserID:      user.Id,
			AccessToken: user.GetAccessToken(),
			Created:     created,
		},
	})
}

func WechatMiniLogin(c *gin.Context) {
	if os.Getenv("WECHAT_MINI_ENABLED") != "true" {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员未开启通过微信小程序登录以及注册",
			"success": false,
		})
		common.SysLog("未开启微信登录")
		return
	}

	var req wechatMiniLoginRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "无效的请求",
			"success": false,
		})
		return
	}

	identity, err := resolveWechatMiniIdentity(req.Code)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}

	boundUser, err := findWechatMiniBoundUser(identity.WechatID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	if boundUser == nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "",
			"success": true,
			"data": wechatMiniLoginResponse{
				Mode: wechatMiniLoginModeBindOrCreate,
			},
		})
		return
	}

	user := *boundUser
	if user.WeChatOpenID != identity.WechatOpenID {
		user.WeChatOpenID = identity.WechatOpenID
		if err := user.Update(false); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"message": err.Error(),
				"success": false,
			})
			return
		}
	}

	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "用户已被封禁",
			"success": false,
		})
		return
	}

	if err := ensureWeChatUserAccessToken(&user); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}

	respondWechatMiniLoginSuccess(c, &user, false)
}

func WechatMiniCreateAccount(c *gin.Context) {
	if os.Getenv("WECHAT_MINI_ENABLED") != "true" {
		respondWechatMiniAuthError(c, "管理员未开启通过微信小程序登录以及注册")
		return
	}

	var req wechatMiniCreateAccountRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		respondWechatMiniAuthError(c, "无效的请求")
		return
	}
	if !common.RegisterEnabled {
		respondWechatMiniAuthError(c, "管理员关闭了新用户注册")
		return
	}

	identity, err := resolveWechatMiniIdentity(req.Code)
	if err != nil {
		respondWechatMiniAuthError(c, err.Error())
		return
	}

	boundUser, err := findWechatMiniBoundUser(identity.WechatID)
	if err != nil {
		respondWechatMiniAuthError(c, err.Error())
		return
	}
	if boundUser != nil {
		respondWechatMiniAuthError(c, "该微信已绑定其他账号")
		return
	}

	user := model.User{
		Username:     "wechat_" + strconv.Itoa(model.GetMaxUserId()+1),
		DisplayName:  "WeChat User",
		Role:         common.RoleCommonUser,
		Status:       common.UserStatusEnabled,
		WeChatId:     identity.WechatID,
		WeChatOpenID: identity.WechatOpenID,
	}
	if err := ensureWeChatUserAccessToken(&user); err != nil {
		respondWechatMiniAuthError(c, err.Error())
		return
	}
	if err := user.Insert(0); err != nil {
		respondWechatMiniAuthError(c, err.Error())
		return
	}

	respondWechatMiniLoginSuccess(c, &user, true)
}

func WechatMiniBindExisting(c *gin.Context) {
	if os.Getenv("WECHAT_MINI_ENABLED") != "true" {
		respondWechatMiniAuthError(c, "管理员未开启通过微信小程序登录以及注册")
		return
	}

	var req wechatMiniBindExistingRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		respondWechatMiniAuthError(c, "无效的请求")
		return
	}

	identity, err := resolveWechatMiniIdentity(req.Code)
	if err != nil {
		respondWechatMiniAuthError(c, err.Error())
		return
	}

	boundUser, err := findWechatMiniBoundUser(identity.WechatID)
	if err != nil {
		respondWechatMiniAuthError(c, err.Error())
		return
	}
	if boundUser != nil {
		respondWechatMiniAuthError(c, "该微信已绑定其他账号")
		return
	}

	user := model.User{}
	if err := wechatMiniAuthenticateAccount(req.Account, req.Password, &user); err != nil {
		respondWechatMiniAuthError(c, "账号或密码错误")
		return
	}
	if (user.WeChatId != "" && user.WeChatId != identity.WechatID) || (user.WeChatOpenID != "" && user.WeChatOpenID != identity.WechatOpenID) {
		respondWechatMiniAuthError(c, "该账号已绑定其他微信")
		return
	}

	user.WeChatId = identity.WechatID
	user.WeChatOpenID = identity.WechatOpenID
	if err := user.Update(false); err != nil {
		respondWechatMiniAuthError(c, err.Error())
		return
	}
	if err := ensureWeChatUserAccessToken(&user); err != nil {
		respondWechatMiniAuthError(c, err.Error())
		return
	}

	respondWechatMiniLoginSuccess(c, &user, false)
}
