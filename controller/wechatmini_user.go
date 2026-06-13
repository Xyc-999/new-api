package controller

import "github.com/gin-gonic/gin"

func GetWechatMiniProfile(c *gin.Context) {
	GetSelf(c)
}
