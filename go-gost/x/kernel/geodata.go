package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GeoFile is a data file a kernel reads next to its binary (Xray's
// geosite.dat / geoip.dat, used by 分流). The kernel archive ships one; the
// agent keeps it current from URL, verified against ChecksumURL.
type GeoFile struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	ChecksumURL string `json:"checksumUrl"`
}

const (
	geoFirstCheck = 10 * time.Minute
	geoCheckEvery = 24 * time.Hour
	geoMaxSize    = 64 << 20
	geoTimeout    = 5 * time.Minute
)

func (g GeoFile) validate() error {
	if !safeName(g.Name) || !strings.HasSuffix(g.Name, ".dat") {
		return fmt.Errorf("非法数据文件名: %s", g.Name)
	}
	for _, u := range []string{g.URL, g.ChecksumURL} {
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return fmt.Errorf("非法数据文件地址: %s", u)
		}
	}
	return nil
}

// startGeoUpdater checks the kernels' data files shortly after start and
// then daily (with jitter so nodes do not all fetch at once).
func (m *Manager) startGeoUpdater() {
	go func() {
		wait := geoFirstCheck
		for {
			time.Sleep(wait)
			ctx, cancel := context.WithTimeout(context.Background(), geoTimeout)
			m.UpdateGeoData(ctx)
			cancel()
			wait = geoCheckEvery + time.Duration(rand.Int63n(int64(time.Hour)))
		}
	}()
}

type geoJob struct {
	kernelType string
	binDir     string
	files      []GeoFile
}

// UpdateGeoData refreshes the data files of every kernel that has some and
// restarts the kernels whose files changed. A kernel that fails to start on
// the new files gets the old ones back.
func (m *Manager) UpdateGeoData(ctx context.Context) {
	m.mu.Lock()
	var jobs []geoJob
	for t, inst := range m.instances {
		if len(inst.req.GeoData) > 0 && inst.binDir != "" {
			jobs = append(jobs, geoJob{t, inst.binDir, inst.req.GeoData})
		}
	}
	m.mu.Unlock()

	for _, job := range jobs {
		staged, err := stageGeoFiles(ctx, job.binDir, job.files)
		if err != nil {
			m.logGeo(job.kernelType, "分流数据更新失败: "+err.Error())
		}
		if len(staged) > 0 {
			m.swapGeoFiles(job, staged)
		}
	}
}

// stageGeoFiles downloads the files that differ from the installed ones to
// "<name>.new" and returns their names. Files are kept as they are when the
// download or the checksum fails.
func stageGeoFiles(ctx context.Context, binDir string, files []GeoFile) ([]string, error) {
	var staged []string
	var errs []string
	for _, f := range files {
		changed, err := stageGeoFile(ctx, binDir, f)
		if err != nil {
			errs = append(errs, f.Name+": "+err.Error())
			continue
		}
		if changed {
			staged = append(staged, f.Name)
		}
	}
	if len(errs) > 0 {
		return staged, errors.New(strings.Join(errs, "；"))
	}
	return staged, nil
}

func stageGeoFile(ctx context.Context, binDir string, f GeoFile) (bool, error) {
	var sumBody strings.Builder
	if err := fetch(ctx, f.ChecksumURL, 4096, &sumBody); err != nil {
		return false, err
	}
	want, ok := parseChecksum(sumBody.String())
	if !ok {
		return false, errors.New("校验文件中没有 SHA256")
	}
	path := filepath.Join(binDir, f.Name)
	if have, err := fileSHA256(path); err == nil && have == want {
		return false, nil
	}

	tmp := path + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	h := sha256.New()
	err = fetch(ctx, f.URL, geoMaxSize, io.MultiWriter(out, h))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != want {
		err = errors.New("SHA256 校验失败")
	}
	if err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// swapGeoFiles moves the staged files in and restarts the kernel if it is
// running; on a failed start the previous files are restored.
func (m *Manager) swapGeoFiles(job geoJob, staged []string) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.Lock()
	inst := m.instances[job.kernelType]
	current := inst != nil && inst.binDir == job.binDir
	m.mu.Unlock()
	if !current {
		// Reinstalled meanwhile: the new version brought its own files.
		for _, name := range staged {
			_ = os.Remove(filepath.Join(job.binDir, name+".new"))
		}
		return
	}

	var swapped []string
	for _, name := range staged {
		path := filepath.Join(job.binDir, name)
		_ = os.Remove(path + ".bak")
		if _, err := os.Stat(path); err == nil {
			if err := os.Rename(path, path+".bak"); err != nil {
				_ = os.Remove(path + ".new")
				continue
			}
		}
		if err := os.Rename(path+".new", path); err != nil {
			_ = os.Rename(path+".bak", path)
			continue
		}
		swapped = append(swapped, name)
	}
	if len(swapped) == 0 {
		return
	}

	m.mu.Lock()
	enabled := inst.req.Enabled
	m.mu.Unlock()
	if enabled {
		m.stopProcesses(inst)
		if err := m.syncNft(); err != nil {
			m.logGeo(job.kernelType, "分流数据已替换，但内核未启动: "+err.Error())
			return
		}
		m.startProcesses(inst)
		time.Sleep(startupProbeIn)
		if !m.state(job.kernelType, false).Running {
			for _, name := range swapped {
				path := filepath.Join(job.binDir, name)
				_ = os.Rename(path+".bak", path)
			}
			m.stopProcesses(inst)
			if err := m.syncNft(); err != nil {
				m.logGeo(job.kernelType, "已恢复分流数据，但内核未启动: "+err.Error())
				return
			}
			m.startProcesses(inst)
			m.logGeo(job.kernelType, "新的分流数据无法启动内核，已恢复原来的文件")
			return
		}
	}
	for _, name := range swapped {
		_ = os.Remove(filepath.Join(job.binDir, name+".bak"))
	}
	m.logGeo(job.kernelType, "分流数据已更新: "+strings.Join(swapped, ", "))
}

func (m *Manager) logGeo(kernelType, msg string) {
	m.mu.Lock()
	inst := m.instances[kernelType]
	m.mu.Unlock()
	if inst != nil {
		inst.logs.add("[flyx] " + msg)
	}
}
