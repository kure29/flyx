package handler

import (
	"testing"

	"go-backend/internal/store/model"
)

func TestKernelSupportsPostQuantumReality(t *testing.T) {
	for _, tc := range []struct {
		kernelType, installed string
		want                  bool
	}{
		{"xray", "25.5.16", true},
		{"xray", "v25.10.15", true},
		{"xray", "26.1", true},
		{"xray", "25.5.15", false},
		{"xray", "25.4", false},
		{"xray", "1.8.24", false},
		// Unknown installed versions do not count: Mihomo needs server support.
		{"xray", "", false},
		{"xray", "latest", false},
		{"xray", "Xray 25.10.15", false},
		{"singbox", "1.12.0", false},
		{"sing-box", "1.13.0", false},
	} {
		got := kernelSupportsPostQuantumReality(&model.NodeKernel{KernelType: tc.kernelType, InstalledVersion: tc.installed})
		if got != tc.want {
			t.Errorf("%s %q: got %v want %v", tc.kernelType, tc.installed, got, tc.want)
		}
	}
	if kernelSupportsPostQuantumReality(nil) {
		t.Error("missing kernel")
	}
}
