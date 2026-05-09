package http

import "testing"

func TestValidULID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		out  string
		ok   bool
	}{
		{
			name: "uppercase passes through",
			in:   "01ABCDEFGHJKMNPQRSTVWXYZ00",
			out:  "01ABCDEFGHJKMNPQRSTVWXYZ00",
			ok:   true,
		},
		{
			name: "lowercase upper-folded",
			in:   "01abcdefghjkmnpqrstvwxyz00",
			out:  "01ABCDEFGHJKMNPQRSTVWXYZ00",
			ok:   true,
		},
		{
			name: "mixed case upper-folded",
			in:   "01AbCdEfGhJkMnPqRsTvWxYz00",
			out:  "01ABCDEFGHJKMNPQRSTVWXYZ00",
			ok:   true,
		},
		{
			name: "too short rejected",
			in:   "01ABCDEFGHJKMNPQRSTVWXYZ0",
			out:  "",
			ok:   false,
		},
		{
			name: "too long rejected",
			in:   "01ABCDEFGHJKMNPQRSTVWXYZ000",
			out:  "",
			ok:   false,
		},
		{
			name: "non-alphanumeric rejected",
			in:   "01ABCDEFGHJKMNPQRSTVWXYZ0!",
			out:  "",
			ok:   false,
		},
		{
			name: "empty rejected",
			in:   "",
			out:  "",
			ok:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := validULID(tc.in)
			if ok != tc.ok {
				t.Fatalf("validULID(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if got != tc.out {
				t.Fatalf("validULID(%q) = %q, want %q", tc.in, got, tc.out)
			}
		})
	}
}
