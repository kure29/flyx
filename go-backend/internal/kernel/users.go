package kernel

import (
	"fmt"
	"sort"
)

// User is an extra client of an inbound (a subscription user). Each user
// has its own credentials so it can be cut off without touching others.
type User struct {
	ID       int64
	UUID     string
	Password string
}

// Email tags the user in Xray configs (must be unique per inbound).
func (u User) Email() string { return fmt.Sprintf("u%d@flyx", u.ID) }

// Name tags the user in sing-box configs and is the NaïveProxy, SOCKS and
// HTTP username.
func (u User) Name() string { return fmt.Sprintf("u%d", u.ID) }

// xrayUserMatch lists what Xray's routing "user" field sees for the user:
// its email, or for SOCKS / HTTP the username itself.
func xrayUserMatch(id int64) []string {
	u := User{ID: id}
	return []string{u.Email(), u.Name()}
}

// SupportsUsers reports whether the inbound can carry per-user credentials.
// Snell has a single PSK. Shadowsocks stays shared too: 2022 multi-user
// mode would require every client, the owner's included, to send a user
// identity, breaking existing links.
func (in Inbound) SupportsUsers() bool {
	switch in.Protocol {
	case ProtocolVLESS, ProtocolVMess, ProtocolTrojan, ProtocolHysteria2, ProtocolTUIC, ProtocolAnyTLS, ProtocolNaive,
		ProtocolSOCKS, ProtocolHTTP:
		return true
	}
	return false
}

// ForUser returns the inbound as the given user connects to it: the same
// server with the user's credentials. Inbounds without user support are
// returned unchanged (shared credentials).
func (in Inbound) ForUser(u User) Inbound {
	if !in.SupportsUsers() {
		return in
	}
	out := in
	out.Users = nil
	switch in.Protocol {
	case ProtocolVLESS, ProtocolVMess:
		out.Settings.UUID = u.UUID
	case ProtocolTUIC:
		out.Settings.UUID = u.UUID
		out.Settings.Password = u.Password
	case ProtocolNaive, ProtocolSOCKS, ProtocolHTTP:
		out.Settings.Username = u.Name()
		out.Settings.Password = u.Password
	default:
		out.Settings.Password = u.Password
	}
	return out
}

// xrayUserClients returns the extra Xray clients for the inbound's users.
func xrayUserClients(in Inbound) []interface{} {
	if !in.SupportsUsers() {
		return nil
	}
	out := make([]interface{}, 0, len(in.Users))
	for _, u := range in.Users {
		var c obj
		switch in.Protocol {
		case ProtocolVLESS:
			c = obj{"id": u.UUID}
			if in.Settings.Flow != "" {
				c["flow"] = in.Settings.Flow
			}
		case ProtocolVMess:
			c = obj{"id": u.UUID}
		case ProtocolTrojan:
			c = obj{"password": u.Password}
		case ProtocolSOCKS, ProtocolHTTP:
			// Xray reports the username as the user's email.
			out = append(out, obj{"user": u.Name(), "pass": u.Password})
			continue
		default:
			continue
		}
		c["email"] = u.Email()
		out = append(out, c)
	}
	return out
}

// singboxUsers returns the extra sing-box users for the inbound's users.
func singboxUsers(in Inbound) []interface{} {
	if !in.SupportsUsers() {
		return nil
	}
	out := make([]interface{}, 0, len(in.Users))
	for _, u := range in.Users {
		var item obj
		switch in.Protocol {
		case ProtocolVLESS:
			item = obj{"uuid": u.UUID}
			if in.Settings.Flow != "" {
				item["flow"] = in.Settings.Flow
			}
		case ProtocolVMess:
			item = obj{"uuid": u.UUID, "alterId": 0}
		case ProtocolTrojan, ProtocolHysteria2, ProtocolAnyTLS:
			item = obj{"password": u.Password}
		case ProtocolTUIC:
			item = obj{"uuid": u.UUID, "password": u.Password}
		case ProtocolNaive, ProtocolSOCKS, ProtocolHTTP:
			out = append(out, obj{"username": u.Name(), "password": u.Password})
			continue
		default:
			continue
		}
		item["name"] = u.Name()
		out = append(out, item)
	}
	return out
}

// userMarkBase tags the sockets of a user's outgoing connections ("FV" +
// user id). The agent counts each mark in nftables: packets out with the
// mark are the user's upload, packets back on the marked connection the
// download. This works on official builds of both kernels (sing-box ships
// without a per-user stats API).
const (
	userMarkBase = 0x46560000
	userMarkMax  = 0xffff
)

// UserMark is the socket mark of the user's traffic; 0 when the id does not
// fit, and the user is then not counted.
func UserMark(id int64) int64 {
	if id <= 0 || id > userMarkMax {
		return 0
	}
	return userMarkBase | id
}

// UserFromMark is the inverse of UserMark (0 for other marks).
func UserFromMark(mark int64) int64 {
	if mark&^userMarkMax != userMarkBase {
		return 0
	}
	return mark & userMarkMax
}

// markedUsers returns the ids of the counted users, sorted.
func markedUsers(inbounds []Inbound) []int64 {
	seen := map[int64]bool{}
	var ids []int64
	for _, in := range inbounds {
		if !in.SupportsUsers() {
			continue
		}
		for _, u := range in.Users {
			if UserMark(u.ID) != 0 && !seen[u.ID] {
				seen[u.ID] = true
				ids = append(ids, u.ID)
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func userOutboundTag(id int64) string { return fmt.Sprintf("flyx-u%d", id) }
