package kdocs

import "testing"

func TestValidateURL(t *testing.T) {
	for _, raw := range []string{
		"https://www.kdocs.cn/l/example",
		"https://kdocs.cn/l/example",
	} {
		if err := ValidateURL(raw); err != nil {
			t.Fatalf("ValidateURL(%q) returned %v", raw, err)
		}
	}
	for _, raw := range []string{
		"http://www.kdocs.cn/l/example",
		"https://kdocs.cn.example.com/l/example",
		"https://example.com/",
	} {
		if err := ValidateURL(raw); err == nil {
			t.Fatalf("ValidateURL(%q) accepted an invalid URL", raw)
		}
	}
}

func TestExtractResources(t *testing.T) {
	document := `<html><body>
		<h2>第一部电影</h2>
		<p><a href="https://pan.baidu.com/s/abc_DEF?from=share&pwd=a1B2">百度网盘</a></p>
		<h2>KDocs 自定义链接</h2>
		<div class="otl-paragraph"><a url="https://pan.baidu.com/s/kdocs_link?pwd=2d3E">百度网盘</a></div>
		<h2>第二部剧集</h2>
		<p>下载：https://pan.baidu.com/s/xyz-123</p>
		<p>提取码：9zX8</p>
		<p>同行标题 https://pan.baidu.com/share/init?surl=hello_world 密码: QWER</p>
		<p>重复：https://pan.baidu.com/s/xyz-123</p>
	</body></html>`

	got := ExtractResources(document)
	if len(got) != 4 {
		t.Fatalf("got %d resources, want 4: %#v", len(got), got)
	}
	assertResource(t, got[0], "第一部电影", "https://pan.baidu.com/s/abc_DEF?from=share&pwd=a1B2", "a1B2")
	assertResource(t, got[1], "KDocs 自定义链接", "https://pan.baidu.com/s/kdocs_link?pwd=2d3E", "2d3E")
	assertResource(t, got[2], "第二部剧集", "https://pan.baidu.com/s/xyz-123", "9zX8")
	assertResource(t, got[3], "同行标题", "https://pan.baidu.com/share/init?surl=hello_world", "QWER")
}

func assertResource(t *testing.T, got Resource, title, rawURL, pwd string) {
	t.Helper()
	if got.Title != title || got.URL != rawURL || got.Pwd != pwd {
		t.Errorf("got %#v, want title=%q url=%q pwd=%q", got, title, rawURL, pwd)
	}
}
