package repo

import (
	"database/sql"
	"testing"
	"time"

	"go-backend/internal/store/model"
)

func TestGetForwardRecordIncludesProxyProtocol(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.DB().Create(&model.Forward{
		UserID:        1,
		UserName:      "admin",
		Name:          "proxy-forward",
		TunnelID:      1,
		RemoteAddr:    "1.1.1.1:443",
		Strategy:      "fifo",
		CreatedTime:   now,
		UpdatedTime:   now,
		Status:        1,
		ProxyProtocol: 2,
	}).Error; err != nil {
		t.Fatalf("create forward: %v", err)
	}

	forwardID := mustRepoLastInsertID(t, r)
	record, err := r.GetForwardRecord(forwardID)
	if err != nil {
		t.Fatalf("GetForwardRecord: %v", err)
	}
	if record == nil {
		t.Fatalf("expected forward record")
	}
	if record.ProxyProtocol != 2 {
		t.Fatalf("expected proxyProtocol 2, got %d", record.ProxyProtocol)
	}
	if record.MaxConn != 0 {
		t.Fatalf("expected default maxConn 0, got %d", record.MaxConn)
	}
}

func TestListForwardsByTunnelIncludesProxyProtocol(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.DB().Create(&model.Forward{
		UserID:        1,
		UserName:      "admin",
		Name:          "proxy-forward",
		TunnelID:      7,
		RemoteAddr:    "1.1.1.1:443",
		Strategy:      "fifo",
		CreatedTime:   now,
		UpdatedTime:   now,
		Status:        1,
		ProxyProtocol: 2,
	}).Error; err != nil {
		t.Fatalf("create forward: %v", err)
	}

	records, err := r.ListForwardsByTunnel(7)
	if err != nil {
		t.Fatalf("ListForwardsByTunnel: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 forward record, got %d", len(records))
	}
	if records[0].ProxyProtocol != 2 {
		t.Fatalf("expected proxyProtocol 2, got %d", records[0].ProxyProtocol)
	}
}

func TestListForwardsByTunnelIncludesMaxConn(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.DB().Create(&model.Forward{
		UserID:      1,
		UserName:    "admin",
		Name:        "max-conn-forward",
		TunnelID:    8,
		RemoteAddr:  "1.1.1.1:443",
		Strategy:    "fifo",
		CreatedTime: now,
		UpdatedTime: now,
		Status:      1,
		MaxConn:     42,
	}).Error; err != nil {
		t.Fatalf("create forward: %v", err)
	}

	records, err := r.ListForwardsByTunnel(8)
	if err != nil {
		t.Fatalf("ListForwardsByTunnel: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 forward record, got %d", len(records))
	}
	if records[0].MaxConn != 42 {
		t.Fatalf("expected maxConn 42, got %d", records[0].MaxConn)
	}
}

func TestListActiveForwardsByUserTunnelIncludesMaxConn(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.DB().Create(&model.Forward{
		UserID:      2,
		UserName:    "user",
		Name:        "active-max-conn-forward",
		TunnelID:    9,
		RemoteAddr:  "1.1.1.1:443",
		Strategy:    "fifo",
		CreatedTime: now,
		UpdatedTime: now,
		Status:      1,
		MaxConn:     55,
	}).Error; err != nil {
		t.Fatalf("create forward: %v", err)
	}

	records, err := r.ListActiveForwardsByUserTunnel(2, 9)
	if err != nil {
		t.Fatalf("ListActiveForwardsByUserTunnel: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 forward record, got %d", len(records))
	}
	if records[0].MaxConn != 55 {
		t.Fatalf("expected maxConn 55, got %d", records[0].MaxConn)
	}
}

