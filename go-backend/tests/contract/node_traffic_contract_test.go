package contract_test

import (
	"testing"
	"time"

	"go-backend/internal/auth"
)

// Each node's traffic this month, for the node list; admins only.
func TestNodeTrafficMonthContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	user, _ := auth.GenerateToken(2, "normal_user", 1, secret)
	node := seedKernelNode(t, r, "香港 HKT")

	hour := time.Now().UnixMilli() - time.Now().UnixMilli()%3600000
	if err := r.DB().Exec(`INSERT INTO node_traffic(node_id, hour, in_bytes, out_bytes) VALUES(?, ?, 300, 200)`, node, hour).Error; err != nil {
		t.Fatal(err)
	}
	month := kernelContractPost(t, router, admin, "/api/v1/node/traffic/month", `{"tzOffset":480}`).Data.(map[string]interface{})
	if v := month[jsonInt64(node)].([]interface{}); valueAsInt(v[0]) != 300 || valueAsInt(v[1]) != 200 {
		t.Fatalf("month traffic: %v", month)
	}
	if out := kernelContractPost(t, router, user, "/api/v1/node/traffic/month", `{}`); out.Code == 0 {
		t.Fatal("node traffic is for admins")
	}
}
