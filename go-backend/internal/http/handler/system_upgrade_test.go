package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go-backend/internal/store/repo"
)

func TestSelectComposeAssetUsesIPv6Template(t *testing.T) {
	exec := &systemUpgradeExecutor{deployDir: "/opt/flvx-panel", backendContainer: "flux-panel-backend"}
	compose := []byte("networks:\n  gost-network:\n    enable_ipv6: true\n")

	if got := exec.selectComposeAsset(compose); got != "docker-compose-v6.yml" {
		t.Fatalf("selectComposeAsset() = %q, want %q", got, "docker-compose-v6.yml")
	}
}

func TestDownloadReleaseAssetUsesGithubProxyWhenEnabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	repoStore, err := repo.Open(dbPath)
	if err != nil {
		t.Fatalf("repo.Open() error = %v", err)
	}
	defer repoStore.Close()

	h := &Handler{repo: repoStore}
	originalBase := systemUpgradeReleaseBaseURL
	systemUpgradeReleaseBaseURL = "https://example.invalid"
	t.Cleanup(func() { systemUpgradeReleaseBaseURL = originalBase })

	originalGet := systemUpgradeHTTPGet
	defer func() { systemUpgradeHTTPGet = originalGet }()

	var gotURL string
	systemUpgradeHTTPGet = func(client *http.Client, url string) (*http.Response, error) {
		gotURL = url
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("services:\n  backend:\n    image: test\n")),
		}, nil
	}

	now := time.Now().UnixMilli()
	if err := repoStore.UpsertConfig("github_proxy_enabled", "true", now); err != nil {
		t.Fatalf("UpsertConfig() github_proxy_enabled error = %v", err)
	}
	if err := repoStore.UpsertConfig("github_proxy_url", "https://proxy.example.com", now); err != nil {
		t.Fatalf("UpsertConfig() github_proxy_url error = %v", err)
	}

	data, err := h.downloadReleaseAsset("2.1.9", "docker-compose-v4.yml")
	if err != nil {
		t.Fatalf("downloadReleaseAsset() error = %v", err)
	}
	if !strings.Contains(string(data), "backend") {
		t.Fatalf("downloadReleaseAsset() data = %q, want compose data", string(data))
	}

	wantURL := "https://proxy.example.com/https://example.invalid/kure29/flyx/releases/download/2.1.9/docker-compose-v4.yml"
	if gotURL != wantURL {
		t.Fatalf("download URL = %q, want %q", gotURL, wantURL)
	}
}

func TestDownloadReleaseAssetRejectsOversizedBody(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	repoStore, err := repo.Open(dbPath)
	if err != nil {
		t.Fatalf("repo.Open() error = %v", err)
	}
	defer repoStore.Close()
	now := time.Now().UnixMilli()
	if err := repoStore.UpsertConfig("github_proxy_enabled", "false", now); err != nil {
		t.Fatalf("UpsertConfig() github_proxy_enabled error = %v", err)
	}

	originalBase := systemUpgradeReleaseBaseURL
	systemUpgradeReleaseBaseURL = "https://example.invalid"
	t.Cleanup(func() { systemUpgradeReleaseBaseURL = originalBase })

	originalGet := systemUpgradeHTTPGet
	defer func() { systemUpgradeHTTPGet = originalGet }()
	systemUpgradeHTTPGet = func(client *http.Client, url string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("a"), maxSystemUpgradeComposeAssetBytes+1))),
		}, nil
	}

	h := &Handler{repo: repoStore}
	_, err = h.downloadReleaseAsset("2.1.9", "docker-compose-v4.yml")
	if err == nil || !strings.Contains(err.Error(), "过大") {
		t.Fatalf("downloadReleaseAsset() error = %v, want oversized error", err)
	}
}

func TestSelectComposeAssetUsesIPv6TemplateForYAMLVariants(t *testing.T) {
	exec := &systemUpgradeExecutor{deployDir: "/opt/flvx-panel", backendContainer: "flux-panel-backend"}
	for _, compose := range [][]byte{
		[]byte("networks:\n  gost-network:\n    enable_ipv6:true\n"),
		[]byte("networks:\n  gost-network:\n    enable_ipv6: True\n"),
		[]byte("networks:\n  gost-network:\n    enable_ipv6: \"true\"\n"),
		[]byte("networks:\n  gost-network:\n    enable_ipv6: 'true'\n"),
		[]byte("networks:\n  gost-network:\n    enable_ipv6: true # comment\n"),
	} {
		if got := exec.selectComposeAsset(compose); got != "docker-compose-v6.yml" {
			t.Fatalf("selectComposeAsset(%q) = %q, want %q", string(compose), got, "docker-compose-v6.yml")
		}
	}
}

