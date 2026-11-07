package handler

import (
	"fmt"
	"go-backend/internal/kernel"
)

func validateOutboundIPVersion(kernelType, version, strategy string) error {
	policy, err := kernel.NormalizeOutboundIPStrategy(kernelType, strategy)
	if err != nil {
		return err
	}
	if policy != kernel.OutboundIPAuto && kernelType == kernel.TypeSingbox && versionBelow(version, 1, 12) {
		return fmt.Errorf("限制出站 IP 需要 sing-box 1.12 及以上，服务器安装的是 %s", version)
	}
	if (policy == kernel.OutboundPreferIPv4 || policy == kernel.OutboundPreferIPv6) && kernelType == kernel.TypeXray && versionBelow(version, 25, 6, 8) {
		return fmt.Errorf("优先 IP 自动回退需要 Xray 25.6.8 及以上，服务器安装的是 %s", version)
	}
	return nil
}
