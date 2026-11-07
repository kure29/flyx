package contract_test

import (
	"strings"
	"testing"

	"go-backend/internal/auth"
)

func TestInboundOutboundIPStrategyContract(t *testing.T) {
	for _, policy := range []string{"ipv4_only", "ipv6_only", "prefer_ipv4", "prefer_ipv6"} {
		t.Run(policy, func(t *testing.T) {
			secret := "contract-egress-secret"
			router, r := setupContractRouter(t, secret)
			admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
			nodeID := seedKernelNode(t, r, "ipv6-entry")
			kernelContractPost(t, router, admin, "/api/v1/kernel/install", `{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","version":"latest"}`)
			base := `"name":"ipv6-entry","protocol":"shadowsocks","listen":"::","port":8388`
			out := kernelContractPost(t, router, admin, "/api/v1/inbound/create", `{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray",`+base+`,"outboundIpStrategy":"`+policy+`"}`)
			if out.Code != 0 {
				t.Fatalf("create: %+v", out)
			}
			id := mustQueryInt64(t, r, `SELECT id FROM node_inbound WHERE name = 'ipv6-entry'`)
			assertPolicy := func(want string) {
				t.Helper()
				if got := mustQueryString(t, r, `SELECT outbound_ip_strategy FROM node_inbound WHERE id = ?`, id); got != want {
					t.Fatalf("persisted policy = %q, want %q", got, want)
				}
				listed := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
				if !strings.Contains(mustJSON(t, listed.Data), `"outboundIpStrategy":"`+want+`"`) {
					t.Fatalf("policy not returned by list: %+v", listed)
				}
			}
			assertPolicy(policy)
			// Older clients omit the new field on edits: retain the saved policy.
			out = kernelContractPost(t, router, admin, "/api/v1/inbound/update", `{"id":`+jsonInt64(id)+`,`+base+`}`)
			if out.Code != 0 {
				t.Fatalf("update: %+v", out)
			}
			assertPolicy(policy)
			out = kernelContractPost(t, router, admin, "/api/v1/inbound/split", `{"id":`+jsonInt64(id)+`,"outboundIpStrategy":"invalid"}`)
			if out.Code == 0 {
				t.Fatal("invalid policy accepted")
			}
			assertPolicy(policy)
			out = kernelContractPost(t, router, admin, "/api/v1/inbound/split", `{"id":`+jsonInt64(id)+`,"outboundIpStrategy":"auto","splitRules":[]}`)
			if out.Code != 0 {
				t.Fatalf("reset: %+v", out)
			}
			assertPolicy("auto")
			out = kernelContractPost(t, router, admin, "/api/v1/inbound/update", `{"id":`+jsonInt64(id)+`,`+base+`,"extra":"{\"tag\":\"custom\"}","outboundIpStrategy":"auto"}`)
			if out.Code != 0 {
				t.Fatalf("automatic advanced configuration: %+v", out)
			}
			out = kernelContractPost(t, router, admin, "/api/v1/inbound/split", `{"id":`+jsonInt64(id)+`,"outboundIpStrategy":"`+policy+`"}`)
			if out.Code == 0 || !strings.Contains(out.Msg, "tag") {
				t.Fatalf("split endpoint must reject conflicting advanced routing before saving: %+v", out)
			}
			assertPolicy("auto")
			// Existing database callers receive the migration's automatic default.
			if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id,node_id,name,protocol,port,created_time,updated_time) SELECT kernel_id,node_id,'legacy','shadowsocks',8390,created_time,updated_time FROM node_inbound WHERE id = ?`, id).Error; err != nil {
				t.Fatal(err)
			}
			if got := mustQueryString(t, r, `SELECT outbound_ip_strategy FROM node_inbound WHERE name = 'legacy'`); got != "auto" {
				t.Fatalf("migration default = %q", got)
			}
		})
	}
}
