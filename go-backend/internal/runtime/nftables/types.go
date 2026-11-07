package nftables

const (
	ModeAgent    = "agent"
	ModeNftables = "nftables"

	StatusPending = "pending"
	StatusApplied = "applied"
	StatusError   = "error"

	CounterDirectionDNAT       = "dnat"
	CounterDirectionToTarget   = "to-target"
	CounterDirectionFromTarget = "from-target"
)

type Target struct {
	Host string
	Port int
}

type Rule struct {
	Paused     bool
	ForwardID  int64
	InPort     int
	BindIP     string
	TargetHost string
	TargetPort int
	Protocols  []string
}

type NodePlan struct {
	Generation string
	NodeID     int64
	Rules      []Rule
}

type SSHConfig struct {
	Host       string
	Port       int
	Username   string
	AuthType   string
	Password   string
	PrivateKey string
	Passphrase string
	SudoMode   string
	// LegacyTables allows reusing an existing flvx / flvx_quota table.
	LegacyTables bool
}

type ApplyResult struct {
	NodeID int64
	Script string
	Hashes map[int64]string
}
