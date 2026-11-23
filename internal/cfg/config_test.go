package cfg

import "testing"

func TestParseAliases(t *testing.T) {
	text := `# 注释行
守护星 = 守护你，遇见爱
《完美说谎者》：完美骗子
Carolina	Carolina (2022)

唯一行不算
`
	m := ParseAliases(text)
	if len(m) != 3 {
		t.Fatalf("want 3 entries, got %d: %v", len(m), m)
	}
	if m["守护星"] != "守护你，遇见爱" {
		t.Fatalf("= separated: %v", m["守护星"])
	}
	if m["完美说谎者"] != "完美骗子" {
		t.Fatalf("全角冒号 + 书名号: %v", m["完美说谎者"])
	}
	if m["Carolina"] != "Carolina (2022)" {
		t.Fatalf("tab separated: %v", m["Carolina"])
	}
}

func TestParseAliasesEmpty(t *testing.T) {
	if m := ParseAliases(""); len(m) != 0 {
		t.Fatalf("empty text should give empty map, got %v", m)
	}
}
