package controller

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestFetchWechatMiniCode2SessionRequiresWechatLoginServer(t *testing.T) {
	originalAddress := common.WeChatServerAddress
	originalToken := common.WeChatServerToken
	t.Cleanup(func() {
		common.WeChatServerAddress = originalAddress
		common.WeChatServerToken = originalToken
	})

	common.WeChatServerAddress = ""
	common.WeChatServerToken = ""

	_, err := fetchWechatMiniCode2Session("wx-code")
	if err == nil || !strings.Contains(err.Error(), "微信登录服务未配置") {
		t.Fatalf("expected missing wechat login server error, got %v", err)
	}
}
