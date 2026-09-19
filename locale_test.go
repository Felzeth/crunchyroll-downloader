package main

import "testing"

func TestPrimaryLocale(t *testing.T) {
	tests := []struct {
		name     string
		langs    []string
		fallback string
		want     string
	}{
		{"first locale wins", []string{"en-US", "es-419"}, "ja-JP", "en-US"},
		{"all falls back", []string{"all"}, "en-US", "en-US"},
		{"empty falls back", nil, "en-US", "en-US"},
		{"all only matters in first position", []string{"ja-JP", "all"}, "en-US", "ja-JP"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := primaryLocale(tc.langs, tc.fallback); got != tc.want {
				t.Fatalf("primaryLocale(%v, %q) = %q, want %q", tc.langs, tc.fallback, got, tc.want)
			}
		})
	}
}
