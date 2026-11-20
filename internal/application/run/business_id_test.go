package run

import (
	"strings"
	"testing"
)

func TestValidBusinessID(t *testing.T) {
	for _, value := range []string{"release-42", "\ufeffrelease", "发布/42 %?# +", strings.Repeat("a", 128), strings.Repeat("中", 42)} {
		if !ValidBusinessID(value) {
			t.Errorf("rejected %q", value)
		}
	}
	for _, value := range []string{"", " release", "\u0085release", "release ", "release\n42", "release\x7f", strings.Repeat("a", 129), strings.Repeat("中", 43), string([]byte{0xff})} {
		if ValidBusinessID(value) {
			t.Errorf("accepted %q", value)
		}
	}
}
