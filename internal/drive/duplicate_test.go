package drive

import (
	"encoding/base32"
	"encoding/hex"
	"strings"
	"testing"
)

func base32StdUpper(raw []byte) string {
	return strings.TrimRight(base32.StdEncoding.EncodeToString(raw), "=")
}

func TestMagnetBtihHex(t *testing.T) {
	m := "magnet:?xt=urn:btih:0123456789ABCDEF0123456789ABCDEF01234567&dn=test"
	if got := MagnetBtih(m); got != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("hex btih = %q", got)
	}
}

func TestMagnetBtihBase32(t *testing.T) {
	// 用真实可解码的 base32：hex 0123456789abcdef 的 base32 编码（大写）
	want := "0123456789abcdef0123456789abcdef01234567"
	raw, _ := hex.DecodeString(want)
	b32 := base32StdUpper(raw)
	m := "magnet:?xt=urn:btih:" + b32 + "&dn=x"
	if got := MagnetBtih(m); got != want {
		t.Fatalf("base32 btih = %q, want %q", got, want)
	}
}

func TestMagnetBtihInvalid(t *testing.T) {
	cases := []string{
		"",
		"magnet:?dn=only-name",
		"magnet:?xt=urn:btih:short&dn=x",
		"magnet:?xt=urn:btih:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", // 40 位非 hex
		"http://example.com/file",
	}
	for _, m := range cases {
		if got := MagnetBtih(m); got != "" {
			t.Fatalf("MagnetBtih(%q) = %q, want empty", m, got)
		}
	}
}
