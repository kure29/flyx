package handler

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"

	"go-backend/internal/config"
	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
)

// Backups uploaded to a private GitHub Gist. Every upload is a new
// revision of the same gist, so the gist's history keeps the older ones.
// With a password the file is encrypted first (scrypt + AES-256-GCM);
// without, it is the plain backup JSON, readable by anyone with the link.

const (
	gistConfigToken    = "gist_token"
	gistConfigPassword = "gist_password"
	gistConfigID       = "gist_id"
	gistConfigInterval = "gist_interval_hours"
	// gistConfigLastAt is the last upload attempt (unix ms), gistConfigLastUpload
	// the last one that made a revision; gistConfigLastHash identifies its
	// content, so an unchanged backup is skipped.
	gistConfigLastAt     = "gist_last_at"
	gistConfigLastUpload = "gist_last_upload"
	gistConfigLastError  = "gist_last_error"
	gistConfigLastHash   = "gist_last_hash"

	gistFileName       = "flyx-backup.json"
	gistMaxBackupBytes = 20 << 20
	notifyEventBackup  = "backup_failed"
)

// gistIntervals are the automatic upload choices (hours; 0 is off).
var gistIntervals = map[int]bool{0: true, 6: true, 12: true, 24: true, 168: true}

// gistBackupTypes are all the backup types, restored together.
var gistBackupTypes = []string{"users", "nodes", "tunnels", "forwards", "userTunnels", "speedLimits",
	"tunnelGroups", "userGroups", "permissions", "configs", "inbounds"}

var (
	gistCheckInterval = 5 * time.Minute
	gistMu            sync.Mutex
)

// gistAPIBase is GitHub's API; FLYX_GIST_API_BASE points tests elsewhere.
func gistAPIBase() string {
	if v := strings.TrimRight(strings.TrimSpace(config.IdentityEnv("FLYX_GIST_API_BASE", "FLVX_GIST_API_BASE")), "/"); v != "" {
		return v
	}
	return "https://api.github.com"
}

func (h *Handler) gistConfig() map[string]string {
	cfg, err := h.repo.ListConfigs()
	if err != nil || cfg == nil {
		return map[string]string{}
	}
	return cfg
}

func gistInt64(cfg map[string]string, key string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(cfg[key]), 10, 64)
	return v
}

// --- encryption ---

type gistEnvelope struct {
	Format string `json:"flyx"`
	V      int    `json:"v"`
	KDF    string `json:"kdf"`
	N      int    `json:"n"`
	R      int    `json:"r"`
	P      int    `json:"p"`
	Salt   string `json:"salt"`
	Nonce  string `json:"nonce"`
	Data   string `json:"data"`
}

const gistEnvelopeFormat = "encrypted-backup"

func gistKey(password string, salt []byte, n, r, p int) ([]byte, error) {
	return scrypt.Key([]byte(password), salt, n, r, p, 32)
}

func gistEncrypt(plain []byte, password string) ([]byte, error) {
	env := gistEnvelope{Format: gistEnvelopeFormat, V: 1, KDF: "scrypt", N: 1 << 15, R: 8, P: 1}
	salt, nonce := make([]byte, 16), make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key, err := gistKey(password, salt, env.N, env.R, env.P)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	env.Salt = base64.StdEncoding.EncodeToString(salt)
	env.Nonce = base64.StdEncoding.EncodeToString(nonce)
	env.Data = base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, plain, nil))
	return json.MarshalIndent(env, "", "  ")
}

// gistOpen returns the backup JSON of a gist file: decrypted when it is
// an encrypted envelope (which needs the password).
func gistOpen(content []byte, password string) ([]byte, bool, error) {
	var env gistEnvelope
	if json.Unmarshal(content, &env) != nil || env.Format != gistEnvelopeFormat {
		return content, false, nil
	}
	if password == "" {
		return nil, true, errText("这个版本是加密的，请输入上传时用的密码")
	}
	if env.KDF != "scrypt" || env.N <= 1 || env.N > 1<<20 || env.R <= 0 || env.R > 32 || env.P <= 0 || env.P > 16 {
		return nil, true, errText("不认识的加密格式")
	}
	salt, err1 := base64.StdEncoding.DecodeString(env.Salt)
	nonce, err2 := base64.StdEncoding.DecodeString(env.Nonce)
	data, err3 := base64.StdEncoding.DecodeString(env.Data)
	if errors.Join(err1, err2, err3) != nil || len(nonce) != 12 {
		return nil, true, errText("加密数据已损坏")
	}
	key, err := gistKey(password, salt, env.N, env.R, env.P)
	if err != nil {
		return nil, true, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, true, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, true, err
	}
	plain, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, true, errText("密码不对，或者数据已损坏")
	}
	return plain, true, nil
}

