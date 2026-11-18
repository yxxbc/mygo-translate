package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"easydict-linux/internal/config"
)

type LibreTranslateProvider struct {
	client *http.Client
}

func NewLibreTranslateProvider() *LibreTranslateProvider {
	return &LibreTranslateProvider{client: &http.Client{Timeout: 15 * time.Second}}
}

func (*LibreTranslateProvider) ID() string   { return "libretranslate" }
func (*LibreTranslateProvider) Name() string { return "LibreTranslate" }

func (p *LibreTranslateProvider) Translate(req QueryRequest) (*ServiceResult, error) {
	return p.translate(config.Get().LibreTranslateURL, req)
}

func (p *LibreTranslateProvider) translate(endpoint string, req QueryRequest) (*ServiceResult, error) {
	base, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || base.Hostname() == "" || (base.Scheme != "http" && base.Scheme != "https") ||
		base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("请在设置中填写 LibreTranslate 服务地址")
	}

	from, to := req.FromLang, req.ToLang
	if from == "" {
		from = LangAuto
	}
	if to == "" || to == LangAuto {
		to = LangZh
	}
	if from == LangZh {
		from = "zh"
	}
	if to == LangZh {
		to = "zh"
	}
	body, err := json.Marshal(map[string]string{
		"q": strings.TrimSpace(req.Text), "source": string(from), "target": string(to), "format": "text",
	})
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Post(base.JoinPath("translate").String(), "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("无法连接 LibreTranslate，请确认服务已启动")
	}
	defer resp.Body.Close()

	var result struct {
		TranslatedText string `json:"translatedText"`
		Error          string `json:"error"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result)
	if resp.StatusCode != http.StatusOK {
		if decodeErr == nil && result.Error != "" {
			return nil, fmt.Errorf("LibreTranslate (%d) %s", resp.StatusCode, result.Error)
		}
		return nil, fmt.Errorf("LibreTranslate 请求失败 (HTTP %d)", resp.StatusCode)
	}
	if decodeErr != nil || strings.TrimSpace(result.TranslatedText) == "" {
		return nil, fmt.Errorf("LibreTranslate 返回了空译文或无效响应")
	}
	return &ServiceResult{
		ServiceID: p.ID(), ServiceName: p.Name(), Status: "success", Translation: result.TranslatedText,
	}, nil
}
