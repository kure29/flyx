// Package geoip finds the country of an IP address with the free DB-IP
// "IP to Country Lite" database (https://db-ip.com, CC BY 4.0).
//
// The database is not shipped with the panel: it is downloaded into the data
// directory on first use and refreshed once a month (DB-IP publishes a new
// file at the start of each month). A file the admin puts there themselves,
// or names with GEOIP_COUNTRY_DB, is used as is and never replaced.
package geoip

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

const (
	// FileName is the database's name in the data directory.
	FileName = "dbip-country-lite.mmdb"
	// RefreshAfter is the age at which a downloaded database is replaced.
	RefreshAfter = 32 * 24 * time.Hour
	// maxDownload bounds the compressed download (about 4 MiB in 2026).
	maxDownload = 64 << 20
	// maxDatabase bounds the uncompressed database (about 8.5 MiB in 2026).
	maxDatabase = 256 << 20
)

// DownloadURL is where the database for a month is published.
var DownloadURL = func(month time.Time) string {
	return fmt.Sprintf("https://download.db-ip.com/free/dbip-country-lite-%s.mmdb.gz", month.Format("2006-01"))
}

// DB is the country database. The zero value has no data; Lookup returns "".
type DB struct {
	path    string
	managed bool // downloaded by the panel, so it may refresh it
	client  *http.Client

	mu     sync.RWMutex
	reader *maxminddb.Reader
}

// New uses the database at path. When managed is false the file is the
// admin's own: it is read but never downloaded or replaced.
func New(path string, managed bool) *DB {
	return &DB{path: path, managed: managed, client: &http.Client{Timeout: 2 * time.Minute}}
}

// Path is where the database is read from.
func (d *DB) Path() string { return d.path }

// Ready reports whether a database is loaded.
func (d *DB) Ready() bool {
	if d == nil {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.reader != nil
}

type record struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
}

// Lookup returns the ISO 3166-1 alpha-2 code (upper case) of ip's country,
// or "" when it is unknown, private, or no database is loaded.
func (d *DB) Lookup(ip net.IP) string {
	if d == nil || ip == nil || !publicIP(ip) {
		return ""
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.reader == nil {
		return ""
	}
	var r record
	if err := d.reader.Lookup(ip, &r); err != nil {
		return ""
	}
	code := strings.ToUpper(strings.TrimSpace(r.Country.ISOCode))
	if len(code) != 2 || code == "ZZ" {
		return ""
	}
	return code
}

func publicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || isCGNAT(ip))
}

func isCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64
}

// Load opens the database file if it exists. A missing file is not an error.
func (d *DB) Load() error {
	reader, err := maxminddb.Open(d.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	d.swap(reader)
	return nil
}

func (d *DB) swap(reader *maxminddb.Reader) {
	d.mu.Lock()
	old := d.reader
	d.reader = reader
	d.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

// Ensure loads the database, downloading it first when the panel manages the
// file and it is missing or older than RefreshAfter. A failed download keeps
// whatever database was loaded before.
func (d *DB) Ensure(ctx context.Context, now time.Time) error {
	if !d.Ready() {
		if err := d.Load(); err != nil && !d.managed {
			return err
		}
	}
	if !d.managed {
		return nil
	}
	if info, err := os.Stat(d.path); err == nil && now.Sub(info.ModTime()) < RefreshAfter && d.Ready() {
		return nil
	}
	return d.download(ctx, now)
}

// download fetches this month's database, or last month's when this month's
// is not out yet, checks that it opens, and puts it in place atomically.
func (d *DB) download(ctx context.Context, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return err
	}
	var lastErr error
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, m := range []time.Time{month, month.AddDate(0, -1, 0)} {
		if err := d.downloadMonth(ctx, DownloadURL(m)); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func (d *DB) downloadMonth(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	gz, err := gzip.NewReader(io.LimitReader(resp.Body, maxDownload))
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer gz.Close()

	tmp, err := os.CreateTemp(filepath.Dir(d.path), ".geoip-*.mmdb")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	n, err := io.Copy(tmp, io.LimitReader(gz, maxDatabase+1))
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	if n > maxDatabase {
		return fmt.Errorf("download %s: database larger than %d MiB", url, maxDatabase>>20)
	}
	// Check it is a database before replacing the old one.
	check, err := maxminddb.Open(tmpName)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	_ = check.Close()
	// CreateTemp makes the file private; the database is public data.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, d.path); err != nil {
		return err
	}
	reader, err := maxminddb.Open(d.path)
	if err != nil {
		return err
	}
	d.swap(reader)
	return nil
}

// Close releases the database.
func (d *DB) Close() {
	if d != nil {
		d.swap(nil)
	}
}
