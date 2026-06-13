package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func TestGetWechatMiniPayOptionsForGroupUsesPaymentSettings(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	originalPrice := operation_setting.Price
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalAmountOptions := append([]int(nil), operation_setting.GetPaymentSetting().AmountOptions...)
	originalDiscounts := make(map[int]float64, len(operation_setting.GetPaymentSetting().AmountDiscount))
	for key, value := range operation_setting.GetPaymentSetting().AmountDiscount {
		originalDiscounts[key] = value
	}
	defer func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.Price = originalPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		operation_setting.GetPaymentSetting().AmountOptions = originalAmountOptions
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
	}()

	common.QuotaPerUnit = 500000
	operation_setting.Price = 7.3
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	operation_setting.GetPaymentSetting().AmountOptions = []int{10, 20}
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{20: 0.5}

	options := getWechatMiniPayOptionsForGroup("default")
	if len(options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(options))
	}
	if options[0].Amount != 10 || options[0].QuotaAdded != 5000000 || options[0].Money != 73 {
		t.Fatalf("unexpected first option: %+v", options[0])
	}
	if options[1].Amount != 20 || options[1].QuotaAdded != 10000000 || options[1].Money != 73 || options[1].Discount != 0.5 {
		t.Fatalf("unexpected discounted option: %+v", options[1])
	}
}

func TestNormalizeWechatMiniTopupAmountInTokensDisplay(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	defer func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
	}()

	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens

	if got := normalizeWechatMiniTopupAmount(1500000); got != 3 {
		t.Fatalf("expected tokens amount to normalize to 3, got %d", got)
	}
	if got := calcWechatMiniQuotaAdded(3); got != 1500000 {
		t.Fatalf("expected normalized amount to credit raw quota, got %d", got)
	}
}

func TestWechatMiniPayDisabledByEnv(t *testing.T) {
	t.Setenv("WECHAT_MINI_PAY_ENABLED", "false")
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/wechatmini/pay/options", nil)

	GetWechatMiniPayOptions(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "小程序支付暂未开启") {
		t.Fatalf("expected disabled message, got %s", body)
	}
}
