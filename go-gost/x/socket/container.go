package socket

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/go-gost/x/identity"
)

// InContainer reports whether the agent runs from the Docker image, which
// sets FLYX_AGENT_CONTAINER=1 (legacy FLUX_AGENT_CONTAINER is accepted). There is no systemd in the container: an
// upgrade swaps the binary in the data volume and stops the agent, and the
// restart policy brings the container back up on the new binary.
func InContainer() bool {
	return identity.Env("FLYX_AGENT_CONTAINER", "FLUX_AGENT_CONTAINER") == "1"
}

// installAndRestartInContainer moves src over the agent binary (rename
// works while the old binary runs) and stops the agent a second later, once
// the panel has the reply.
func installAndRestartInContainer(src, binaryPath string) error {
	if err := os.Rename(src, binaryPath); err != nil {
		return fmt.Errorf("替换程序失败: %v", err)
	}
	go func() {
		time.Sleep(time.Second)
		// SIGTERM goes through the normal shutdown; exit code 0 still
		// restarts under --restart unless-stopped / always.
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	return nil
}