func TestSelectComposeAssetFallsBackToIPv4Template(t *testing.T) {
	exec := &systemUpgradeExecutor{deployDir: "/opt/flvx-panel", backendContainer: "flux-panel-backend"}
	compose := []byte("services:\n  backend:\n    image: test\n")

	if got := exec.selectComposeAsset(compose); got != "docker-compose-v4.yml" {
		t.Fatalf("selectComposeAsset() = %q, want %q", got, "docker-compose-v4.yml")
	}
}

func TestUpdateEnvVersionReplacesExistingValue(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("FLUX_VERSION=2.1.8\nJWT_SECRET=test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	exec := &systemUpgradeExecutor{deployDir: dir, backendContainer: "flux-panel-backend"}
	if err := exec.updateEnvVersion(envPath, "2.1.9"); err != nil {
		t.Fatalf("updateEnvVersion() error = %v", err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	want := "FLUX_VERSION=2.1.9\nJWT_SECRET=test\nFLYX_VERSION=2.1.9\n"
	if string(data) != want {
		t.Fatalf("env content = %q, want %q", string(data), want)
	}
}

func TestUpdateEnvVersionAppendsMissingValue(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("JWT_SECRET=test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	exec := &systemUpgradeExecutor{deployDir: dir, backendContainer: "flux-panel-backend"}
	if err := exec.updateEnvVersion(envPath, "2.1.9"); err != nil {
		t.Fatalf("updateEnvVersion() error = %v", err)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	want := "JWT_SECRET=test\nFLYX_VERSION=2.1.9\nFLUX_VERSION=2.1.9\n"
	if string(data) != want {
		t.Fatalf("env content = %q, want %q", string(data), want)
	}
}

func TestUpdateEnvVersionRejectsUnsafeValue(t *testing.T) {
	for _, version := range []string{"", "2.1.9\nJWT_SECRET=bad", "2.1.9\rbad", "2.1.9\x00bad", "2.1.9\x1fbad"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			envPath := filepath.Join(dir, ".env")
			original := []byte("JWT_SECRET=test\n")
			if err := os.WriteFile(envPath, original, 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			exec := &systemUpgradeExecutor{deployDir: dir, backendContainer: "flux-panel-backend"}
			if err := exec.updateEnvVersion(envPath, version); err == nil {
				t.Fatal("expected unsafe version to fail validation")
			}

			data, err := os.ReadFile(envPath)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(data) != string(original) {
				t.Fatalf("env content changed to %q, want %q", string(data), string(original))
			}
		})
	}
}

func TestUpdateEnvVersionAcceptsVersionLabels(t *testing.T) {
	for _, version := range []string{"2.1.9", "2.1.9-beta14", "v-test"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			envPath := filepath.Join(dir, ".env")
			if err := os.WriteFile(envPath, []byte("JWT_SECRET=test\n"), 0o644); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			exec := &systemUpgradeExecutor{deployDir: dir, backendContainer: "flux-panel-backend"}
			if err := exec.updateEnvVersion(envPath, version); err != nil {
				t.Fatalf("updateEnvVersion() error = %v", err)
			}
		})
	}
}

func TestUpdateEnvVersionPreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("FLUX_VERSION=2.1.8\nJWT_SECRET=test\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	exec := &systemUpgradeExecutor{deployDir: dir, backendContainer: "flux-panel-backend"}
	if err := exec.updateEnvVersion(envPath, "2.1.9"); err != nil {
		t.Fatalf("updateEnvVersion() error = %v", err)
	}

	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("env mode = %o, want 0600", got)
	}
}

func TestValidateBackendContainerNameRejectsUnsafeValue(t *testing.T) {
	if err := validateBackendContainerName("flux-panel-backend;rm -rf /"); err == nil {
		t.Fatal("expected unsafe container name to fail validation")
	}
}

func TestBuildHelperRunArgsUsesDetachedContainer(t *testing.T) {
	exec := &systemUpgradeExecutor{deployDir: "/opt/flvx-panel", backendContainer: "flux-panel-backend"}
	args, err := exec.buildHelperRunArgs("sha256:abc", "flvx-upgrade-helper")
	if err != nil {
		t.Fatalf("buildHelperRunArgs() error = %v", err)
	}
	want := []string{
		"run", "-d", "--rm", "--name", "flvx-upgrade-helper",
		"--volumes-from", "flux-panel-backend",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-e", "PANEL_DEPLOY_DIR=/opt/flvx-panel",
		"--entrypoint", "/bin/sh", "sha256:abc",
		"-c", exec.helperScript(),
	}

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("buildHelperRunArgs() = %#v, want %#v", args, want)
	}
}