func TestForwardRepositoryPersistsProxyProtocolReceiveAndSend(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	forwardID, err := r.CreateForwardTx(1, "admin", "proxy-protocol-forward", 2, "1.1.1.1:443", "fifo", now, 1, []int64{3}, 24000, "", 0, 1, 2)
	if err != nil {
		t.Fatalf("CreateForwardTx: %v", err)
	}

	record, err := r.GetForwardRecord(forwardID)
	if err != nil {
		t.Fatalf("GetForwardRecord after create: %v", err)
	}
	if record.ProxyProtocolReceive != 1 || record.ProxyProtocolSend != 2 {
		t.Fatalf("expected proxyProtocol receive/send 1/2 after create, got %d/%d", record.ProxyProtocolReceive, record.ProxyProtocolSend)
	}

	if err := r.UpdateForward(forwardID, "proxy-protocol-forward", 2, "2.2.2.2:443", "fifo", now+1, 0, 2, 1); err != nil {
		t.Fatalf("UpdateForward: %v", err)
	}
	record, err = r.GetForwardRecord(forwardID)
	if err != nil {
		t.Fatalf("GetForwardRecord after update: %v", err)
	}
	if record.ProxyProtocolReceive != 2 || record.ProxyProtocolSend != 1 {
		t.Fatalf("expected proxyProtocol receive/send 2/1 after update, got %d/%d", record.ProxyProtocolReceive, record.ProxyProtocolSend)
	}

	records, err := r.ListForwardsByTunnel(2)
	if err != nil {
		t.Fatalf("ListForwardsByTunnel: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 listed record, got %d", len(records))
	}
	if records[0].ProxyProtocolReceive != 2 || records[0].ProxyProtocolSend != 1 {
		t.Fatalf("expected listed proxyProtocol receive/send 2/1, got %d/%d", records[0].ProxyProtocolReceive, records[0].ProxyProtocolSend)
	}
}

func TestForwardRepositoryMapsLegacyProxyProtocolToSend(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.DB().Create(&model.Forward{
		UserID:        2,
		UserName:      "user",
		Name:          "legacy-proxy-protocol-forward",
		TunnelID:      9,
		RemoteAddr:    "1.1.1.1:443",
		Strategy:      "fifo",
		CreatedTime:   now,
		UpdatedTime:   now,
		Status:        1,
		ProxyProtocol: 2,
	}).Error; err != nil {
		t.Fatalf("create legacy forward: %v", err)
	}
	forwardID := mustRepoLastInsertID(t, r)

	record, err := r.GetForwardRecord(forwardID)
	if err != nil {
		t.Fatalf("GetForwardRecord: %v", err)
	}
	if record.ProxyProtocolReceive != 0 || record.ProxyProtocolSend != 2 {
		t.Fatalf("expected legacy proxyProtocol to map to receive/send 0/2, got %d/%d", record.ProxyProtocolReceive, record.ProxyProtocolSend)
	}
}

func mustRepoLastInsertID(t *testing.T, r *Repository) int64 {
	t.Helper()
	var id int64
	if err := r.DB().Raw("SELECT last_insert_rowid()").Row().Scan(&id); err != nil {
		t.Fatalf("last_insert_rowid: %v", err)
	}
	if id <= 0 {
		t.Fatalf("invalid last_insert_rowid %d", id)
	}
	return id
}

func TestUpdateForwardClearsLegacyLimits(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	legacy := model.Forward{
		UserID: 1, UserName: "admin", Name: "legacy-limits", TunnelID: 2, RemoteAddr: "1.1.1.1:443",
		Strategy: "fifo", CreatedTime: now, UpdatedTime: now, Status: 1,
		SpeedID: sql.NullInt64{Int64: 3, Valid: true}, MaxConn: 7,
		IPSpeedID: sql.NullInt64{Int64: 4, Valid: true}, IPMaxConn: 5,
	}
	if err := r.DB().Create(&legacy).Error; err != nil {
		t.Fatalf("seed forward: %v", err)
	}
	if err := r.UpdateForward(legacy.ID, "legacy-limits", 2, "2.2.2.2:443", "fifo", now+1, 0, 0, 0); err != nil {
		t.Fatalf("UpdateForward: %v", err)
	}
	record, err := r.GetForwardRecord(legacy.ID)
	if err != nil {
		t.Fatalf("GetForwardRecord: %v", err)
	}
	if record.SpeedID.Valid || record.IPSpeedID.Valid || record.MaxConn != 0 || record.IPMaxConn != 0 {
		t.Fatalf("speed and connection limits must be cleared: %+v", record)
	}
}
