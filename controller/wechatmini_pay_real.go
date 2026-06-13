package controller

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type WechatMiniCode2SessionResponse struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid,omitempty"`
	ErrCode    int    `json:"errcode,omitempty"`
	ErrMsg     string `json:"errmsg,omitempty"`
}

type wechatMiniUnifiedOrderResponse struct {
	PrepayID string `json:"prepay_id"`
}

type wechatMiniNotifyBody struct {
	ID           string `json:"id"`
	EventType    string `json:"event_type"`
	ResourceType string `json:"resource_type"`
	Resource     struct {
		Algorithm      string `json:"algorithm"`
		Ciphertext     string `json:"ciphertext"`
		AssociatedData string `json:"associated_data"`
		Nonce          string `json:"nonce"`
	} `json:"resource"`
}

type WechatMiniNotifyResource struct {
	OutTradeNo    string `json:"out_trade_no"`
	TradeState    string `json:"trade_state"`
	TransactionID string `json:"transaction_id"`
	Amount        struct {
		Total      int `json:"total"`
		PayerTotal int `json:"payer_total"`
	} `json:"amount"`
}

var wechatMiniResolveOpenID = func(code string) (string, error) {
	appID := os.Getenv("WECHAT_APPID")
	appSecret := os.Getenv("WECHAT_APP_SECRET")
	if appID == "" || appSecret == "" {
		return "", fmt.Errorf("wechatmini app config incomplete")
	}
	params := url.Values{}
	params.Set("appid", appID)
	params.Set("secret", appSecret)
	params.Set("js_code", code)
	params.Set("grant_type", "authorization_code")
	resp, err := http.Get("https://api.weixin.qq.com/sns/jscode2session?" + params.Encode())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body WechatMiniCode2SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.ErrCode != 0 {
		if body.ErrMsg != "" {
			return "", errors.New(body.ErrMsg)
		}
		return "", fmt.Errorf("code2Session failed")
	}
	if body.OpenID == "" {
		return "", fmt.Errorf("openid missing")
	}
	return body.OpenID, nil
}

var wechatMiniCreateUnifiedOrder = func(tradeNo string, payMoney float64, openid string) (string, error) {
	return performWechatMiniUnifiedOrder(tradeNo, payMoney, openid)
}

var wechatMiniDecodeNotify = func(rawBody string, headers http.Header) (*WechatMiniNotifyResource, error) {
	return decodeWechatMiniNotify(rawBody, headers)
}

