package controller

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type wechatMiniPayCreateRequest struct {
	Amount int64 `json:"amount"`
}

type wechatMiniPayCreateResponse struct {
	TradeNo    string  `json:"tradeNo"`
	Amount     int64   `json:"amount"`
	Money      float64 `json:"money"`
	QuotaAdded int     `json:"quota_added"`
	Currency   string  `json:"currency"`
	TimeStamp  string  `json:"timeStamp"`
	NonceStr   string  `json:"nonceStr"`
	Package    string  `json:"package"`
	SignType   string  `json:"signType"`
	PaySign    string  `json:"paySign"`
}

type wechatMiniPayNotifyRequest struct {
	TradeNo string  `json:"tradeNo"`
	Amount  int64   `json:"amount"`
	Money   float64 `json:"money"`
}

type wechatMiniPayOrderResponse struct {
	TradeNo    string  `json:"tradeNo"`
	Status     string  `json:"status"`
	Amount     int64   `json:"amount"`
	Money      float64 `json:"money"`
	QuotaAdded int     `json:"quota_added"`
	Currency   string  `json:"currency"`
	PaidAt     int64   `json:"paid_at"`
}

type wechatMiniPayOptionResponse struct {
	Amount      int64   `json:"amount"`
	Money       float64 `json:"money"`
	QuotaAdded  int     `json:"quota_added"`
	Discount    float64 `json:"discount"`
	Recommended bool    `json:"recommended"`
}

func isWechatMiniPayEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("WECHAT_MINI_PAY_ENABLED")))
	return value == "true" || value == "1" || value == "yes" || value == "on"
}

func respondWechatMiniPayDisabled(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": false, "message": "小程序支付暂未开启"})
}

func normalizeWechatMiniTopupAmount(amount int64) int64 {
	if operation_setting.GetQuotaDisplayType() != operation_setting.QuotaDisplayTypeTokens {
		return amount
	}
	normalized := decimal.NewFromInt(amount).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		IntPart()
	if normalized < 1 {
		return 1
	}
	return normalized
}

func calcWechatMiniQuotaAdded(amount int64) int {
	quota := decimal.NewFromInt(amount).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		IntPart()
	if quota <= 0 {
		return 0
	}
	return int(quota)
}

func roundWechatMiniPayMoney(value float64) float64 {
	return decimal.NewFromFloat(value).Round(2).InexactFloat64()
}

func wechatMiniPayMoneyToCents(value float64) int {
	return int(decimal.NewFromFloat(value).Mul(decimal.NewFromInt(100)).Round(0).IntPart())
}

func getWechatMiniPayOptionsForGroup(group string) []wechatMiniPayOptionResponse {
	amounts := operation_setting.GetPaymentSetting().AmountOptions
	if len(amounts) == 0 {
		amounts = []int{10, 20, 50, 100, 200, 500}
	}

	seen := map[int64]bool{}
	options := make([]wechatMiniPayOptionResponse, 0, len(amounts))
	for _, rawAmount := range amounts {
		amount := int64(rawAmount)
		if amount <= 0 || amount < getMinTopup() || seen[amount] {
			continue
		}
		seen[amount] = true
		normalizedAmount := normalizeWechatMiniTopupAmount(amount)
		quotaAdded := calcWechatMiniQuotaAdded(normalizedAmount)
		payMoney := roundWechatMiniPayMoney(getPayMoney(amount, group))
		if quotaAdded <= 0 || payMoney < 0.01 {
			continue
		}
		discount := 1.0
		if configured, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && configured > 0 {
			discount = configured
		}
		options = append(options, wechatMiniPayOptionResponse{
			Amount:      amount,
			Money:       payMoney,
			QuotaAdded:  quotaAdded,
			Discount:    discount,
			Recommended: false,
		})
	}

	sort.Slice(options, func(i, j int) bool { return options[i].Amount < options[j].Amount })
	if len(options) > 0 {
		options[len(options)/2].Recommended = true
	}
	return options
}

