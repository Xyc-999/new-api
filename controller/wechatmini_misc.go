package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func GetWechatMiniDisplayConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota_per_unit":                common.QuotaPerUnit,
			"display_in_currency":           operation_setting.IsCurrencyDisplay(),
			"quota_display_type":            operation_setting.GetQuotaDisplayType(),
			"custom_currency_symbol":        operation_setting.GetGeneralSetting().CustomCurrencySymbol,
			"custom_currency_exchange_rate": operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate,
			"usd_exchange_rate":             operation_setting.USDExchangeRate,
		},
	})
}
