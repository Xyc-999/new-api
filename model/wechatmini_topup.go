package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"gorm.io/gorm"
)

type WechatMiniTopupCreditInput struct {
	UserID   int
	TradeNo  string
	Amount   int64
	Money    float64
	CallerIP string
}

type WechatMiniTopupCreditResult struct {
	UserID     int
	QuotaAdded int
	TopUpID    int
	Created    bool
}

func CreditWechatMiniTopup(input WechatMiniTopupCreditInput) (*WechatMiniTopupCreditResult, error) {
	if input.UserID == 0 || input.TradeNo == "" || input.Amount <= 0 || input.Money <= 0 {
		return nil, errors.New("invalid wechatmini topup payload")
	}

	quotaToAdd := int(float64(input.Amount) * common.QuotaPerUnit)
	if quotaToAdd <= 0 {
		return nil, errors.New("invalid wechatmini quota")
	}

	result := &WechatMiniTopupCreditResult{UserID: input.UserID, QuotaAdded: quotaToAdd}
	shouldLog := false

	err := DB.Transaction(func(tx *gorm.DB) error {
		var topUp TopUp
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where("trade_no = ?", input.TradeNo).First(&topUp).Error
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			topUp = TopUp{
				UserId:          input.UserID,
				Amount:          input.Amount,
				Money:           input.Money,
				TradeNo:         input.TradeNo,
				PaymentMethod:   PaymentMethodMiniappWechat,
				PaymentProvider: PaymentProviderMiniappWechat,
				CreateTime:      common.GetTimestamp(),
				Status:          common.TopUpStatusPending,
			}
			if err := tx.Create(&topUp).Error; err != nil {
				return err
			}
			result.Created = true
		}

		if topUp.UserId != input.UserID {
			return fmt.Errorf("topup user mismatch")
		}
		if topUp.PaymentProvider != PaymentProviderMiniappWechat || topUp.PaymentMethod != PaymentMethodMiniappWechat {
			return ErrPaymentMethodMismatch
		}
		if topUp.Amount != input.Amount {
			return fmt.Errorf("topup amount mismatch")
		}
		if topUp.Money != input.Money {
			return fmt.Errorf("topup money mismatch")
		}

		result.TopUpID = topUp.Id
		if topUp.Status == common.TopUpStatusSuccess {
			result.QuotaAdded = 0
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		if err := tx.Model(&User{}).Where("id = ?", input.UserID).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(&topUp).Error; err != nil {
			return err
		}
		shouldLog = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	if shouldLog {
		RecordTopupLog(input.UserID, fmt.Sprintf("wechatmini 充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), input.Money), input.CallerIP, PaymentMethodMiniappWechat, PaymentMethodMiniappWechat)
	}
	return result, nil
}