func GetWechatMiniPayOptions(c *gin.Context) {
	if !isWechatMiniPayEnabled() {
		respondWechatMiniPayDisabled(c)
		return
	}
	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取用户分组失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"currency":   "CNY",
			"min_amount": getMinTopup(),
			"items":      getWechatMiniPayOptionsForGroup(user.Group),
		},
	})
}

func WechatMiniPayCreate(c *gin.Context) {
	if !isWechatMiniPayEnabled() {
		respondWechatMiniPayDisabled(c)
		return
	}
	userId := c.GetInt("id")
	var req wechatMiniPayCreateRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.Amount <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "参数错误"})
		return
	}
	if req.Amount < int64(getMinTopup()) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("充值数量不能小于 %d", getMinTopup())})
		return
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取用户分组失败"})
		return
	}
	payMoney := roundWechatMiniPayMoney(getPayMoney(req.Amount, user.Group))
	if payMoney < 0.01 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "充值金额过低"})
		return
	}
	tradeNo := fmt.Sprintf("wxm_%d_%s", userId, common.GetRandomString(12))
	openid := strings.TrimSpace(user.WeChatOpenID)
	if openid == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "当前用户缺少 wechat_openid，请重新登录后再支付"})
		return
	}
	prepayID, err := wechatMiniCreateUnifiedOrder(tradeNo, payMoney, openid)
	if err != nil {
		writeWechatMiniPaymentConfigError(c, err)
		return
	}
	amountToStore := normalizeWechatMiniTopupAmount(req.Amount)
	topUp := &model.TopUp{
		UserId:          userId,
		Amount:          amountToStore,
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodMiniappWechat,
		PaymentProvider: model.PaymentProviderMiniappWechat,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	payPackage, err := createWechatMiniPayPackage(tradeNo, prepayID)
	if err != nil {
		writeWechatMiniPaymentConfigError(c, err)
		return
	}
	payPackage.Amount = req.Amount
	payPackage.Money = payMoney
	payPackage.QuotaAdded = calcWechatMiniQuotaAdded(amountToStore)
	payPackage.Currency = "CNY"
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    payPackage,
	})
}

func WechatMiniPayNotify(c *gin.Context) {
	bodyBytes, err := common.Marshal(map[string]any{})
	if c.Request != nil && c.Request.Body != nil {
		rawBody, readErr := io.ReadAll(c.Request.Body)
		if readErr != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": readErr.Error()})
			return
		}
		bodyBytes = rawBody
	}
	decoded, err := wechatMiniDecodeNotify(string(bodyBytes), c.Request.Header)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if decoded.TradeState != "SUCCESS" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "支付状态异常"})
		return
	}
	order := model.GetTopUpByTradeNo(strings.TrimSpace(decoded.OutTradeNo))
	if order == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "订单不存在"})
		return
	}
	expectedTotal := wechatMiniPayMoneyToCents(order.Money)
	if decoded.Amount.Total != 0 && decoded.Amount.Total != expectedTotal {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "topup amount mismatch"})
		return
	}
	if decoded.Amount.PayerTotal != 0 && decoded.Amount.PayerTotal != expectedTotal {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "topup amount mismatch"})
		return
	}
	result, err := model.CreditWechatMiniTopup(model.WechatMiniTopupCreditInput{UserID: order.UserId, TradeNo: order.TradeNo, Amount: order.Amount, Money: order.Money, CallerIP: c.ClientIP()})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"user_id": result.UserID, "quota_added": result.QuotaAdded, "topup_id": result.TopUpID, "transaction_id": decoded.TransactionID}})
}

func GetWechatMiniPayOrder(c *gin.Context) {
	if !isWechatMiniPayEnabled() {
		respondWechatMiniPayDisabled(c)
		return
	}
	tradeNo := strings.TrimSpace(c.Param("tradeNo"))
	if tradeNo == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "订单号不能为空"})
		return
	}
	order := model.GetTopUpByTradeNo(tradeNo)
	if order == nil || order.UserId != c.GetInt("id") {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "订单不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": wechatMiniPayOrderResponse{TradeNo: order.TradeNo, Status: order.Status, Amount: order.Amount, Money: order.Money, QuotaAdded: calcWechatMiniQuotaAdded(order.Amount), Currency: "CNY", PaidAt: order.CompleteTime}})
}