func TestBuildHelperRunArgsRejectsUnsafeBackendContainer(t *testing.T) {
	exec := &systemUpgradeExecutor{deployDir: "/opt/flvx-panel", backendContainer: "flux-panel-backend;rm -rf /"}
	if _, err := exec.buildHelperRunArgs("sha256:abc", "flvx-upgrade-helper"); err == nil {
		t.Fatal("expected unsafe backend container name to fail validation")
	}
}

func TestSystemVersionRejectsWrongMethod(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/version", nil)
	rr := httptest.NewRecorder()

	h.systemVersion(rr, req)

	if !strings.Contains(rr.Body.String(), "请求失败") {
		t.Fatalf("expected wrong-method response, got %s", rr.Body.String())
	}
}

func TestSystemUpgradeRejectsConcurrentRequests(t *testing.T) {
	h := &Handler{}
	h.systemUpgradeMu.Lock()
	defer h.systemUpgradeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/upgrade", strings.NewReader(`{"channel":"stable"}`))
	rr := httptest.NewRecorder()

	h.systemUpgrade(rr, req)

	if !strings.Contains(rr.Body.String(), systemUpgradeConflictError) {
		t.Fatalf("expected conflict message, got %s", rr.Body.String())
	}
}

func TestSystemUpgradeFailsFastBeforeMutatingFiles(t *testing.T) {
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(composePath, []byte("services:\n  backend:\n    image: test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() compose error = %v", err)
	}
	if err := os.WriteFile(envPath, []byte("FLUX_VERSION=2.1.8\nJWT_SECRET=test\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() env error = %v", err)
	}

	fakeDockerDir := t.TempDir()
	fakeDockerPath := filepath.Join(fakeDockerDir, "docker")
	fakeDockerScript := "#!/bin/sh\ncase \"$1\" in\n  --version)\n    echo 'Docker version 27.0.0'\n    exit 0\n    ;;&\n  compose)\n    if [ \"$2\" = version ]; then\n      echo 'Docker Compose version v2.33.0'\n      exit 0\n    fi\n    exit 0\n    ;;&\n  inspect)\n    echo 'No such object: flux-panel-backend' >&2\n    exit 1\n    ;;&\n  *)\n    exit 0\n    ;;&\n esac\n"
	if err := os.WriteFile(fakeDockerPath, []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatalf("WriteFile() fake docker error = %v", err)
	}
	t.Setenv("PATH", fakeDockerDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(panelDeployDirEnv, dir)
	t.Setenv(panelBackendContainerEnv, "flux-panel-backend")

	h := &Handler{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/upgrade", strings.NewReader(`{"channel":"stable"}`))
	rr := httptest.NewRecorder()

	h.systemUpgrade(rr, req)

	if !strings.Contains(rr.Body.String(), "当前环境不支持面板自升级") {
		t.Fatalf("expected fail-fast capability error, got %s", rr.Body.String())
	}
	if _, err := os.Stat(composePath + ".upgrade.bak"); !os.IsNotExist(err) {
		t.Fatalf("expected no compose backup, got err=%v", err)
	}
	if _, err := os.Stat(envPath + ".upgrade.bak"); !os.IsNotExist(err) {
		t.Fatalf("expected no env backup, got err=%v", err)
	}
	composeData, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("ReadFile() compose error = %v", err)
	}
	if string(composeData) != "services:\n  backend:\n    image: test\n" {
		t.Fatalf("compose mutated unexpectedly: %q", string(composeData))
	}
	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("ReadFile() env error = %v", err)
	}
	if string(envData) != "FLUX_VERSION=2.1.8\nJWT_SECRET=test\n" {
		t.Fatalf("env mutated unexpectedly: %q", string(envData))
	}
}

