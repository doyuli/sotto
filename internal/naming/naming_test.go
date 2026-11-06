package naming

import (
	"reflect"
	"strings"
	"testing"
)

func TestDetectStyle(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"getUserInfo", Camel},
		{"UserInfo", Camel},
		{"user_info", Snake},
		{"USER_INFO", Constant},
		{"user-info", Kebab},
		{"user name", None},
		{"userinfo", None},
		{"HTTPSERVER", None}, // all caps without a separator cannot be split
		{"你好世界", None},
		{"", None},
		{"123", None},
	}
	for _, tt := range tests {
		if got := DetectStyle(tt.in); got != tt.want {
			t.Errorf("DetectStyle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"getUserInfo", []string{"get", "User", "Info"}},
		{"HTTPServer", []string{"HTTP", "Server"}},
		{"getUserID42", []string{"get", "User", "ID", "42"}},
		{"user_info", []string{"user", "info"}},
		{"USER-INFO", []string{"USER", "INFO"}},
		{"user  name", []string{"user", "name"}},
		{"hello", []string{"hello"}},
		{"Hello, world!", []string{"Hello", "world"}},
		{"foo+bar.baz", []string{"foo", "bar", "baz"}},
		{"user's name", []string{"user", "s", "name"}},
		{"", nil},
		{"___", nil},
	}
	for _, tt := range tests {
		if got := SplitWords(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitWords(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormat(t *testing.T) {
	words := []string{"user", "name"}
	tests := []struct {
		style string
		want  string
	}{
		{LowerCamel, "userName"},
		{UpperCamel, "UserName"},
		{Snake, "user_name"},
		{Constant, "USER_NAME"},
		{Kebab, "user-name"},
		{"bogus", ""},
	}
	for _, tt := range tests {
		if got := Format(words, tt.style); got != tt.want {
			t.Errorf("Format(user name, %s) = %q, want %q", tt.style, got, tt.want)
		}
	}
	// The first word of a lowerCamel keeps its case only lowered.
	if got := Format([]string{"Get", "User", "Info"}, LowerCamel); got != "getUserInfo" {
		t.Errorf("Format lowerCamel = %q, want getUserInfo", got)
	}
	// Words after the first are normalized, so "ID" formats as "Id".
	if got := Format([]string{"user", "ID"}, LowerCamel); got != "userId" {
		t.Errorf("Format lowerCamel = %q, want userId", got)
	}
	// Digits pass through.
	if got := Format([]string{"user", "42"}, Constant); got != "USER_42" {
		t.Errorf("Format constant = %q, want USER_42", got)
	}
	if got := Format(nil, Snake); got != "" {
		t.Errorf("Format(nil) = %q, want empty", got)
	}
	// A full round trip: split an identifier and rebuild it.
	if got := Format(SplitWords("get_user_info"), UpperCamel); got != "GetUserInfo" {
		t.Errorf("round trip = %q, want GetUserInfo", got)
	}
	// Punctuation from a translated sentence must not leak into a copy
	// format: "Hello, world!" copies as "helloWorld", not "hello,World!".
	if got := Format(SplitWords("Hello, world!"), LowerCamel); got != "helloWorld" {
		t.Errorf("punctuation round trip = %q, want helloWorld", got)
	}
	if got := Format(SplitWords("Hello, world!"), Snake); got != "hello_world" {
		t.Errorf("punctuation round trip = %q, want hello_world", got)
	}
}

func TestStylesAndLabels(t *testing.T) {
	for _, style := range Styles {
		if _, ok := Labels[style]; !ok {
			t.Errorf("style %q has no label", style)
		}
		if strings.Contains(style, " ") {
			t.Errorf("style %q contains a space", style)
		}
	}
}
