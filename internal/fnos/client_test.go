package fnos

import (
	"context"
	"os"
	"testing"
)

func TestParseGUID(t *testing.T) {
	const id = "93ed1f200b614427b3eb9edfa7171526"
	for _, in := range []string{
		"http://vip.dsj6.cn:5666/v/tv/season/" + id,
		"/v/tv/season/" + id,
		id,
	} {
		if got := parseGUID(in); got != id {
			t.Fatalf("parseGUID(%q)=%q, want %q", in, got, id)
		}
	}
}

// TestRemoteEpisodes 在 NAS 上设置 FNOS_TEST=1 时运行真实只读接口测试。
// 凭证通过环境变量传入，测试不会打印凭证或 token。
func TestRemoteEpisodes(t *testing.T) {
	if testing.Short() || getenv("FNOS_TEST") != "1" {
		t.Skip("set FNOS_TEST=1 to run integration test")
	}
	c := New(getenv("FNOS_URL"), getenv("FNOS_USER"), getenv("FNOS_PASS"), getenv("FNOS_SECRET"), getenv("FNOS_API_KEY"))
	items, err := c.Episodes(context.Background(), "http://vip.dsj6.cn:5666/v/tv/season/93ed1f200b614427b3eb9edfa7171526", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 21 {
		t.Fatalf("episodes=%d, want 21", len(items))
	}
}

func TestRemoteXianNiBothURLs(t *testing.T) {
	if testing.Short() || getenv("FNOS_TEST") != "1" {
		t.Skip("set FNOS_TEST=1 to run integration test")
	}
	c := New(getenv("FNOS_URL"), getenv("FNOS_USER"), getenv("FNOS_PASS"), getenv("FNOS_SECRET"), getenv("FNOS_API_KEY"))
	cases := []string{
		"http://vip.dsj6.cn:5666/v/tv/0e5ca664728343ffaffadddc85df9df3",
		"http://vip.dsj6.cn:5666/v/tv/season/fca212cfeece496c858aeee7784edd45",
	}
	for _, u := range cases {
		items, err := c.Episodes(context.Background(), u, 1)
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		eps := map[int]bool{}
		for _, it := range items {
			if it.Episode > 0 {
				eps[it.Episode] = true
			}
		}
		if len(eps) != 155 || !eps[1] || !eps[155] {
			t.Fatalf("%s: count=%d e1=%v e155=%v", u, len(eps), eps[1], eps[155])
		}
	}
}

func getenv(k string) string { return os.Getenv(k) }
