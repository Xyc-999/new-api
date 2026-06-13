package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var wechatMiniFetchCode2Session = fetchWechatMiniCode2Session

func WechatMiniFetchCode2SessionForTest() func(string) (*WechatMiniCode2SessionResponse, error) {
	return wechatMiniFetchCode2Session
}

func SetWechatMiniFetchCode2SessionForTest(fn func(string) (*WechatMiniCode2SessionResponse, error)) {
	wechatMiniFetchCode2Session = fn
}

type wechatMiniLoginServerResponse struct {
	Success bool                           `json:"success"`
	Message string                         `json:"message"`
	Data    WechatMiniCode2SessionResponse `json:"data"`
}

func fetchWechatMiniCode2Session(code string) (*WechatMiniCode2SessionResponse, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("无效的参数")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(common.WeChatServerAddress), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("微信登录服务未配置")
	}
	bodyBytes, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/wechat/mini/session", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if common.WeChatServerToken != "" {
		req.Header.Set("Authorization", common.WeChatServerToken)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result wechatMiniLoginServerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if result.Message != "" {
			return nil, errors.New(result.Message)
		}
		return nil, fmt.Errorf("微信登录服务请求失败: %d", resp.StatusCode)
	}
	if !result.Success {
		if result.Message != "" {
			return nil, errors.New(result.Message)
		}
		return nil, fmt.Errorf("微信登录服务请求失败")
	}
	if result.Data.OpenID == "" {
		return nil, fmt.Errorf("openid missing")
	}
	return &result.Data, nil
}
