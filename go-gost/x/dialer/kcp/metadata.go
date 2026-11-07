package kcp

import (
	"encoding/json"
	"time"

	mdata "github.com/go-gost/core/metadata"
	mdutil "github.com/go-gost/x/metadata/util"
	kcp_util "github.com/go-gost/x/internal/util/kcp"
)

type metadata struct {
	handshakeTimeout time.Duration
	config           *kcp_util.Config
}

func (d *kcpDialer) parseMetadata(md mdata.Metadata) (err error) {
	const (
		config           = "config"
		configFile       = "c"
		handshakeTimeout = "handshakeTimeout"
	)

	if file := mdutil.GetString(md, "kcp.configFile", "configFile", "c"); file != "" {
		d.md.config, err = kcp_util.ParseFromFile(file)
		if err != nil {
			return
		}
	}

	if m := mdutil.GetStringMap(md, "kcp.config", "config"); len(m) > 0 {
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		cfg := &kcp_util.Config{}
		if err := json.Unmarshal(b, cfg); err != nil {
			return err
		}
		d.md.config = cfg
	}
	if d.md.config == nil {
		d.md.config = kcp_util.DefaultConfig.Clone()
	} else {
		d.md.config = d.md.config.Clone()
	}

	if md != nil {
		if md.IsExists("kcp.tcp") {
		d.md.config.TCP = mdutil.GetBool(md, "kcp.tcp")
	}
	if md.IsExists("kcp.key") {
		d.md.config.Key = mdutil.GetString(md, "kcp.key")
	}
	if md.IsExists("kcp.crypt") {
		d.md.config.Crypt = mdutil.GetString(md, "kcp.crypt")
	}
	if md.IsExists("kcp.mode") {
		d.md.config.Mode = mdutil.GetString(md, "kcp.mode")
	}
	if md.IsExists("kcp.keepalive") {
		d.md.config.KeepAlive = mdutil.GetInt(md, "kcp.keepalive")
	}
	if md.IsExists("kcp.interval") {
		d.md.config.Interval = mdutil.GetInt(md, "kcp.interval")
	}
	if md.IsExists("kcp.mtu") {
		d.md.config.MTU = mdutil.GetInt(md, "kcp.mtu")
	}
	if md.IsExists("kcp.rcvwnd") {
		d.md.config.RcvWnd = mdutil.GetInt(md, "kcp.rcvwnd")
	}
	if md.IsExists("kcp.sndwnd") {
		d.md.config.SndWnd = mdutil.GetInt(md, "kcp.sndwnd")
	}
	if md.IsExists("kcp.sockbuf") {
		d.md.config.SockBuf = mdutil.GetInt(md, "kcp.sockbuf")
	}
	if md.IsExists("kcp.smuxver") {
		d.md.config.SmuxVer = mdutil.GetInt(md, "kcp.smuxver")
	}
	if md.IsExists("kcp.smuxbuf") {
		d.md.config.SmuxBuf = mdutil.GetInt(md, "kcp.smuxbuf")
	}
	if md.IsExists("kcp.streambuf") {
		d.md.config.StreamBuf = mdutil.GetInt(md, "kcp.streambuf")
	}
		if md.IsExists("kcp.nocomp") {
			d.md.config.NoComp = mdutil.GetBool(md, "kcp.nocomp")
		}
		if md.IsExists("kcp.datashard") {
			d.md.config.DataShard = mdutil.GetInt(md, "kcp.datashard")
		}
		if md.IsExists("kcp.parityshard") {
			d.md.config.ParityShard = mdutil.GetInt(md, "kcp.parityshard")
		}
	}

	d.md.handshakeTimeout = mdutil.GetDuration(md, handshakeTimeout)
	return
}
