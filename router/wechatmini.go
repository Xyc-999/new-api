package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterWechatMiniRouter(apiRouter *gin.RouterGroup) {
	wechatMiniRoute := apiRouter.Group("/wechatmini")
	{
		wechatMiniRoute.POST("/auth/login", middleware.CriticalRateLimit(), controller.WechatMiniLogin)
		wechatMiniRoute.POST("/auth/create-account", middleware.CriticalRateLimit(), controller.WechatMiniCreateAccount)
		wechatMiniRoute.POST("/auth/bind-existing", middleware.CriticalRateLimit(), controller.WechatMiniBindExisting)
		wechatMiniRoute.POST("/pay/notify", controller.WechatMiniPayNotify)

		authed := wechatMiniRoute.Group("/")
		authed.Use(middleware.UserAuth())
		{
			authed.GET("/user/profile", controller.GetWechatMiniProfile)
			authed.GET("/user/groups", controller.GetUserGroups)
			authed.GET("/log", controller.ListWechatMiniLogs)
			authed.GET("/topup", controller.ListWechatMiniTopups)
			authed.GET("/token", controller.ListWechatMiniTokens)
			authed.GET("/token/:id", controller.GetWechatMiniToken)
			authed.POST("/token", controller.CreateWechatMiniToken)
			authed.PUT("/token/:id", controller.UpdateWechatMiniToken)
			authed.POST("/token/:id/key", controller.RevealWechatMiniToken)
			authed.GET("/pay/options", controller.GetWechatMiniPayOptions)
			authed.POST("/pay/create", controller.WechatMiniPayCreate)
			authed.GET("/pay/order/:tradeNo", controller.GetWechatMiniPayOrder)
			authed.GET("/config/display", controller.GetWechatMiniDisplayConfig)
			authed.GET("/usage/overview", controller.GetWechatMiniUsageOverview)
			authed.GET("/usage/analytics", controller.GetWechatMiniUsageAnalytics)
			authed.GET("/pricing/models", controller.GetWechatMiniPricingModels)
		}
	}
}
