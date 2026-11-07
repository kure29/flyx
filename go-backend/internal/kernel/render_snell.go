package kernel

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ShadowTLSExtra is the extra-binary name the agent installs for ShadowTLS.
const ShadowTLSExtra = "shadow-tls"

// renderSnell gives every inbound its own snell-server process (Snell serves
// a single port and PSK). The binary's major version matches the inbounds'
// client version (v6 servers only serve v6 clients; see the handler). ShadowTLS-fronted inbounds bind Snell to loopback
// and add a shadow-tls v3 process on the public port.
func renderSnell(inbounds []Inbound) (Rendered, error) {
	var out Rendered
	needFront := false
	for _, in := range inbounds {
		if in.Protocol != ProtocolSnell {
			return Rendered{}, fmt.Errorf("Snell 内核不支持协议 %s", in.Protocol)
		}
		if in.Via != nil {
			return Rendered{}, errors.New("Snell 内核不支持链式出站")
		}
		if len(in.Split) > 0 {
			return Rendered{}, errors.New("Snell 内核不支持分流")
		}
		name := fmt.Sprintf("snell-%d", in.ID)
		listen := net.JoinHostPort(in.Listen, strconv.Itoa(in.Port))
		ipv6 := net.ParseIP(in.Listen).To4() == nil
		front := in.Security.Type == SecurityShadowTLS
		if front {
			listen = net.JoinHostPort("127.0.0.1", strconv.Itoa(in.Settings.BackendPort))
			ipv6 = false
		}

		var conf strings.Builder
		conf.WriteString("[snell-server]\n")
		fmt.Fprintf(&conf, "listen = %s\n", listen)
		fmt.Fprintf(&conf, "psk = %s\n", in.Settings.Password)
		fmt.Fprintf(&conf, "ipv6 = %t\n", ipv6)
		if in.Settings.SnellVersion == 6 {
			// v6 has no obfs setting; mode replaces it.
			if in.Settings.SnellMode != "" {
				fmt.Fprintf(&conf, "mode = %s\n", in.Settings.SnellMode)
			}
		} else {
			conf.WriteString("obfs = off\n")
		}

		file := name + ".conf"
		out.Files = append(out.Files, File{Name: file, Content: conf.String()})
		out.Processes = append(out.Processes, Process{Name: name, Args: []string{"-c", "{RUN_DIR}/" + file}})

		if front {
			needFront = true
			out.Processes = append(out.Processes, Process{
				Name: fmt.Sprintf("stls-%d", in.ID),
				Bin:  ShadowTLSExtra,
				Args: []string{
					"--v3", "server",
					"--listen", net.JoinHostPort(in.Listen, strconv.Itoa(in.Port)),
					"--server", listen,
					"--tls", net.JoinHostPort(in.Security.SNI, "443"),
					"--password", in.Security.ShadowTLSPassword,
				},
			})
		}
	}
	if needFront {
		out.Extras = []string{ShadowTLSExtra}
	}
	out.Idle = len(out.Processes) == 0
	return out, nil
}
