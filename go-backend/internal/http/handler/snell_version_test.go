package handler

import "testing"

func TestSnellServes(t *testing.T) {
	for _, tc := range []struct {
		version string
		client  int
		ok      bool
	}{
		{"5.0.1", 5, true},
		{"5.0.1", 4, true}, // v5 servers accept v4 clients
		{"5.0.1", 6, false},
		{"4.1.1", 5, false},
		{"4.1.1", 4, true},
		{"6.0.0", 6, true},
		{"6.0.0", 5, false}, // v6 servers reject v4 / v5 clients
		{"6.0.0", 4, false},
	} {
		if err := snellServes(tc.version, tc.client); (err == nil) != tc.ok {
			t.Errorf("snell %s, v%d client: err = %v", tc.version, tc.client, err)
		}
	}
}
