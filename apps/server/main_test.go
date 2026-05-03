package main

import (
	"reflect"
	"testing"
)

func TestParseAllowedOrigins(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "https://chat.example", []string{"https://chat.example"}},
		{"two", "https://a.example,https://b.example", []string{"https://a.example", "https://b.example"}},
		{"trailing comma is dropped (not a wildcard)", "https://a.example,", []string{"https://a.example"}},
		{"leading comma is dropped", ",https://a.example", []string{"https://a.example"}},
		{"whitespace around entries is trimmed", " https://a.example , https://b.example ", []string{"https://a.example", "https://b.example"}},
		{"only commas + whitespace produces empty slice", " , , ,", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAllowedOrigins(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseAllowedOrigins(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}
