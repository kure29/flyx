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

func TestNodeTrafficHistoryContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	user, _ := auth.GenerateToken(2, "normal_user", 1, secret)
	node1 := seedKernelNode(t, r, "东京 Tokyo")
	node2 := seedKernelNode(t, r, "新加坡 SG")

	bucket := time.Now().UnixMilli() - time.Now().UnixMilli()%(15*60*1000)
	if err := r.DB().Exec(`INSERT INTO node_traffic(node_id, hour, bucket_minutes, in_bytes, out_bytes) VALUES(?, ?, 15, 500, 300)`, node1, bucket).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`INSERT INTO node_traffic(node_id, hour, bucket_minutes, in_bytes, out_bytes) VALUES(?, ?, 15, 700, 100)`, node2, bucket).Error; err != nil {
		t.Fatal(err)
	}

	single := kernelContractPost(t, router, admin, "/api/v1/node/traffic/history", `{"nodeId":`+jsonInt64(node1)+`,"range":"hour","tzOffset":480}`).Data.(map[string]interface{})
	if valueAsInt(single["down"]) != 500 || valueAsInt(single["up"]) != 300 {
		t.Fatalf("single node history: %v", single)
	}
	if valueAsInt(single["since"]) <= 0 {
		t.Fatalf("expected period since timestamp for single node: %v", single)
	}

	all := kernelContractPost(t, router, admin, "/api/v1/node/traffic/history", `{"range":"day","tzOffset":480}`).Data.(map[string]interface{})
	if valueAsInt(all["down"]) != 1200 || valueAsInt(all["up"]) != 400 {
		t.Fatalf("aggregate node history: %v", all)
	}

	if out := kernelContractPost(t, router, user, "/api/v1/node/traffic/history", `{"range":"hour"}`); out.Code == 0 {
		t.Fatal("node traffic history is for admins")
	}

	// SumStatisticsFlowSince falls back to tunnel_metric when statistics_flow is empty.
	nowMs := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO tunnel_metric(tunnel_id, node_id, timestamp, bytes_in, bytes_out, connections, errors, avg_latency_ms) VALUES(1, ?, ?, 1024, 2048, 1, 0, 10)`, node1, nowMs).Error; err != nil {
		t.Fatal(err)
	}
	flow, err := r.SumStatisticsFlowSince(nowMs - 60*1000)
	if err != nil {
		t.Fatal(err)
	}
	if flow != 3072 {
		t.Fatalf("expected tunnel_metric fallback flow 3072, got %d", flow)
	}
}
