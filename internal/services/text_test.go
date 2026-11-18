package services

import "testing"

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		text string
		want Language
	}{
		{"你好", LangZh},
		{"今天天气很好，我们去公园散步吧。", LangZh},
		{"hello", LangEn},
		{"Artificial intelligence is transforming society.", LangEn},
		{"用 Go 写一个 CLI", LangZh},
		{"在 Go 中实现 hello world 的例子", LangZh},
		{"こんにちは", LangJa},
		{"안녕하세요", LangKo},
		{"123 456", LangAuto},
	}
	for _, c := range cases {
		if got := DetectLanguage(c.text); got != c.want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}
