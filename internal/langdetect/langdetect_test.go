package langdetect

import "testing"

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"chinese", "你好，世界", "zh"},
		{"japanese kana", "こんにちは", "ja"},
		{"japanese mixed", "漢字とひらがなの混ざった文", "ja"},
		{"kanji only is chinese", "東京", "zh"},
		{"korean", "안녕하세요", "ko"},
		{"russian", "Привет мир", "ru"},
		{"arabic", "مرحبا بالعالم", "ar"},
		{"thai", "สวัสดี", "th"},
		{"hebrew", "שלום", "he"},
		{"greek", "Γειά σου κόσμε", "el"},
		{"english", "Hello, world!", "latin"},
		{"french", "Bonjour le monde", "latin"},
		{"mixed chinese and latin", "使用 Go 编写", "zh"},
		{"mostly russian one han", "Привет 你", "ru"},
		{"digits only", "12345", "unknown"},
		{"empty", "", "unknown"},
		{"punctuation only", "?!...", "unknown"},
	}
	for _, tt := range tests {
		if got := Detect(tt.text); got != tt.want {
			t.Errorf("Detect(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}
