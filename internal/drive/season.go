package drive

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// seasonPatterns 按优先级匹配季数。
var seasonPatterns = []struct {
	re   *regexp.Regexp
	conv func(string) (int, bool)
}{
	{regexp.MustCompile(`(?i)\bS(\d{1,2})(?:E\d{1,3})?\b`), parseArab},
	{regexp.MustCompile(`(?i)Season\s*(\d{1,2})`), parseArab},
	{regexp.MustCompile(`第([一二三四五六七八九十]{1,3})\s*季`), parseChinese},
	{regexp.MustCompile(`第([一二三四五六七八九十]{1,3})\s*部`), parseChinese},
}

// ParseSeason 从文本中解析季数，解析不到返回 1。
func ParseSeason(text string) int {
	for _, p := range seasonPatterns {
		if m := p.re.FindStringSubmatch(text); m != nil {
			if n, ok := p.conv(m[1]); ok && n > 0 {
				return n
			}
		}
	}
	return 1
}

func parseArab(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil
}

var chNum = map[rune]int{
	'一': 1, '二': 2, '三': 3, '四': 4, '五': 5,
	'六': 6, '七': 7, '八': 8, '九': 9, '十': 10,
}

// parseChinese 解析中文数字（支持 十、十一、二十、二十三 等）。
func parseChinese(s string) (int, bool) {
	r := []rune(s)
	if len(r) == 1 {
		if v, ok := chNum[r[0]]; ok {
			return v, true
		}
		return 0, false
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
