package contract_test

import (
	"strings"
	"testing"

	"go-backend/internal/auth"
)

func TestSingboxSnellNeedsSingbox114Contract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	nodeID := seedKernelNode(t, r, "landing")
	install := func(version string) string {
		// The node is offline: the kernel is registered and stays pending.
		return kernelContractPost(t, router, admin, "/api/v1/kernel/install",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"singbox","version":"`+version+`"}`).Msg
	}
	createSnell := func(version int) (int, string) {
		out := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"singbox","name":"snell","protocol":"snell","port":6160,
			  "settings":{"snellVersion":`+jsonInt64(int64(version))+`}}`)
		return out.Code, out.Msg
	}

	install("1.12.8")
	if code, msg := createSnell(5); code == 0 || !strings.Contains(msg, "1.14") {
		t.Fatalf("sing-box 1.12 has no Snell: %d %s", code, msg)
	}

	install("1.14.3")
	if code, msg := createSnell(6); code != 0 {
		t.Fatalf("snell v6 on sing-box 1.14: %d %s", code, msg)
	}
	if v := mustQueryString(t, r, `SELECT version FROM node_kernel WHERE node_id = ?`, nodeID); v != "1.14.3" {
		t.Fatalf("kernel version = %q", v)
	}

	if msg := install("1.13.0"); !strings.Contains(msg, "内置 Snell") {
		t.Fatalf("downgrade below 1.14 must be refused: %s", msg)
	}
	if v := mustQueryString(t, r, `SELECT version FROM node_kernel WHERE node_id = ?`, nodeID); v != "1.14.3" {
		t.Fatalf("refused downgrade changed the version to %q", v)
	}
}
