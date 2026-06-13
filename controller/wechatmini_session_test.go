package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestFetchWechatMiniCode2SessionSetsRequestTimeout(t *testing.T) {
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if _, ok := req.Context().Deadline(); !ok {
			return nil, errors.New("missing deadline")
		}
		return nil, context.DeadlineExceeded
	})
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})

	t.Setenv("WECHAT_APPID", "test-appid")
	t.Setenv("WECHAT_APP_SECRET", "test-secret")

	_, err := fetchWechatMiniCode2Session("wx-code")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}
