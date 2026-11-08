package handler

import (
	"fmt"
	"strings"

	"go-backend/internal/store/model"
)

// Inbound speed limits: the agent caps an inbound's total upload and
// download with nftables on its port (go-gost/x/kernel/nft.go), so they work
// for every protocol and also cover traffic arriving through relays.

// minRateAgent is the first agent release that enforces speed limits; older
// agents ignore them.
var minRateAgent = [3]int{3, 9, 4}

// validateInboundRates checks speed limits (Mbit/s, 0 = none) before they are
// saved for an inbound on node.
func validateInboundRates(node *model.Node, up, down int) error {
	for _, v := range []int{up, down} {
		if v < 0 || v > model.MaxInboundRateMbps {
			return fmt.Errorf("限速应在 0 到 %d Mbps 之间（0 为不限）", model.MaxInboundRateMbps)
		}
	}
	if up == 0 && down == 0 || node == nil {
		return nil
	}
	if msg := strings.TrimSpace(node.NftError); msg != "" {
		return fmt.Errorf("该服务器无法使用限速：%s", msg)
	}
	if node.Version.Valid && versionBelow(node.Version.String, minRateAgent[0], minRateAgent[1], minRateAgent[2]) {
		return fmt.Errorf("限速需要服务器 agent %d.%d.%d 或更新，这台服务器是 %s，请先升级 agent",
			minRateAgent[0], minRateAgent[1], minRateAgent[2], node.Version.String)
	}
	return nil
}
