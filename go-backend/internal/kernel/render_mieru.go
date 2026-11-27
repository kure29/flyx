package kernel

import (
	"errors"
	"fmt"
)

// renderMieru runs one mita process per inbound so every inbound owns its own
// port, credentials, listen address, MTU and DNS dual-stack policy cleanly.
// Each process runs in the foreground (`mita run`) with MITA_CONFIG_JSON_FILE,
// an isolated per-process MITA_UDS_PATH, and MITA_INSECURE_UDS=1 so no systemd
// unit or mita OS user is required.
func renderMieru(inbounds []Inbound) (Rendered, error) {
	var out Rendered
	for _, in := range inbounds {
		if in.Protocol != ProtocolMieru {
			return Rendered{}, fmt.Errorf("Mieru 内核不支持协议 %s", in.Protocol)
		}
		if in.Via != nil {
			return Rendered{}, errors.New("Mieru 内核不支持链式出站")
		}
		if len(in.Split) > 0 {
			return Rendered{}, errors.New("Mieru 内核不支持分流")
		}

		proto := "TCP"
		if in.Transport.Type == TransportUDP {
			proto = "UDP"
		}
		mtu := in.Settings.MTU
		if mtu == 0 {
			mtu = DefaultMieruMTU
		}

		cfg := map[string]interface{}{
			"portBindings": []interface{}{
				map[string]interface{}{
					"port":     in.Port,
					"protocol": proto,
				},
			},
			"users": []interface{}{
				map[string]interface{}{
					"name":     in.Settings.Username,
					"password": in.Settings.Password,
				},
			},
			"loggingLevel": "WARN",
			"mtu":          mtu,
		}
		// Empty listenIPAddress tells mita to listen on all interfaces
		// (dual-stack without failing on IPv4-only hosts).
		if in.Listen != "" && in.Listen != "::" {
			cfg["listenIPAddress"] = in.Listen
		}
		if dualStack := mieruDualStack(in.OutboundIPStrategy); dualStack != "" {
			cfg["dns"] = map[string]interface{}{"dualStack": dualStack}
		}
		if err := mergeExtra(cfg, in.Extra); err != nil {
			return Rendered{}, err
		}

		content, err := marshalConfig(cfg)
		if err != nil {
			return Rendered{}, err
		}

		name := fmt.Sprintf("mita-%d", in.ID)
		file := name + ".json"
		out.Files = append(out.Files, File{Name: file, Content: content})
		out.Processes = append(out.Processes, Process{
			Name: name,
			Args: []string{"run"},
			Env: map[string]string{
				"MITA_CONFIG_JSON_FILE": "{RUN_DIR}/" + file,
				"MITA_UDS_PATH":         "{RUN_DIR}/" + name + ".sock",
				"MITA_INSECURE_UDS":     "1",
				"MITA_LOG_NO_TIMESTAMP": "1",
			},
		})
	}
	out.Idle = len(out.Processes) == 0
	return out, nil
}

func mieruDualStack(strategy string) string {
	switch strategy {
	case OutboundIPv4Only:
		return "ONLY_IPv4"
	case OutboundIPv6Only:
		return "ONLY_IPv6"
	case OutboundPreferIPv4:
		return "PREFER_IPv4"
	case OutboundPreferIPv6:
		return "PREFER_IPv6"
	default:
		return ""
	}
}
