package ocr

import "testing"

func TestCJKText(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"划 词 翻 译", "划词翻译"},
		{"自 动 检 测 语 言", "自动检测语言"},
		{"中文 English 混排", "中文 English 混排"},
		{"hello world", "hello world"},
		{"こ ん に ち は", "こんにちは"},
		{"안 녕 하 세 요", "안 녕 하 세 요"}, // Korean keeps its spaces
		{"第一行\n第 二 行", "第一行\n第二行"},
		{"文字 。", "文字。"},
		{"中文，English 混排。", "中文，English 混排。"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := cjkText(tt.in); got != tt.want {
			t.Errorf("cjkText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCleanText(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "control and zero-width characters",
			in:   "he\x00llo\u200b wor\u200dld\ufeff",
			want: "hello world",
		},
		{
			name: "replacement and private use",
			in:   "a\ufffdb\ue123c\ufffc",
			want: "abc",
		},
		{
			name: "line endings and tabs",
			in:   "one\r\ntwo\rthree\tfour",
			want: "one\ntwo\nthree four",
		},
		{
			name: "ragged whitespace",
			in:   "  one   \n\n\n  two  \n",
			want: "  one\n\n  two",
		},
		{
			name: "cjk spacing",
			in:   "划 词 翻 译 。",
			want: "划词翻译。",
		},
		{
			name: "ordinary punctuation survives",
			in:   `He said: "hi!" — (ok)? 100%.`,
			want: `He said: "hi!" — (ok)? 100%.`,
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		if got := cleanText(tt.in); got != tt.want {
			t.Errorf("%s: cleanText(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}