// --- GitHub API ---

type gistFile struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	RawURL    string `json:"raw_url"`
	Size      int64  `json:"size"`
}

type gistRevision struct {
	Version     string `json:"version"`
	CommittedAt string `json:"committed_at"`
}

type gistDoc struct {
	ID      string              `json:"id"`
	HTMLURL string              `json:"html_url"`
	Files   map[string]gistFile `json:"files"`
	History []gistRevision      `json:"history"`
}

var errGistNotFound = errors.New("gist not found")

func gistRequest(ctx context.Context, method, url, token string, body interface{}, out interface{}) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "flyx-panel")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("连接 GitHub 失败：%v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, gistMaxBackupBytes*2))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errText("GitHub 令牌无效或已过期")
	case resp.StatusCode == http.StatusForbidden:
		return errText("GitHub 拒绝了请求：令牌需要 gist 权限，或者请求太频繁")
	case resp.StatusCode == http.StatusNotFound:
		return errGistNotFound
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		return fmt.Errorf("GitHub 返回 %d：%s", resp.StatusCode, truncate(e.Message, 200))
	}
	if out == nil {
		return nil
	}
	if raw, ok := out.(*[]byte); ok {
		*raw = data
		return nil
	}
	return json.Unmarshal(data, out)
}

// gistContent is a gist file's content; large files come truncated and are
// read from their raw URL.
func gistContent(ctx context.Context, token string, f gistFile) ([]byte, error) {
	if !f.Truncated {
		return []byte(f.Content), nil
	}
	var raw []byte
	if err := gistRequest(ctx, http.MethodGet, f.RawURL, token, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// --- upload ---

type gistUploadResult struct {
	Uploaded bool   `json:"uploaded"`
	Message  string `json:"message"`
}

// gistBackupContent is the file to upload and the hash that tells whether
// it changed: the backup without its export time, and the password.
func (h *Handler) gistBackupContent(password string) ([]byte, string, error) {
	backup, err := h.repo.ExportAll()
	if err != nil {
		return nil, "", fmt.Errorf("导出备份失败：%v", err)
	}
	exportedAt := backup.ExportedAt
	backup.ExportedAt = 0
	stable, err := json.Marshal(backup)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(append(append(stable, 0), []byte(password)...))
	backup.ExportedAt = exportedAt
	plain, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return nil, "", err
	}
	if len(plain) > gistMaxBackupBytes {
		return nil, "", errText("备份超过 20MB，无法上传到 Gist")
	}
	content := plain
	if password != "" {
		if content, err = gistEncrypt(plain, password); err != nil {
			return nil, "", err
		}
	}
	return content, hex.EncodeToString(sum[:]), nil
}

// gistUpload uploads the backup as a new revision (creating the gist the
// first time, or again when it was deleted). An unchanged backup is
// skipped unless force.
func (h *Handler) gistUpload(ctx context.Context, force bool) (gistUploadResult, error) {
	gistMu.Lock()
	defer gistMu.Unlock()
	cfg := h.gistConfig()
	token := strings.TrimSpace(cfg[gistConfigToken])
	if token == "" {
		return gistUploadResult{}, errText("请先填写 GitHub 令牌")
	}
	now := time.Now().UnixMilli()
	result, err := h.gistUploadLocked(ctx, cfg, token, force, now)
	fields := map[string]string{gistConfigLastAt: strconv.FormatInt(now, 10), gistConfigLastError: ""}
	if err != nil {
		fields[gistConfigLastError] = truncate(err.Error(), 255)
	}
	_ = h.repo.UpsertConfigs(fields, now)
	return result, err
}

func (h *Handler) gistUploadLocked(ctx context.Context, cfg map[string]string, token string, force bool, now int64) (gistUploadResult, error) {
	password := cfg[gistConfigPassword]
	content, hash, err := h.gistBackupContent(password)
	if err != nil {
		return gistUploadResult{}, err
	}
	id := strings.TrimSpace(cfg[gistConfigID])
	if !force && id != "" && hash == cfg[gistConfigLastHash] {
		return gistUploadResult{Message: "内容没有变化，没有产生新版本"}, nil
	}
	files := map[string]interface{}{gistFileName: map[string]string{"content": string(content)}}
	var doc gistDoc
	if id != "" {
		err = gistRequest(ctx, http.MethodPatch, gistAPIBase()+"/gists/"+id, token, map[string]interface{}{"files": files}, &doc)
		if errors.Is(err, errGistNotFound) {
			id = "" // deleted on GitHub: start a new one
		} else if err != nil {
			return gistUploadResult{}, err
		}
	}
	if id == "" {
		body := map[string]interface{}{"description": "FLYX 面板备份", "public": false, "files": files}
		if err := gistRequest(ctx, http.MethodPost, gistAPIBase()+"/gists", token, body, &doc); err != nil {
			if errors.Is(err, errGistNotFound) {
				return gistUploadResult{}, errText("GitHub 找不到接口，请检查令牌权限")
			}
			return gistUploadResult{}, err
		}
		if doc.ID == "" {
			return gistUploadResult{}, errText("GitHub 没有返回 Gist 编号")
		}
	}
	if err := h.repo.UpsertConfigs(map[string]string{
		gistConfigID: doc.ID, gistConfigLastHash: hash, gistConfigLastUpload: strconv.FormatInt(now, 10),
	}, now); err != nil {
		return gistUploadResult{}, err
	}
	return gistUploadResult{Uploaded: true, Message: "已上传"}, nil
}

// gistDue reports whether an automatic upload is due.
func gistDue(cfg map[string]string, now time.Time) bool {
	hours := gistInt64(cfg, gistConfigInterval)
	if hours <= 0 || strings.TrimSpace(cfg[gistConfigToken]) == "" {
		return false
	}
	return now.UnixMilli()-gistInt64(cfg, gistConfigLastAt) >= hours*int64(time.Hour/time.Millisecond)
}

func (h *Handler) runGistBackupLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	ticker := time.NewTicker(gistCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !gistDue(h.gistConfig(), time.Now()) {
			continue
		}
		if _, err := h.gistUpload(ctx, false); err != nil {
			log.Printf("gist backup failed: %v", err)
			h.notify(notifyEventBackup, "Gist 备份失败", err.Error())
		}
	}
}

