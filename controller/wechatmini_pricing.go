package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func GetWechatMiniPricingModels(c *gin.Context) {
	userGroup := "default"
	if userId := c.GetInt("id"); userId > 0 {
		if user, err := model.GetUserById(userId, false); err == nil {
			if g := user.Group; g != "" {
				userGroup = g
			}
		}
	}

	groupRatio := ratio_setting.GetGroupRatio(userGroup)

	pricing := model.GetPricing()
	items := make([]gin.H, 0, len(pricing))
	for _, item := range pricing {
		if !common.StringsContains(item.EnableGroup, userGroup) {
			continue
		}
		items = append(items, gin.H{
			"model_name":               item.ModelName,
			"description":              item.Description,
			"icon":                     item.Icon,
			"vendor_id":                item.VendorID,
			"quota_type":               item.QuotaType,
			"model_ratio":              item.ModelRatio,
			"completion_ratio":         item.CompletionRatio,
			"model_price":              item.ModelPrice,
			"cache_ratio":              item.CacheRatio,
			"create_cache_ratio":       item.CreateCacheRatio,
			"image_ratio":              item.ImageRatio,
			"audio_ratio":              item.AudioRatio,
			"audio_completion_ratio":   item.AudioCompletionRatio,
			"enable_groups":            item.EnableGroup,
			"supported_endpoint_types": item.SupportedEndpointTypes,
			"owner_by":                 item.OwnerBy,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":       items,
			"vendors":     model.GetVendors(),
			"user_group":  userGroup,
			"group_ratio": groupRatio,
		},
	})
}
