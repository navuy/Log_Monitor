package main

import "testing"

func TestParseRuleID(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    int64
		wantErr bool
	}{
		{name: "API route", path: "/api/rules/3", want: 3},
		{name: "legacy route", path: "/rules/42", want: 42},
		{name: "missing ID", path: "/api/rules/", wantErr: true},
		{name: "nested path", path: "/api/rules/3/extra", wantErr: true},
		{name: "unsupported path", path: "/other/3", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseRuleID(test.path)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseRuleID(%q) expected an error", test.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRuleID(%q) returned error: %v", test.path, err)
			}
			if got != test.want {
				t.Fatalf("parseRuleID(%q) = %d, want %d", test.path, got, test.want)
			}
		})
	}
}
