package kernel

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestXraySSSubscriptionUsers(t *testing.T) {
	inbound := Inbound{ID: 1, Protocol: ProtocolShadowsocks, Port: 8388,
		Settings: Settings{Method: "aes-128-gcm", Password: "old-shared", SSSubscriptionUsers: true}}
	if err := Normalize(TypeXray, &inbound); err != nil || inbound.Settings.Password != "" {
		t.Fatalf("normalized settings: %+v; err=%v", inbound.Settings, err)
	}
	for _, invalid := range []struct {
		kernelType, method, extra string
	}{
		{TypeSingbox, "aes-128-gcm", ""},
		{TypeXray, "2022-blake3-chacha20-poly1305", ""},
		{TypeXray, "aes-128-gcm", `{"settings":{"password":"fallback"}}`},
	} {
		candidate := inbound
		candidate.Settings.Method = invalid.method
		candidate.Extra = json.RawMessage(invalid.extra)
		if err := Normalize(invalid.kernelType, &candidate); err == nil {
			t.Errorf("accepted unsupported configuration: %+v", invalid)
		}
	}

	first := User{ID: 7, Password: "subscriber-one"}
	second := User{ID: 8, Password: "subscriber-two"}
	inbound.Users = []User{first, second, {ID: 9}, {ID: userMarkMax + 1, Password: "unmetered"}}
	rendered, err := Render(TypeXray, []Inbound{inbound})
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Inbounds []struct {
			Port     int
			Settings struct {
				Password, Network string
				Users             []struct{ Email, Method, Password string }
			}
		}
	}
	if err := json.Unmarshal([]byte(rendered.Files[0].Content), &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Inbounds) != 1 || config.Inbounds[0].Port != inbound.Port || !slices.Equal(rendered.UserMarks, []int64{7, 8}) {
		t.Fatalf("expected both metered users on one port: %#v, %v", config, rendered.UserMarks)
	}
	settings := config.Inbounds[0].Settings
	if settings.Password != "" || settings.Network != "tcp,udp" || len(settings.Users) != 2 {
		t.Fatalf("unexpected SS settings: %+v", settings)
	}
	for idx, user := range []User{first, second} {
		account := settings.Users[idx]
		if account.Email != user.Email() || account.Password != user.Password || account.Method != inbound.Settings.Method {
			t.Errorf("account %d: %+v", user.ID, account)
		}
		if !strings.Contains(rendered.Files[0].Content, `"outboundTag": "`+userOutboundTag(user.ID)+`"`) {
			t.Errorf("account %d has no metered outbound", user.ID)
		}
	}
	if blocked, err := RenderWith(TypeXray, []Inbound{inbound}, RenderOptions{NoUserMarks: true}); err != nil ||
		len(blocked.UserMarks) != 0 || strings.Contains(blocked.Files[0].Content, `"in-1"`) {
		t.Fatalf("unmetered SS inlet must not listen: %v, %v", blocked, err)
	}
}

func TestXraySS2022SubscriptionUsers(t *testing.T) {
	for _, method := range []string{"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"} {
		t.Run(method, func(t *testing.T) {
			users := []User{{ID: 7, Password: "first-password"}, {ID: 8, Password: "second-password"}}
			inbound := Inbound{ID: 1, Protocol: ProtocolShadowsocks, Port: 8388, Users: users,
				Settings: Settings{Method: method, SSSubscriptionUsers: true}}
			if err := Normalize(TypeXray, &inbound); err != nil {
				t.Fatal(err)
			}
			checkKey := func(key string) {
				t.Helper()
				decoded, err := base64.StdEncoding.DecodeString(key)
				if err != nil || len(decoded) != SS2022KeyLen(method) {
					t.Fatal("incorrect SS2022 key encoding or length")
				}
			}
			checkKey(inbound.Settings.Password)
			rendered, err := Render(TypeXray, []Inbound{inbound})
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Inbounds []struct {
					Settings struct {
						Method, Password, Network string
						Clients                   []struct{ Email, Method, Password string }
					}
				}
			}
			if err := json.Unmarshal([]byte(rendered.Files[0].Content), &config); err != nil {
				t.Fatal(err)
			}
			if len(config.Inbounds) != 1 || !slices.Equal(rendered.UserMarks, []int64{7, 8}) {
				t.Fatal("expected one port and two counted users")
			}
			settings := config.Inbounds[0].Settings
			if settings.Method != method || settings.Password != inbound.Settings.Password || settings.Network != "tcp,udp" || len(settings.Clients) != 2 {
				t.Fatal("incorrect SS2022 server settings")
			}
			seen := map[string]bool{settings.Password: true}
			for index, user := range users {
				client := settings.Clients[index]
				checkKey(client.Password)
				if client.Email != user.Email() || client.Method != "" || seen[client.Password] {
					t.Fatal("expected distinct user keys with methods specified only at the server level")
				}
				seen[client.Password] = true
				if inbound.ForUser(user).Settings.Password != settings.Password+":"+client.Password {
					t.Fatal("subscription export did not use the configured user key")
				}
				rotated := user
				rotated.Password += "-rotated"
				if inbound.ForUser(rotated).Settings.Password == inbound.ForUser(user).Settings.Password {
					t.Fatal("rotating the subscription password must rotate its SS2022 key")
				}
			}
			blocked, err := RenderWith(TypeXray, []Inbound{inbound}, RenderOptions{NoUserMarks: true})
			if err != nil || len(blocked.UserMarks) != 0 || strings.Contains(blocked.Files[0].Content, `"in-1"`) {
				t.Fatal("SS2022 independent credentials must not be served without usage tracking")
			}
		})
	}
}

