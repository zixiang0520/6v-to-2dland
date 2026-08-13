package site6v

import (
	"testing"
	"time"
)

func TestParseGvodDate(t *testing.T) {
	now := time.Date(2026, 8, 14, 15, 0, 0, 0, time.Local)
	got, ok := parseGvodDate("08-14", now)
	if !ok || got != "2026-08-14" {
		t.Fatalf("today = %q ok=%v", got, ok)
	}
	got, ok = parseGvodDate("08-13", now)
	if !ok || got != "2026-08-13" {
		t.Fatalf("yesterday = %q ok=%v", got, ok)
	}
	// 晚于今天 → 上一年（跨年）
	got, ok = parseGvodDate("12-24", now)
	if !ok || got != "2025-12-24" {
		t.Fatalf("wrap year = %q ok=%v", got, ok)
	}
	if _, ok := parseGvodDate("bad", now); ok {
		t.Fatal("bad date should fail")
	}
}

func TestRecentCutoff(t *testing.T) {
	now := time.Date(2026, 8, 14, 15, 0, 0, 0, time.Local)
	got := recentCutoff(now, 10)
	want := time.Date(2026, 8, 5, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("cutoff = %v want %v", got, want)
	}
}

func TestGvodItemRe(t *testing.T) {
	html := `<li><span>[08-14]</span><a href="/dy/2026-07-07/49937.html" target="_blank"><font color='#FF0000'>2025动作《火遮眼》</font></a></li>`
	m := gvodItemRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatal("no match")
	}
	if m[1] != "08-14" || m[2] != "/dy/2026-07-07/49937.html" {
		t.Fatalf("got %#v", m)
	}
	title := stripTags(m[3])
	if title != "2025动作《火遮眼》" {
		t.Fatalf("title = %q", title)
	}
}

func TestParseListDateFilter(t *testing.T) {
	cutoff := time.Date(2026, 8, 5, 0, 0, 0, 0, time.Local)
	in, ok := parseListDate("2026-08-10")
	if !ok || in.Before(cutoff) {
		t.Fatal("in-window date rejected")
	}
	old, ok := parseListDate("2026-07-01")
	if !ok || !old.Before(cutoff) {
		t.Fatal("old date should be before cutoff")
	}
}
