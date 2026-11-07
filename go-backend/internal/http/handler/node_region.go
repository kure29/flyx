package handler

import (
	"context"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/geoip"
	"go-backend/internal/store/model"
)

const (
	// How often the nodes' addresses are looked at again; unchanged
	// addresses are skipped, so this is cheap.
	nodeRegionInterval = time.Hour
	// How often the country database is checked for its monthly update.
	nodeRegionDBCheck = 24 * time.Hour
	// Downloading the database may take a while on a slow link.
	nodeRegionDownloadTimeout = 5 * time.Minute
	nodeRegionResolveTimeout  = 5 * time.Second
)

// SetGeoIP sets the country database used to detect where nodes are; nil
// turns detection off.
func (h *Handler) SetGeoIP(db *geoip.DB) {
	if h != nil {
		h.geoip = db
	}
}

// nudgeNodeRegions asks the region loop to look at the nodes now, after a
// node was added or its address changed.
func (h *Handler) nudgeNodeRegions() {
	if h == nil || h.regionNudge == nil {
		return
	}
	select {
	case h.regionNudge <- struct{}{}:
	default:
	}
}

// normalizeNodeRegion accepts a two-letter country code in any case; anything
// else means "detect".
func normalizeNodeRegion(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if len(v) != 2 || v[0] < 'A' || v[0] > 'Z' || v[1] < 'A' || v[1] > 'Z' {
		return ""
	}
	return v
}

func (h *Handler) runNodeRegionLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	db := h.geoip
	if db == nil {
		return
	}
	defer db.Close()

	ensure := func() bool {
		before := db.Ready()
		dctx, cancel := context.WithTimeout(ctx, nodeRegionDownloadTimeout)
		defer cancel()
		info := statFile(db.Path())
		if err := db.Ensure(dctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("node region: country database %s: %v", db.Path(), err)
		}
		// A new database may place addresses differently: look again.
		return db.Ready() && (!before || statFile(db.Path()) != info)
	}

	h.detectNodeRegions(ctx, ensure())
	regions := time.NewTicker(nodeRegionInterval)
	defer regions.Stop()
	dbCheck := time.NewTicker(nodeRegionDBCheck)
	defer dbCheck.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.regionNudge:
			h.detectNodeRegions(ctx, false)
		case <-regions.C:
			h.detectNodeRegions(ctx, false)
		case <-dbCheck.C:
			h.detectNodeRegions(ctx, ensure())
		}
	}
}

// statFile identifies a version of the database file.
func statFile(path string) string {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime().String() + "/" + strconv.FormatInt(info.Size(), 10)
	}
	return ""
}

// detectNodeRegions looks up the country of every node whose address changed
// since the last lookup (all of them with force).
func (h *Handler) detectNodeRegions(ctx context.Context, force bool) {
	if h.geoip == nil || !h.geoip.Ready() {
		return
	}
	nodes, err := h.repo.ListNodeRegionSources()
	if err != nil {
		log.Printf("node region: list nodes: %v", err)
		return
	}
	for _, n := range nodes {
		if ctx.Err() != nil {
			return
		}
		ip := nodeRegionSource(ctx, n)
		from := ""
		if ip != nil {
			from = ip.String()
		}
		if !force && from == n.RegionDetectedFrom {
			continue
		}
		region := ""
		if ip != nil {
			region = h.geoip.Lookup(ip)
		}
		if err := h.repo.SetNodeDetectedRegion(n.ID, region, from); err != nil {
			log.Printf("node region: store region of node %d: %v", n.ID, err)
		}
	}
}

// nodeRegionSource is the public address the node is reached at: its IPv4
// address, else its address or host name (resolved), else its IPv6 address.
func nodeRegionSource(ctx context.Context, n model.Node) net.IP {
	candidates := []string{n.ServerIPV4.String, n.ServerIP, n.ServerIPV6.String}
	for _, c := range candidates {
		if ip := publicAddress(ctx, c); ip != nil {
			return ip
		}
	}
	return nil
}

func publicAddress(ctx context.Context, raw string) net.IP {
	host := strings.TrimSpace(raw)
	if host == "" {
		return nil
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		if isPublicIP(ip) {
			return ip
		}
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, nodeRegionResolveTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(rctx, host)
	if err != nil {
		return nil
	}
	// Prefer IPv4: it is what most nodes are reached at.
	var v6 net.IP
	for _, a := range addrs {
		if !isPublicIP(a.IP) {
			continue
		}
		if a.IP.To4() != nil {
			return a.IP
		}
		if v6 == nil {
			v6 = a.IP
		}
	}
	return v6
}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	v4 := ip.To4()
	return !(v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64)
}
