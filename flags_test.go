package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestNormalizeArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "single dash alias",
			in:   []string{"-url", "u", "-sub-only"},
			want: []string{"-url", "u", "--subs-only"},
		},
		{
			name: "double dash alias",
			in:   []string{"--sub-only"},
			want: []string{"--subs-only"},
		},
		{
			name: "alias with true value",
			in:   []string{"--sub-only=true"},
			want: []string{"--subs-only=true"},
		},
		{
			name: "alias with false value",
			in:   []string{"-sub-only=false"},
			want: []string{"--subs-only=false"},
		},
		{
			name: "canonical flag untouched",
			in:   []string{"--subs-only"},
			want: []string{"--subs-only"},
		},
		{
			name: "unrelated flag untouched",
			in:   []string{"-url", "https://x"},
			want: []string{"-url", "https://x"},
		},
		{
			name: "program name and positional untouched",
			in:   []string{"crdl-windows.exe", "value"},
			want: []string{"crdl-windows.exe", "value"},
		},
		{
			name: "args after terminator untouched",
			in:   []string{"--", "-sub-only"},
			want: []string{"--", "-sub-only"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := append([]string(nil), tc.in...)
			normalizeArgs(got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeArgs(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestFlagAliasesAreHiddenAndResolvable guards the contract that aliases point
// at a real flag but are not themselves registered (or they would appear in -h).
func TestFlagAliasesAreHiddenAndResolvable(t *testing.T) {
	for alias, canonical := range flagAliases {
		if flag.Lookup(canonical) == nil {
			t.Fatalf("alias %q points at unknown flag %q", alias, canonical)
		}
		if flag.Lookup(alias) != nil {
			t.Fatalf("alias %q is registered as a real flag, so it would show in usage", alias)
		}
	}
}
