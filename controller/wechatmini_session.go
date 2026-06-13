package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

var wechatMiniFetchCode2Session = fetchWechatMiniCode2Session

func WechatMiniFetchCode2SessionForTest() func(string) (*WechatMiniCode2SessionResponse, error) {
	return wechatMiniFetchCode2Session
}

func SetWechatMiniFetchCode2SessionForTest(fn func(string) (*WechatMiniCode2SessionResponse, error)) {
	wechatMiniFetchCode2Session = fn
}

func fetchWechatMiniCode2Session(code string) (*WechatMiniCode2SessionResponse, error) {
	appID := os.Getenv("WECHAT_APPID")
	appSecret := os.Getenv("WECHAT_APP_SECRET")
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("wechatmini app config incomplete")
	}
	params := url.Values{}
	params.Set("appid", appID)
	params.Set("secret", appSecret)
	params.Set("js_code", code)
	params.Set("grant_type", "authorization_code")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.weixin.qq.com/sns/jscode2session?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body WechatMiniCode2SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.ErrCode != 0 {
		if body.ErrMsg != "" {
			return nil, errors.New(body.ErrMsg)
		}
		return nil, fmt.Errorf("code2Session failed")
	}
	if body.OpenID == "" {
		return nil, fmt.Errorf("openid missing")
	}
	return &body, nil
}
