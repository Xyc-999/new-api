package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type apiResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type miniSessionRequest struct {
	Code string `json:"code"`
}

type miniSessionResponse struct {
	OpenID     string `json:"openid"`
	SessionKey string `json:"session_key"`
	UnionID    string `json:"unionid,omitempty"`
	ErrCode    int    `json:"errcode,omitempty"`
	ErrMsg     string `json:"errmsg,omitempty"`
}

var httpClient = &http.Client{Timeout: 5 * time.Second}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/wechat/web/authorize", handleWebAuthorize)
	mux.HandleFunc("/api/wechat/web/callback", handleWebCallback)
	mux.HandleFunc("/api/wechat/user", requireToken(handleWebUser))
	mux.HandleFunc("/api/wechat/mini/session", requireToken(handleMiniSession))

	port := strings.TrimSpace(os.Getenv("WECHAT_LOGIN_SERVER_PORT"))
	if port == "" {
		port = "8090"
	}
	addr := ":" + port
	log.Printf("wechat login server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "", Data: "ok"})
}

func handleWebAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Message: "method not allowed"})
		return
	}
	appID := strings.TrimSpace(os.Getenv("WECHAT_WEB_APPID"))
	callbackURL := strings.TrimSpace(os.Getenv("WECHAT_WEB_CALLBACK_URL"))
	if appID == "" || callbackURL == "" {
		writeJSON(w, http.StatusOK, apiResponse{Success: false, Message: "wechat web oauth config incomplete"})
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		state = randomState()
	}
	params := url.Values{}
	params.Set("appid", appID)
	params.Set("redirect_uri", callbackURL)
	params.Set("response_type", "code")
	params.Set("scope", "snsapi_login")
	params.Set("state", state)
	http.Redirect(w, r, "https://open.weixin.qq.com/connect/qrconnect?"+params.Encode()+"#wechat_redirect", http.StatusFound)
}

func handleWebCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Message: "method not allowed"})
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeJSON(w, http.StatusOK, apiResponse{Success: false, Message: "missing code"})
		return
	}
	frontendURL := strings.TrimSpace(os.Getenv("WECHAT_WEB_FRONTEND_OAUTH_URL"))
	if frontendURL == "" {
		writeJSON(w, http.StatusOK, apiResponse{Success: false, Message: "wechat web frontend oauth url not configured"})
		return
	}
	target, err := url.Parse(frontendURL)
	if err != nil {
		writeJSON(w, http.StatusOK, apiResponse{Success: false, Message: err.Error()})
		return
	}
	query := target.Query()
	if query.Get("provider") == "" {
		query.Set("provider", "wechat")
	}
	query.Set("code", code)
	if state := strings.TrimSpace(r.URL.Query().Get("state")); state != "" {
		query.Set("state", state)
	}
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func handleWebUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Message: "method not allowed"})
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	wechatID, err := resolveWebWechatID(code)
	if err != nil {
		writeJSON(w, http.StatusOK, apiResponse{Success: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "", Data: wechatID})
}

func handleMiniSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Message: "method not allowed"})
		return
	}
	var req miniSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, apiResponse{Success: false, Message: "无效的请求"})
		return
	}
	session, err := resolveMiniSession(req.Code)
	if err != nil {
		writeJSON(w, http.StatusOK, apiResponse{Success: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, apiResponse{Success: true, Message: "", Data: session})
}

func requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(os.Getenv("WECHAT_LOGIN_SERVER_TOKEN"))
		if token != "" && r.Header.Get("Authorization") != token {
			writeJSON(w, http.StatusUnauthorized, apiResponse{Success: false, Message: "unauthorized"})
			return
		}
		next(w, r)
	}
}

func resolveWebWechatID(code string) (string, error) {
	if code == "" {
		return "", errors.New("无效的参数")
	}
	appID := strings.TrimSpace(os.Getenv("WECHAT_WEB_APPID"))
	appSecret := strings.TrimSpace(os.Getenv("WECHAT_WEB_APP_SECRET"))
	if appID == "" || appSecret == "" {
		return "", errors.New("wechat web app config incomplete")
	}
	token, err := fetchOAuthAccessToken(appID, appSecret, code)
	if err != nil {
		return "", err
	}
	if token.UnionID != "" {
		return token.UnionID, nil
	}
	if token.OpenID == "" {
		return "", errors.New("openid missing")
	}
	return token.OpenID, nil
}

func resolveMiniSession(code string) (*miniSessionResponse, error) {
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("无效的参数")
	}
	appID := firstNonEmpty(os.Getenv("WECHAT_MINI_APPID"), os.Getenv("WECHAT_APPID"))
	appSecret := firstNonEmpty(os.Getenv("WECHAT_MINI_APP_SECRET"), os.Getenv("WECHAT_APP_SECRET"))
	if appID == "" || appSecret == "" {
		return nil, errors.New("wechatmini app config incomplete")
	}
	params := url.Values{}
	params.Set("appid", appID)
	params.Set("secret", appSecret)
	params.Set("js_code", code)
	params.Set("grant_type", "authorization_code")
	resp, err := httpClient.Get("https://api.weixin.qq.com/sns/jscode2session?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body miniSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.ErrCode != 0 {
		if body.ErrMsg != "" {
			return nil, errors.New(body.ErrMsg)
		}
		return nil, errors.New("code2Session failed")
	}
	if body.OpenID == "" {
		return nil, errors.New("openid missing")
	}
	return &body, nil
}

type webOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	OpenID       string `json:"openid"`
	Scope        string `json:"scope"`
	UnionID      string `json:"unionid,omitempty"`
	ErrCode      int    `json:"errcode,omitempty"`
	ErrMsg       string `json:"errmsg,omitempty"`
}

func fetchOAuthAccessToken(appID string, appSecret string, code string) (*webOAuthTokenResponse, error) {
	params := url.Values{}
	params.Set("appid", appID)
	params.Set("secret", appSecret)
	params.Set("code", code)
	params.Set("grant_type", "authorization_code")
	resp, err := httpClient.Get("https://api.weixin.qq.com/sns/oauth2/access_token?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body webOAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.ErrCode != 0 {
		if body.ErrMsg != "" {
			return nil, errors.New(body.ErrMsg)
		}
		return nil, fmt.Errorf("wechat oauth failed")
	}
	return &body, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func randomState() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

func writeJSON(w http.ResponseWriter, status int, data apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("write response failed: %v", err)
	}
}
