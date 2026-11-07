package monitoring

import "testing"

func TestMonitoringRetentionDaysFromConfigMap(t *testing.T) {
	tests := []struct {
		name string
		cfg  map[string]string
		want int
	}{
		{"missing uses default", nil, 7},
		{"valid custom", map[string]string{ConfigMonitorRetentionDays: "3"}, 3},
		{"trimmed custom", map[string]string{ConfigMonitorRetentionDays: " 30 "}, 30},
		{"invalid uses default", map[string]string{ConfigMonitorRetentionDays: "abc"}, 7},
		{"too small uses default", map[string]string{ConfigMonitorRetentionDays: "0"}, 7},
		{"too large uses default", map[string]string{ConfigMonitorRetentionDays: "3651"}, 7},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MonitoringRetentionDaysFromConfigMap(tc.cfg); got != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, got)
			}
		})
	}
}

func TestNormalizeMonitoringRetentionDays(t *testing.T) {
	for _, value := range []string{"1", "7", "3650", " 30 "} {
		if got, err := NormalizeMonitoringRetentionDays(value); err != nil || got == "" {
			t.Fatalf("expected %q valid, got value=%q err=%v", value, got, err)
		}
	}

	for _, value := range []string{"", "0", "-1", "3651", "abc", "1.5"} {
		if got, err := NormalizeMonitoringRetentionDays(value); err == nil {
			t.Fatalf("expected %q invalid, got value=%q", value, got)
		}
	}
}
