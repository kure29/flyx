package kcp

import (
	"encoding/json"

	mdata "github.com/go-gost/core/metadata"
	kcp_util "github.com/go-gost/x/internal/util/kcp"
	mdutil "github.com/go-gost/x/metadata/util"
)

const (
	defaultBacklog = 128
)

type metadata struct {
	config  *kcp_util.Config
	backlog int
}

func (l *kcpListener) parseMetadata(md mdata.Metadata) (err error) {
	const (
		backlog    = "backlog"
		configFile = "c"
	)

	if file := mdutil.GetString(md, "kcp.configFile", "configFile", "c"); file != "" {
		l.md.config, err = kcp_util.ParseFromFile(file)
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
		l.md.config = cfg
	}

	if l.md.config == nil {
		l.md.config = kcp_util.DefaultConfig.Clone()
	} else {
		l.md.config = l.md.config.Clone()
	}

	if md != nil {
		if md.IsExists("kcp.tcp") {
			l.md.config.TCP = mdutil.GetBool(md, "kcp.tcp")
		}
		if md.IsExists("kcp.key") {
			l.md.config.Key = mdutil.GetString(md, "kcp.key")
		}
		if md.IsExists("kcp.crypt") {
			l.md.config.Crypt = mdutil.GetString(md, "kcp.crypt")
		}
		if md.IsExists("kcp.mode") {
			l.md.config.Mode = mdutil.GetString(md, "kcp.mode")
		}
		if md.IsExists("kcp.keepalive") {
			l.md.config.KeepAlive = mdutil.GetInt(md, "kcp.keepalive")
		}
		if md.IsExists("kcp.interval") {
			l.md.config.Interval = mdutil.GetInt(md, "kcp.interval")
		}
		if md.IsExists("kcp.mtu") {
			l.md.config.MTU = mdutil.GetInt(md, "kcp.mtu")
		}
		if md.IsExists("kcp.rcvwnd") {
			l.md.config.RcvWnd = mdutil.GetInt(md, "kcp.rcvwnd")
		}
		if md.IsExists("kcp.sndwnd") {
			l.md.config.SndWnd = mdutil.GetInt(md, "kcp.sndwnd")
		}
		if md.IsExists("kcp.sockbuf") {
			l.md.config.SockBuf = mdutil.GetInt(md, "kcp.sockbuf")
		}
		if md.IsExists("kcp.smuxver") {
			l.md.config.SmuxVer = mdutil.GetInt(md, "kcp.smuxver")
		}
		if md.IsExists("kcp.smuxbuf") {
			l.md.config.SmuxBuf = mdutil.GetInt(md, "kcp.smuxbuf")
		}
		if md.IsExists("kcp.streambuf") {
			l.md.config.StreamBuf = mdutil.GetInt(md, "kcp.streambuf")
		}
		if md.IsExists("kcp.nocomp") {
			l.md.config.NoComp = mdutil.GetBool(md, "kcp.nocomp")
		}
		if md.IsExists("kcp.datashard") {
			l.md.config.DataShard = mdutil.GetInt(md, "kcp.datashard")
		}
		if md.IsExists("kcp.parityshard") {
			l.md.config.ParityShard = mdutil.GetInt(md, "kcp.parityshard")
		}
	}

	l.md.backlog = mdutil.GetInt(md, backlog)
	if l.md.backlog <= 0 {
		l.md.backlog = defaultBacklog
	}

	return
}
