package contract_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-backend/internal/auth"
)

// fakeGist is a GitHub Gist API holding one gist and its revisions.
type fakeGist struct {
	mu        sync.Mutex
	revisions []string // file contents, oldest first
	created   int
	patched   int
}

func (f *fakeGist) serve(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		doc := func(rev int) map[string]interface{} {
			history := []map[string]interface{}{}
			for i := len(f.revisions) - 1; i >= 0; i-- {
				history = append(history, map[string]interface{}{
					"version": fmt.Sprintf("v%d", i+1), "committed_at": time.Date(2026, 10, 24, i, 0, 0, 0, time.UTC).Format(time.RFC3339)})
			}
			files := map[string]interface{}{}
			if rev > 0 {
				files["flyx-backup.json"] = map[string]interface{}{"content": f.revisions[rev-1], "truncated": false}
			}
			return map[string]interface{}{"id": "g1", "html_url": "https://gist.github.com/g1", "files": files, "history": history}
		}
		var body struct {
			Public bool `json:"public"`
			Files  map[string]struct {
				Content string `json:"content"`
			} `json:"files"`
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/gists":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/gists":
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Public {
				t.Error("the backup gist must be private")
			}
			f.created++
			f.revisions = append(f.revisions, body.Files["flyx-backup.json"].Content)
			_ = json.NewEncoder(w).Encode(doc(len(f.revisions)))
		case r.Method == http.MethodPatch && r.URL.Path == "/gists/g1":
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.patched++
			f.revisions = append(f.revisions, body.Files["flyx-backup.json"].Content)
			_ = json.NewEncoder(w).Encode(doc(len(f.revisions)))
		case r.Method == http.MethodGet && r.URL.Path == "/gists/g1":
			_ = json.NewEncoder(w).Encode(doc(len(f.revisions)))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/gists/g1/v"):
			var n int
			_, _ = fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/gists/g1/v"), "%d", &n)
			if n < 1 || n > len(f.revisions) {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(doc(n))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func (f *fakeGist) revision(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revisions[i]
}

func TestGistBackupContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	post := func(path, body string) map[string]interface{} {
		t.Helper()
		out := kernelContractPost(t, router, admin, path, body)
		if out.Code != 0 {
			return map[string]interface{}{"error": out.Msg}
		}
		data, _ := out.Data.(map[string]interface{})
		if data == nil {
			data = map[string]interface{}{"ok": true}
		}
		return data
	}
	fake := &fakeGist{}
	srv := fake.serve(t)
	defer srv.Close()
	t.Setenv("FLVX_GIST_API_BASE", srv.URL)
	nodeID := seedKernelNode(t, r, "gistnode")

	// Nothing set up yet.
	if res := post("/api/v1/backup/gist/upload", `{}`); !strings.Contains(fmt.Sprint(res["error"]), "令牌") {
		t.Fatalf("upload without token: %v", res)
	}
	if res := post("/api/v1/backup/gist/save", `{"token":"bad-token","intervalHours":24}`); !strings.Contains(fmt.Sprint(res["error"]), "令牌不可用") {
		t.Fatalf("a token GitHub refuses must not be saved: %v", res)
	}
	if res := post("/api/v1/backup/gist/save", `{"token":"good-token","intervalHours":5}`); res["error"] == nil {
		t.Fatal("an interval outside the choices must be rejected")
	}

	// Without a password the backup is plain JSON.
	st := post("/api/v1/backup/gist/save", `{"token":"good-token","intervalHours":24}`)
	if st["configured"] != true || st["encrypted"] != false || valueAsInt(st["intervalHours"]) != 24 || strings.Contains(fmt.Sprint(st), "good-token") {
		t.Fatalf("status after save: %v", st)
	}
	up := post("/api/v1/backup/gist/upload", `{}`)
	if up["result"].(map[string]interface{})["uploaded"] != true || fake.created != 1 || !strings.Contains(fake.revision(0), "gistnode-secret") {
		t.Fatalf("first upload creates the gist: %v %d", up, fake.created)
	}
	st = up["status"].(map[string]interface{})
	if st["gistUrl"] != "https://gist.github.com/g1" || valueAsInt(st["lastUpload"]) == 0 || st["lastError"] != "" {
		t.Fatalf("status after upload: %v", st)
	}
	// Unchanged: no new revision.
	if up := post("/api/v1/backup/gist/upload", `{}`); up["result"].(map[string]interface{})["uploaded"] != false || fake.patched != 0 {
		t.Fatalf("unchanged backup must be skipped: %v", up)
	}

	// The token and password never come back, and stay out of backups.
	cfgs := kernelContractPost(t, router, admin, "/api/v1/config/list", `{}`).Data.(map[string]interface{})
	if _, ok := cfgs["gist_token"]; ok {
		t.Fatal("config list must not return the gist token")
	}
	if strings.Contains(fake.revision(0), "good-token") {
		t.Fatal("the backup must not contain the gist token")
	}
	if res := post("/api/v1/config/update-single", `{"name":"gist_token","value":"x"}`); res["error"] == nil {
		t.Fatal("gist settings are set through their own endpoint only")
	}

	// With a password the next revision is encrypted.
	st = post("/api/v1/backup/gist/save", `{"password":"hunter2","intervalHours":24}`)
	if st["encrypted"] != true || st["configured"] != true {
		t.Fatalf("password saved, token kept: %v", st)
	}
	post("/api/v1/backup/gist/upload", `{}`)
	if fake.patched != 1 || strings.Contains(fake.revision(1), "gistnode-secret") || !strings.Contains(fake.revision(1), "encrypted-backup") {
		t.Fatalf("encrypted revision: %d %.200s", fake.patched, fake.revision(1))
	}

	versions := kernelContractPost(t, router, admin, "/api/v1/backup/gist/versions", `{}`).Data.([]interface{})
	if len(versions) != 2 || versions[0].(map[string]interface{})["version"] != "v2" {
		t.Fatalf("versions, newest first: %v", versions)
	}

	// Restore brings the deleted node back; an encrypted version needs
	// the right password.
	if err := r.DB().Exec(`DELETE FROM node WHERE id = ?`, nodeID).Error; err != nil {
		t.Fatal(err)
	}
	if res := post("/api/v1/backup/gist/restore", `{"version":"v2","password":"nope"}`); !strings.Contains(fmt.Sprint(res["error"]), "密码不对") {
		t.Fatalf("wrong password: %v", res)
	}
	if res := post("/api/v1/backup/gist/restore", `{"version":"v2"}`); !strings.Contains(fmt.Sprint(res["error"]), "加密") {
		t.Fatalf("missing password: %v", res)
	}
	if res := post("/api/v1/backup/gist/restore", `{"version":"v2","password":"hunter2"}`); res["error"] != nil {
		t.Fatalf("restore: %v", res)
	}
	if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM node WHERE name = 'gistnode'`); n != 1 {
		t.Fatalf("node restored: %d", n)
	}
	// Restoring users signs the admin out, as importing a file does.
	time.Sleep(1100 * time.Millisecond)
	admin, _ = auth.GenerateToken(1, "admin_user", 0, secret)
	// The plain version restores without a password.
	if res := post("/api/v1/backup/gist/restore", `{"version":"v1"}`); res["error"] != nil {
		t.Fatalf("plain restore: %v", res)
	}
	time.Sleep(1100 * time.Millisecond)
	admin, _ = auth.GenerateToken(1, "admin_user", 0, secret)
	// Restoring keeps the current Gist settings.
	if st := post("/api/v1/backup/gist/status", `{}`); st["configured"] != true || st["encrypted"] != true {
		t.Fatalf("settings kept after restore: %v", st)
	}

	// Clearing the token turns it off.
	if st := post("/api/v1/backup/gist/save", `{"clearToken":true,"intervalHours":0}`); st["configured"] != false {
		t.Fatalf("cleared: %v", st)
	}
}