func WechatMiniResolveOpenIDForTest() func(string) (string, error)      { return wechatMiniResolveOpenID }
func SetWechatMiniResolveOpenIDForTest(fn func(string) (string, error)) { wechatMiniResolveOpenID = fn }
func WechatMiniCreateUnifiedOrderForTest() func(string, float64, string) (string, error) {
	return wechatMiniCreateUnifiedOrder
}
func SetWechatMiniCreateUnifiedOrderForTest(fn func(string, float64, string) (string, error)) {
	wechatMiniCreateUnifiedOrder = fn
}
func WechatMiniDecodeNotifyForTest() func(string, http.Header) (*WechatMiniNotifyResource, error) {
	return wechatMiniDecodeNotify
}
func SetWechatMiniDecodeNotifyForTest(fn func(string, http.Header) (*WechatMiniNotifyResource, error)) {
	wechatMiniDecodeNotify = fn
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func loadWechatMiniPrivateKey() (*rsa.PrivateKey, error) {
	privateKeyPEM := os.Getenv("WECHAT_PAY_PRIVATE_KEY")
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("invalid wechatmini private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("wechatmini private key is not RSA")
	}
	return rsaKey, nil
}

func buildWechatMiniPaySign(message string) (string, error) {
	rsaKey, err := loadWechatMiniPrivateKey()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func buildWechatMiniAPIAuthorization(method string, path string, body string) (string, error) {
	mchID := os.Getenv("WECHAT_PAY_MCH_ID")
	serialNo := os.Getenv("WECHAT_PAY_MCH_SERIAL_NO")
	if mchID == "" || serialNo == "" {
		return "", fmt.Errorf("wechatmini payment config incomplete")
	}
	nonceStr := common.GetRandomString(16)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	message := strings.Join([]string{method, path, timestamp, nonceStr, body, ""}, "\n")
	signature, err := buildWechatMiniPaySign(message)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",timestamp="%s",serial_no="%s",signature="%s"`, mchID, nonceStr, timestamp, serialNo, signature), nil
}

func createWechatMiniPayPackage(tradeNo string, prepayID string) (wechatMiniPayCreateResponse, error) {
	appID := os.Getenv("WECHAT_APPID")
	if appID == "" || prepayID == "" {
		return wechatMiniPayCreateResponse{}, fmt.Errorf("wechatmini payment config incomplete")
	}
	timeStamp := fmt.Sprintf("%d", common.GetTimestamp())
	nonceStr := "nonce_" + tradeNo
	pkg := "prepay_id=" + prepayID
	message := strings.Join([]string{appID, timeStamp, nonceStr, pkg, ""}, "\n")
	paySign, err := buildWechatMiniPaySign(message)
	if err != nil {
		return wechatMiniPayCreateResponse{}, err
	}
	return wechatMiniPayCreateResponse{TradeNo: tradeNo, TimeStamp: timeStamp, NonceStr: nonceStr, Package: pkg, SignType: "RSA", PaySign: paySign}, nil
}

func writeWechatMiniPaymentConfigError(c *gin.Context, err error) {
	c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
}

func performWechatMiniUnifiedOrder(tradeNo string, payMoney float64, openid string) (string, error) {
	appID := os.Getenv("WECHAT_APPID")
	mchID := os.Getenv("WECHAT_PAY_MCH_ID")
	notifyURL := os.Getenv("WECHAT_PAY_NOTIFY_URL")
	if appID == "" || mchID == "" || notifyURL == "" || openid == "" {
		return "", fmt.Errorf("wechatmini payment config incomplete")
	}

	payload := map[string]any{
		"appid":        appID,
		"mchid":        mchID,
		"description":  "WeChatMini TopUp",
		"out_trade_no": tradeNo,
		"notify_url":   notifyURL,
		"amount": map[string]any{
			"total":    wechatMiniPayMoneyToCents(payMoney),
			"currency": "CNY",
		},
		"payer": map[string]any{
			"openid": openid,
		},
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	path := "/v3/pay/transactions/jsapi"
	authorization, err := buildWechatMiniAPIAuthorization(http.MethodPost, path, string(bodyBytes))
	if err != nil {
		return "", err
	}
	client := service.GetHttpClient()
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.mch.weixin.qq.com"+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", authorization)
	req.Header.Set("Wechatpay-Serial", os.Getenv("WECHAT_PAY_MCH_SERIAL_NO"))
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result wechatMiniUnifiedOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || result.PrepayID == "" {
		return "", fmt.Errorf("wechatmini unified order failed")
	}
	return result.PrepayID, nil
}

func decodeWechatMiniNotify(rawBody string, headers http.Header) (*WechatMiniNotifyResource, error) {
	serialHeader := headers.Get("Wechatpay-Serial")
	timestamp := headers.Get("Wechatpay-Timestamp")
	nonce := headers.Get("Wechatpay-Nonce")
	signature := headers.Get("Wechatpay-Signature")
	certPath := os.Getenv("WECHAT_PAY_PLATFORM_CERT_PATH")
	apiV3Key := os.Getenv("WECHAT_PAY_API_V3_KEY")
	if serialHeader == "" || timestamp == "" || nonce == "" || signature == "" || certPath == "" || apiV3Key == "" {
		return nil, fmt.Errorf("wechatmini notify config incomplete")
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("invalid wechatmini platform cert")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("invalid wechatmini platform public key")
	}
	message := strings.Join([]string{timestamp, nonce, rawBody, ""}, "\n")
	hash := sha256.Sum256([]byte(message))
	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return nil, err
	}
	if err := rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, hash[:], sigBytes); err != nil {
		return nil, err
	}
	var notify wechatMiniNotifyBody
	if err := json.Unmarshal([]byte(rawBody), &notify); err != nil {
		return nil, err
	}
	cipherBlock, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(cipherBlock)
	if err != nil {
		return nil, err
	}
	cipherData, err := base64.StdEncoding.DecodeString(notify.Resource.Ciphertext)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, []byte(notify.Resource.Nonce), cipherData, []byte(notify.Resource.AssociatedData))
	if err != nil {
		return nil, err
	}
	var resource WechatMiniNotifyResource
	if err := json.Unmarshal(plaintext, &resource); err != nil {
		return nil, err
	}
	return &resource, nil
}