func TestRenderUsers(t *testing.T) {
	users := []User{{ID: 7, UUID: "11111111-1111-4111-8111-111111111111", Password: "userpw"}}
	vless := Inbound{ID: 1, Protocol: ProtocolVLESS, Port: 443,
		Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000", Flow: "xtls-rprx-vision"},
		Security: Security{Type: SecurityReality, SNI: "www.example.com", RealityDest: "www.example.com:443",
			RealityPrivateKey: "k", RealityShortIDs: []string{"ab"}},
		Users: users}
	ss := Inbound{ID: 2, Protocol: ProtocolShadowsocks, Port: 8388,
		Settings: Settings{Method: "2022-blake3-aes-128-gcm", Password: "c2VydmVyc2VydmVyc2VydmVy"}, Users: users}

	r, err := Render(TypeXray, []Inbound{vless, ss})
	if err != nil {
		t.Fatal(err)
	}
	cfg := r.Files[0].Content
	if !strings.Contains(cfg, `"00000000-0000-4000-8000-000000000000"`) || !strings.Contains(cfg, `"u7@flyx"`) ||
		strings.Count(cfg, `"flow": "xtls-rprx-vision"`) != 2 {
		t.Fatalf("xray must keep the owner and add the user with the same flow:\n%s", cfg)
	}
	if strings.Contains(cfg, "userpw") {
		t.Fatalf("shadowsocks stays single-user:\n%s", cfg)
	}

	hy2 := Inbound{ID: 3, Protocol: ProtocolHysteria2, Port: 443, Settings: Settings{Password: "owner"},
		Security: Security{Type: SecurityTLS, SNI: "a.example.com", CertPEM: "c", KeyPEM: "k"}, Users: users}
	naive := Inbound{ID: 4, Protocol: ProtocolNaive, Port: 444, Settings: Settings{Username: "me", Password: "owner"},
		Security: Security{Type: SecurityTLS, SNI: "a.example.com", CertPEM: "c", KeyPEM: "k"}, Users: users}
	r, err = Render(TypeSingbox, []Inbound{hy2, naive})
	if err != nil {
		t.Fatal(err)
	}
	cfg = r.Files[0].Content
	if !strings.Contains(cfg, `"name": "u7"`) || !strings.Contains(cfg, `"username": "u7"`) ||
		strings.Count(cfg, `"password": "owner"`) != 2 || strings.Count(cfg, `"password": "userpw"`) != 2 {
		t.Fatalf("sing-box users:\n%s", cfg)
	}

	// Removing the user changes the config, so the panel re-applies it.
	hy2.Users = nil
	r2, _ := Render(TypeSingbox, []Inbound{hy2, naive})
	if r2.Hash == r.Hash {
		t.Fatal("hash must change with the user list")
	}
}

func TestForUser(t *testing.T) {
	u := User{ID: 3, UUID: "u-uuid", Password: "u-pw"}
	in := Inbound{Protocol: ProtocolTUIC, Settings: Settings{UUID: "o-uuid", Password: "o-pw"}, Users: []User{u}}
	got := in.ForUser(u)
	if got.Settings.UUID != "u-uuid" || got.Settings.Password != "u-pw" || got.Users != nil {
		t.Fatalf("tuic for user: %+v", got)
	}
	snell := Inbound{Protocol: ProtocolSnell, Settings: Settings{Password: "psk"}}
	if snell.ForUser(u).Settings.Password != "psk" || snell.SupportsUsers() {
		t.Fatal("snell keeps the shared psk")
	}
	if (Inbound{Protocol: ProtocolNaive}).ForUser(u).Settings.Username != "u3" {
		t.Fatal("naive username")
	}
}

