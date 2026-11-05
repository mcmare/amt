package amt

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	orig := ConnectRequest{Token: strings.Repeat("x", 32), Address: "example.com:443"}
	if err := writeFrame(&buf, orig); err != nil {
		t.Fatal(err)
	}
	var got ConnectRequest
	if err := readFrame(&buf, &got); err != nil {
		t.Fatal(err)
	}
	if got != orig {
		t.Fatalf("got %+v, want %+v", got, orig)
	}
}
func TestFrameTooLarge(t *testing.T) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.BigEndian, uint32(maxFrame+1))
	var got ConnectResponse
	if err := readFrame(&buf, &got); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if err := writeFrame(&buf, ConnectRequest{Token: strings.Repeat("a", 5000)}); err == nil {
		t.Fatal("oversized outbound frame accepted")
	}
}
func TestTokenAuth(t *testing.T) {
	good := strings.Repeat("b", 64)
	if !tokenEqual(good, good) {
		t.Fatal("valid token rejected")
	}
	for _, bad := range []string{"", strings.Repeat("b", 32), strings.Repeat("c", 64)} {
		if tokenEqual(bad, good) {
			t.Fatalf("accepted token %q", bad)
		}
	}
}
func TestDestinationValidation(t *testing.T) {
	for _, s := range []string{"127.0.0.1:0", "localhost:65536", "nosuchport", "example.com:-1", ":80"} {
		if _, _, err := validateAddress(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
	for _, s := range []string{"example.com:443", "[2001:4860:4860::8888]:53"} {
		if _, _, err := validateAddress(s); err != nil {
			t.Errorf("rejected %q: %v", s, err)
		}
	}
}
func TestExitPublicIPPolicy(t *testing.T) {
	denied := []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1", "169.254.169.254", "100.100.100.200", "198.18.0.1", "192.0.2.1", "::1", "fc00::1", "fe80::1", "2001:db8::1", "224.0.0.1"}
	for _, s := range denied {
		if isAllowedPublic(netip.MustParseAddr(s)) {
			t.Errorf("allowed prohibited address %s", s)
		}
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"}
	for _, s := range allowed {
		if !isAllowedPublic(netip.MustParseAddr(s)) {
			t.Errorf("blocked public address %s", s)
		}
	}
}
func TestLoopbackSOCKS(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1", "localhost"} {
		if !isLoopbackListen(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"", "0.0.0.0", "192.168.1.12", "::"} {
		if isLoopbackListen(s) {
			t.Fatal(s)
		}
	}
}
