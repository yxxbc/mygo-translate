package services

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type libreTestTransport func(*http.Request) (*http.Response, error)

func (f libreTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLibreTranslate(t *testing.T) {
	const source = "First paragraph.\n\nSecond paragraph."
	p := NewLibreTranslateProvider()
	p.client.Transport = libreTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/local/translate" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		}
		var payload map[string]string
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["source"] != "auto" || payload["target"] != "zh" ||
			payload["q"] != source || payload["format"] != "text" {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"translatedText":"第一段。\n\n第二段。"}`)),
		}, nil
	})
	result, err := p.translate("http://localhost:5000/local/", QueryRequest{Text: source, ToLang: LangZh})
	if err != nil || result.Status != "success" || result.Translation != "第一段。\n\n第二段。" {
		t.Fatalf("result=%+v, error=%v", result, err)
	}
}

func TestLibreTranslateReportsUnsupportedPair(t *testing.T) {
	p := NewLibreTranslateProvider()
	p.client.Transport = libreTestTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(strings.NewReader(`{"error":"zh-TW is not supported"}`)),
		}, nil
	})
	_, err := p.translate("http://localhost:5000", QueryRequest{Text: "hello", ToLang: LangZhTw})
	if err == nil || !strings.Contains(err.Error(), "zh-TW is not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}
