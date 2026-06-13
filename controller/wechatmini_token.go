package controller

import "github.com/gin-gonic/gin"

func ListWechatMiniTokens(c *gin.Context) {
	GetAllTokens(c)
}

func GetWechatMiniToken(c *gin.Context) {
	GetToken(c)
}

func CreateWechatMiniToken(c *gin.Context) {
	AddToken(c)
}

func UpdateWechatMiniToken(c *gin.Context) {
	c.Params = append(c.Params[:0], gin.Param{Key: "id", Value: c.Param("id")})
	UpdateToken(c)
}

func RevealWechatMiniToken(c *gin.Context) {
	GetTokenKey(c)
}