// --- API ---

type gistStatusView struct {
	Configured    bool   `json:"configured"`
	TokenHint     string `json:"tokenHint"`
	Encrypted     bool   `json:"encrypted"`
	IntervalHours int64  `json:"intervalHours"`
	GistURL       string `json:"gistUrl"`
	LastAt        int64  `json:"lastAt"`
	LastUpload    int64  `json:"lastUpload"`
	LastError     string `json:"lastError"`
}

// tokenHint shows a token's start and end only.
func tokenHint(t string) string {
	if len(t) <= 10 {
		return strings.Repeat("•", len(t))
	}
	return t[:4] + "••••••••" + t[len(t)-4:]
}

func gistStatus(cfg map[string]string) gistStatusView {
	token := strings.TrimSpace(cfg[gistConfigToken])
	v := gistStatusView{
		Configured: token != "", TokenHint: tokenHint(token), Encrypted: cfg[gistConfigPassword] != "",
		IntervalHours: gistInt64(cfg, gistConfigInterval), LastAt: gistInt64(cfg, gistConfigLastAt),
		LastUpload: gistInt64(cfg, gistConfigLastUpload), LastError: cfg[gistConfigLastError],
	}
	if id := strings.TrimSpace(cfg[gistConfigID]); id != "" {
		v.GistURL = "https://gist.github.com/" + id
	}
	return v
}

func (h *Handler) gistBackupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	response.WriteJSON(w, response.OK(gistStatus(h.gistConfig())))
}

