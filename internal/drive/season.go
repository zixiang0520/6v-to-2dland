package drive

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// seasonPatterns 按优先级匹配季数。
// 注意：Go 的 RE2 里 \b 只认 ASCII 词边界，紧跟汉字的「S02」用 \b 会失效，
// 这里统一用「左邻非字母数字」做边界，与 organize.go 的集数正则保持一致。
var seasonPatterns = []struct {
	re   *regexp.Regexp
	conv func(string) (int, bool)
}{
	// S02 / S02E05
	{regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])S(\d{1,2})(?:E\d{1,3})?(?:[^A-Za-z0-9]|$)`), parseArab},
	{regexp.MustCompile(`(?i)Season\s*(\d{1,2})`), parseArab},
	// 第2季 / 第二季（旧版只认中文数字，阿拉伯数字的「第2季」会被漏成第1季）
	{regexp.MustCompile(`第\s*(\d{1,3})\s*[季部]`), parseArab},
	{regexp.MustCompile(`第([一二三四五六七八九十百]{1,4})\s*[季部]`), parseChinese},
}

// ParseSeason 从文本中解析季数，解析不到返回 1。
func ParseSeason(text string) int {
	if n, ok := ParseSeasonOK(text); ok {
		return n
	}
	return 1
}

// ParseSeasonOK 解析季数并返回是否命中；未命中返回 (0,false)。
// 调用方需要区分「第1季」和「没有任何季信息」时用这个。
func ParseSeasonOK(text string) (int, bool) {
	for _, p := range seasonPatterns {
		if m := p.re.FindStringSubmatch(text); m != nil {
			if n, ok := p.conv(m[1]); ok && n > 0 {
				return n, true
			}
		}
	}
	return 0, false
}

func parseArab(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil
}

var chNum = map[rune]int{
	'一': 1, '二': 2, '三': 3, '四': 4, '五': 5,
	'六': 6, '七': 7, '八': 8, '九': 9, '十': 10,
	'百': 100,
}

// parseChinese 解析中文数字（支持 十、十一、二十、二十三、一百 等）。
func parseChinese(s string) (int, bool) {
	r := []rune(s)
	if len(r) == 0 {
		return 0, false
	}
	// 纯单字
	if len(r) == 1 {
		if v, ok := chNum[r[0]]; ok {
			return v, true
		}
		return 0, false
	}
	if strings.ContainsRune(s, '百') {
		// 一百 / 一百二十：剧集季数极少见，做简单支持
		parts := strings.SplitN(s, "百", 2)
		n := 1
		if parts[0] != "" {
			if v, ok := chNum[[]rune(parts[0])[0]]; ok {
				n = v
			}
		}
		val := n * 100
		if len(parts) > 1 && parts[1] != "" {
			if v, ok := parseChinese(parts[1]); ok {
				val += v
			}
		}
		return val, true
	}
	if strings.ContainsRune(s, '十') {
		parts := strings.SplitN(s, "十", 2)
		ten := 1
		if parts[0] != "" {
			if v, ok := chNum[[]rune(parts[0])[0]]; ok {
				ten = v
			}
		}
		val := ten * 10
		if len(parts) > 1 && parts[1] != "" {
			if v, ok := chNum[[]rune(parts[1])[0]]; ok {
				val += v
			}
		}
		return val, true
	}
	return 0, false
}

// seasonFromMagnet 从磁力链 dn 参数与描述文本解析季数。
func seasonFromMagnet(magnet, desc string) int {
	text := desc
	if dn := extractDN(magnet); dn != "" {
		if dec, err := url.QueryUnescape(dn); err == nil {
			text += " " + dec
		}
	}
	return ParseSeason(text)
}

// extractDN 从 magnet 链接提取 dn 参数。
func extractDN(magnet string) string {
	m, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	return m.Query().Get("dn")
}
