package bootstrap

import (
	"net"
	"testing"

	"github.com/alibaba/open-guardrail/internal/config"
)

func TestLocalBootstrapListener(t *testing.T) {
	for _, tc := range []struct {
		ip, publicURL string
		want          bool
	}{
		{"127.0.0.1", "", true},
		{"::1", "", true},
		{"0.0.0.0", "", false},
		{"::", "", false},
		{"192.0.2.1", "", false},
		{"127.0.0.1", "https://guardrail.example", false},
	} {
		if got := localBootstrap(config.Config{PublicURL: tc.publicURL}, &net.TCPAddr{IP: net.ParseIP(tc.ip), Port: 8080}); got != tc.want {
			t.Errorf("%s public=%s: got %v", tc.ip, tc.publicURL, got)
		}
	}
	if localBootstrap(config.Config{}, &net.UnixAddr{Name: "test", Net: "unix"}) {
		t.Fatal("unknown transport allowed local bootstrap")
	}
}
