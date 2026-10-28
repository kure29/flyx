// Package identity holds the agent's installation names and legacy aliases.
package identity

import (
	"os"
	"path/filepath"
)

const AgentName = "flyx-agent"

func Env(name, legacy string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return os.Getenv(legacy)
}

// Old agents install an upgrade at their existing binary path. Following the
// running executable preserves its service, kernels, spool and rollback file.
func installation(executable string) (dir, binary, service string) {
	switch filepath.Base(executable) {
	case AgentName, "flux_agent":
		return filepath.Dir(executable), executable, filepath.Base(executable)
	default:
		return "/etc/flyx-agent", "/etc/flyx-agent/flyx-agent", AgentName
	}
}

func DataDir() string {
	executable, _ := os.Executable()
	dir, _, _ := installation(executable)
	return dir
}

func BinaryPath() string {
	executable, _ := os.Executable()
	_, binary, _ := installation(executable)
	return binary
}