func TestUpgradeBackupUsesStablePathAndRestoreRestoresOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	exec := &systemUpgradeExecutor{deployDir: dir, backendContainer: "flux-panel-backend"}
	backupPath, err := exec.backupFile(path)
	if err != nil {
		t.Fatalf("backupFile() error = %v", err)
	}
	if backupPath != path+".upgrade.bak" {
		t.Fatalf("backup path = %q, want %q", backupPath, path+".upgrade.bak")
	}
	if err := os.WriteFile(path, []byte("mutated"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := exec.restoreBackup(path); err != nil {
		t.Fatalf("restoreBackup() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "original" {
		t.Fatalf("restored content = %q, want original", string(data))
	}
}

func TestRestoreBackupPreservesOriginalFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("FLUX_VERSION=2.1.8\nJWT_SECRET=test\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	exec := &systemUpgradeExecutor{deployDir: dir, backendContainer: "flux-panel-backend"}
	if _, err := exec.backupFile(path); err != nil {
		t.Fatalf("backupFile() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := exec.restoreBackup(path); err != nil {
		t.Fatalf("restoreBackup() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("restored mode = %o, want 0600", got)
	}
}

func TestDecodeSystemUpgradeRequestRejectsTruncatedJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/check-updates", strings.NewReader(`{"channel":"stable"`))
	var payload systemUpgradeRequest

	if err := decodeSystemUpgradeRequest(req, &payload); err == nil {
		t.Fatal("expected truncated JSON to be rejected")
	}
}

func TestDecodeSystemUpgradeRequestAllowsEmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/check-updates", strings.NewReader(""))
	var payload systemUpgradeRequest

	if err := decodeSystemUpgradeRequest(req, &payload); err != nil {
		t.Fatalf("expected empty body to be accepted, got %v", err)
	}
}

func TestSystemUpgradeVersionDataSurfacesLookupFailureReason(t *testing.T) {
	data, err := json.Marshal(systemUpgradeVersionData{Reason: "GitHub unavailable"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"reason":"GitHub unavailable"`) {
		t.Fatalf("expected reason field in JSON, got %s", string(data))
	}
}

func TestHelperRunArgsCarryComposeProject(t *testing.T) {
	exec := &systemUpgradeExecutor{deployDir: "/opt/flvx-panel", backendContainer: "flux-panel-backend", composeProject: "root"}
	args, err := exec.buildHelperRunArgs("sha256:abc", "helper")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-e COMPOSE_PROJECT_NAME=root") {
		t.Fatalf("helper must reuse the running project: %v", args)
	}
	exec.composeProject = "bad name;rm"
	args, _ = exec.buildHelperRunArgs("sha256:abc", "helper")
	if strings.Contains(strings.Join(args, " "), "-e COMPOSE_PROJECT_NAME=") {
		t.Fatalf("invalid project names must be dropped: %v", args)
	}
}

func TestHelperRunsInHostDeployDir(t *testing.T) {
	exec := &systemUpgradeExecutor{deployDir: "/opt/flvx-panel", backendContainer: "flux-panel-backend", hostDeployDir: "/root"}
	args, err := exec.buildHelperRunArgs("sha256:abc", "helper")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	// Compose resolves ./ against the directory it runs in as a host path,
	// so the helper must work in the host directory itself.
	if !strings.Contains(joined, "-v /root:/root") || !strings.Contains(joined, "-e PANEL_DEPLOY_DIR=/root") ||
		strings.Contains(joined, "PANEL_DEPLOY_DIR=/opt/flvx-panel") {
		t.Fatalf("helper must run in the host deploy dir: %v", args)
	}
	exec.hostDeployDir = "/bad dir;x"
	joined = strings.Join(mustArgs(t, exec), " ")
	if !strings.Contains(joined, "-e PANEL_DEPLOY_DIR=/opt/flvx-panel") || strings.Contains(joined, "bad dir") {
		t.Fatalf("unsafe host dirs must be ignored: %v", joined)
	}
}

func mustArgs(t *testing.T, e *systemUpgradeExecutor) []string {
	t.Helper()
	args, err := e.buildHelperRunArgs("sha256:abc", "helper")
	if err != nil {
		t.Fatal(err)
	}
	return args
}
