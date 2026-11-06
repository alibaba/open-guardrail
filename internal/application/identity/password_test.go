package identity

import (
	"strings"
	"testing"
)

func TestPreviewPasswordPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"empty", "", false},
		{"three digits", "123", false},
		{"five characters", "abcde", false},
		{"six digits", "123456", true},
		{"six lowercase", "abcdef", true},
		{"three multibyte characters", "中文密", false},
		{"six multibyte characters", "中文密码测试", true},
		{"six emoji", strings.Repeat("😀", 6), true},
		{"128 bytes", strings.Repeat("a", 128), true},
		{"129 bytes", strings.Repeat("a", 129), false},
		{"multibyte byte limit", strings.Repeat("😀", 33), false},
		{"invalid UTF8", "abcdef\xff", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidPassword(tc.value); got != tc.valid {
				t.Fatalf("valid=%v want %v", got, tc.valid)
			}
		})
	}
}