func TestRenderUserMarks(t *testing.T) {
	users := []User{{ID: 7, UUID: "11111111-1111-4111-8111-111111111111", Password: "userpw"}}
	vless := Inbound{ID: 1, Protocol: ProtocolVLESS, Port: 443,
		Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"}, Users: users}

	r, err := Render(TypeXray, []Inbound{vless})
	if err != nil {
		t.Fatal(err)
	}
	cfg := r.Files[0].Content
	if len(r.UserMarks) != 1 || r.UserMarks[0] != 7 || !strings.Contains(cfg, `"mark": 1180041223`) ||
		!strings.Contains(cfg, `"u7@flyx"`) || !strings.Contains(cfg, `"outboundTag": "flyx-u7"`) {
		t.Fatalf("xray must route the user through its marked outbound (%v):\n%s", r.UserMarks, cfg)
	}

	r, err = Render(TypeSingbox, []Inbound{vless})
	if err != nil {
		t.Fatal(err)
	}
	cfg = r.Files[0].Content
	if len(r.UserMarks) != 1 || !strings.Contains(cfg, `"routing_mark": 1180041223`) ||
		!strings.Contains(cfg, `"outbound": "flyx-u7"`) || !strings.Contains(cfg, `"final": "direct"`) {
		t.Fatalf("sing-box must route the user through its marked outbound:\n%s", cfg)
	}

	vless.Users = nil
	if r, _ := Render(TypeXray, []Inbound{vless}); len(r.UserMarks) != 0 || strings.Contains(r.Files[0].Content, "routing") {
		t.Fatal("no users, nothing to mark")
	}
	if UserMark(70000) != 0 || UserFromMark(UserMark(12)) != 12 || UserFromMark(0x12345678) != 0 {
		t.Fatal("mark round trip")
	}
}

// On servers that may not set socket marks (LXC/OpenVZ without
// CAP_NET_ADMIN) every marked dial fails with "operation not permitted", so
// subscription users could connect but reach nothing. With NoUserMarks they
// keep their credentials and leave through the default outbounds.
func TestRenderNoUserMarks(t *testing.T) {
	via := Endpoint{Host: "1.2.3.4", Port: 443, Direct: true, Proxy: Proxy{
		"type": "trojan", "server": "1.2.3.4", "port": 443, "password": "tpw", "sni": "t.example.com"}}
	plain := Inbound{ID: 1, Protocol: ProtocolVLESS, Port: 443,
		Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"}, Users: chainUser}
	chained := Inbound{ID: 2, Protocol: ProtocolVLESS, Port: 444, Security: Security{Type: SecurityNone},
		Settings: Settings{UUID: "00000000-0000-4000-8000-000000000001"}, Users: chainUser, Via: &via}
	hy2 := Inbound{ID: 3, Protocol: ProtocolHysteria2, Port: 8443, Settings: Settings{Password: "hp"},
		Security: Security{Type: SecurityTLS, SNI: "h.example.com", CertPEM: "c", KeyPEM: "k"}, Users: chainUser}

	for _, kt := range []string{TypeXray, TypeSingbox} {
		ins := []Inbound{plain, chained}
		if kt == TypeSingbox {
			ins = append(ins, hy2)
		}
		r, err := RenderWith(kt, ins, RenderOptions{NoUserMarks: true})
		if err != nil {
			t.Fatalf("%s: %v", kt, err)
		}
		cfg := r.Files[0].Content
		if len(r.UserMarks) != 0 || strings.Contains(cfg, "routing_mark") || strings.Contains(cfg, `"mark"`) ||
			strings.Contains(cfg, "flyx-u7") || strings.Contains(cfg, "-u7\"") {
			t.Fatalf("%s must not mark users (%v):\n%s", kt, r.UserMarks, cfg)
		}
		// The user's own credentials still work.
		if !strings.Contains(cfg, "11111111-1111-4111-8111-111111111111") {
			t.Fatalf("%s dropped the user's credentials:\n%s", kt, cfg)
		}
		marked, err := RenderWith(kt, ins, RenderOptions{})
		if err != nil || len(marked.UserMarks) != 1 || marked.Hash == r.Hash {
			t.Fatalf("%s: marks must still apply by default (%v %v)", kt, marked.UserMarks, err)
		}
	}
}
