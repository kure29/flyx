package socket

import "testing"

func TestVirtualInterface(t *testing.T) {
	for _, name := range []string{"lo", "docker0", "veth3f2a1b", "br-1a2b3c", "virbr0", "tun0", "wg0", "warp", "tailscale0", "cni0", "flannel.1", "cali1234"} {
		if !virtualInterface(name) {
			t.Errorf("%s must not be counted", name)
		}
	}
	for _, name := range []string{"eth0", "ens3", "enp1s0", "eno1", "venet0", "bond0", "ens18"} {
		if virtualInterface(name) {
			t.Errorf("%s must be counted", name)
		}
	}
}
