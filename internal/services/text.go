package services

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// DetectLanguage guesses the language of text from its script. It is a
// direction hint for the auto-detect mode, not a full language detector:
// Latin text is English, mixed Chinese and Latin text follows the majority.
func DetectLanguage(text string) Language {
	var han, kana, hangul, latinWords int
	inLatin := false
	for _, r := range text {
		isLatin := unicode.Is(unicode.Latin, r)
		if isLatin && !inLatin {
			latinWords++
		}
		inLatin = isLatin
		switch {
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			kana++
		case unicode.Is(unicode.Hangul, r):
			hangul++
		}
	}
	switch {
	case kana > 0:
		return LangJa
	case hangul > 0:
		return LangKo
	case han > 0 && han >= latinWords:
		return LangZh
	case latinWords > 0:
		return LangEn
	}
	return LangAuto
}

func isDictionaryQuery(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\n\r.!?;。！？；") || len(strings.Fields(text)) > 3 || utf8.RuneCountInString(text) > 60 {
		return false
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			// shortcut: short CJK phrases use dictionary mode; add an explicit mode if ambiguous phrases need it.
			return utf8.RuneCountInString(text) <= 6
		}
	}
	return true
}