// gistBackupSave stores the settings. An empty token or password keeps
// the saved one; clearToken / clearPassword remove it (no token turns
// the Gist backup off).
func (h *Handler) gistBackupSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		Token         string `json:"token"`
		ClearToken    bool   `json:"clearToken"`
		Password      string `json:"password"`
		ClearPassword bool   `json:"clearPassword"`
		IntervalHours int    `json:"intervalHours"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if !gistIntervals[req.IntervalHours] {
		response.WriteJSON(w, response.ErrDefault("自动上传间隔不正确"))
		return
	}
	if len(req.Token) > 255 || strings.ContainsAny(req.Token, " \r\n\t") || len(req.Password) > 255 {
		response.WriteJSON(w, response.ErrDefault("令牌或密码格式不正确"))
		return
	}
	if req.Token != "" {
		// Check the token before keeping it.
		if err := gistRequest(r.Context(), http.MethodGet, gistAPIBase()+"/gists?per_page=1", req.Token, nil, nil); err != nil {
			response.WriteJSON(w, response.ErrDefault("令牌不可用："+err.Error()))
			return
		}
	}
	fields := map[string]string{gistConfigInterval: strconv.Itoa(req.IntervalHours)}
	switch {
	case req.ClearToken:
		fields[gistConfigToken] = ""
	case req.Token != "":
		fields[gistConfigToken] = req.Token
	}
	switch {
	case req.ClearPassword:
		fields[gistConfigPassword] = ""
	case req.Password != "":
		fields[gistConfigPassword] = req.Password
	}
	now := time.Now().UnixMilli()
	if err := h.repo.UpsertConfigs(fields, now); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(gistStatus(h.gistConfig())))
}

func (h *Handler) gistBackupUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	result, err := h.gistUpload(r.Context(), false)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("上传失败："+err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"result": result, "status": gistStatus(h.gistConfig())}))
}

type gistVersionView struct {
	Version     string `json:"version"`
	CommittedAt int64  `json:"committedAt"`
}

func (h *Handler) gistDoc(ctx context.Context, version string) (gistDoc, string, error) {
	cfg := h.gistConfig()
	token, id := strings.TrimSpace(cfg[gistConfigToken]), strings.TrimSpace(cfg[gistConfigID])
	if token == "" || id == "" {
		return gistDoc{}, "", errText("还没有上传过 Gist 备份")
	}
	url := gistAPIBase() + "/gists/" + id
	if version != "" {
		url += "/" + version
	}
	var doc gistDoc
	if err := gistRequest(ctx, http.MethodGet, url, token, nil, &doc); err != nil {
		if errors.Is(err, errGistNotFound) {
			return gistDoc{}, "", errText("Gist 或这个版本已经不存在")
		}
		return gistDoc{}, "", err
	}
	return doc, token, nil
}

// gistBackupVersions lists the gist's revisions, newest first (at most 10).
func (h *Handler) gistBackupVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	doc, _, err := h.gistDoc(r.Context(), "")
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	out := []gistVersionView{}
	for _, rev := range doc.History {
		if len(out) == 10 {
			break
		}
		at, _ := time.Parse(time.RFC3339, rev.CommittedAt)
		out = append(out, gistVersionView{Version: rev.Version, CommittedAt: at.UnixMilli()})
	}
	response.WriteJSON(w, response.OK(out))
}

// gistBackupRestore restores every backup type from a revision, after an
// automatic backup of the current data (as importing a file does).
func (h *Handler) gistBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		Version  string `json:"version"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	req.Version = strings.TrimSpace(req.Version)
	if req.Version == "" || len(req.Version) > 64 || strings.ContainsAny(req.Version, "/?#") {
		response.WriteJSON(w, response.ErrDefault("请选择版本"))
		return
	}
	doc, token, err := h.gistDoc(r.Context(), req.Version)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	file, ok := doc.Files[gistFileName]
	if !ok {
		response.WriteJSON(w, response.ErrDefault("这个版本里没有 FLYX 备份文件"))
		return
	}
	content, err := gistContent(r.Context(), token, file)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("下载备份失败："+err.Error()))
		return
	}
	plain, _, err := gistOpen(content, req.Password)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	var backup model.BackupData
	if err := json.Unmarshal(plain, &backup); err != nil || backup.Version == "" {
		response.WriteJSON(w, response.ErrDefault("备份数据格式错误"))
		return
	}
	autoBackup, err := h.repo.ExportAll()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, fmt.Sprintf("恢复前自动备份失败: %v", err)))
		return
	}
	result, err := h.repo.Import(&backup, gistBackupTypes)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, fmt.Sprintf("恢复失败: %v", err)))
		return
	}
	h.reconcileAllKernelsAfterImport(result, &backup)
	result.AutoBackup = autoBackup
	response.WriteJSON(w, response.OK(result))
}
