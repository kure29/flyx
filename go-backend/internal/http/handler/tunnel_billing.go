package handler

import (
	"fmt"
	"math"
)

func validTunnelBilling(flow int64, ratio float64) bool {
	return (flow == 1 || flow == 2) && ratio > 0 && !math.IsNaN(ratio) && !math.IsInf(ratio, 0)
}

func parseTunnelBilling(req map[string]interface{}) (int64, float64, error) {
	flow, ratio := int64(1), 1.0
	if raw, present := req["flow"]; present {
		flow = asInt64(raw, 0)
	}
	if raw, present := req["trafficRatio"]; present {
		ratio = asFloat(raw, math.NaN())
	}
	if !validTunnelBilling(flow, ratio) {
		return 0, 0, fmt.Errorf("计费方向必须为 1 或 2，流量倍率必须为有限正数")
	}
	return flow, ratio, nil
}
