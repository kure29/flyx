package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type trafficEnvelope struct {
	Stream   string              `json:"stream"`
	Sequence int64               `json:"sequence"`
	Items    []TrafficReportItem `json:"items"`
}
type trafficSpoolState struct {
	Stream   string                `json:"stream"`
	Next     int64                 `json:"next"`
	Pending  *trafficEnvelope      `json:"pending,omitempty"`
	Buffered []TrafficReportItem   `json:"buffered,omitempty"`
	Retry    [][]TrafficReportItem `json:"retry,omitempty"`
}
type trafficSpool struct {
	path  string
	state trafficSpoolState
}

var configuredTrafficSpool *trafficSpool

// ConfigureTrafficSpool is called before services start. A different panel or
// node secret gets a separate file, so old usage cannot debit a new owner.
func ConfigureTrafficSpool(dir, identity string) error {
	path := filepath.Join(dir, fmt.Sprintf("traffic-%x.json", sha256.Sum256([]byte(identity))))
	spool, err := openTrafficSpool(path)
	if err != nil {
		return err
	}
	configuredTrafficSpool = spool
	return nil
}

func openTrafficSpool(path string) (*trafficSpool, error) {
	s := &trafficSpool{path: path}
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := json.Unmarshal(data, &s.state); err != nil {
				return nil, fmt.Errorf("invalid traffic spool: %w", err)
			}
			stream, err := hex.DecodeString(s.state.Stream)
			if err != nil || len(stream) != 16 || s.state.Next < 1 {
				return nil, errors.New("invalid traffic spool identity")
			}
			if p := s.state.Pending; p != nil && (p.Stream != s.state.Stream || p.Sequence != s.state.Next) {
				return nil, errors.New("invalid pending traffic identity")
			}
			return s, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	s.state = trafficSpoolState{Stream: hex.EncodeToString(id[:]), Next: 1}
	if err := s.save(s.state); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *trafficSpool) save(state trafficSpoolState) error {
	if s.path == "" {
		return nil
	} // In-memory managers used by embedded callers/tests.
	return saveTrafficJSON(s.path, state)
}

func saveTrafficJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".traffic-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func mergeTrafficItems(groups ...[]TrafficReportItem) []TrafficReportItem {
	totals := map[string]TrafficReportItem{}
	for _, items := range groups {
		for _, item := range items {
			v := totals[item.N]
			v.N = item.N
			v.U += item.U
			v.D += item.D
			totals[item.N] = v
		}
	}
	out := make([]TrafficReportItem, 0, len(totals))
	for _, item := range totals {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out
}

// recoverRejected never changes a pending identity before the new state is
// durable. Invalid batches are bisected so healthy siblings still get billed.
func (s *trafficSpool) recoverRejected(reason trafficReportRejection) error {
	pending := s.state.Pending
	if pending == nil {
		return nil
	}
	next := s.state
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	next.Stream, next.Next = hex.EncodeToString(id[:]), 1
	replacement := *pending
	replacement.Stream, replacement.Sequence = next.Stream, 1
	next.Pending = &replacement
	switch reason {
	case "reset_stream": // Keep the exact bytes; only the lost cursor changes.
	case "invalid_batch":
		if len(pending.Items) > 1 {
			mid := len(pending.Items) / 2
			replacement.Items = pending.Items[:mid]
			next.Retry = append([][]TrafficReportItem{pending.Items[mid:]}, next.Retry...)
			break
		}
		fallthrough
	case "identity_conflict":
		// Preserve rejected evidence before unblocking the queue. Replaying an
		// identity conflict could charge already committed bytes twice.
		if s.path == "" {
			return errors.New("cannot quarantine traffic without a spool path")
		}
		archive := fmt.Sprintf("%s.rejected-%s-%d.json", s.path, pending.Stream, pending.Sequence)
		if err := saveTrafficJSON(archive, struct {
			Reason trafficReportRejection `json:"reason"`
			Batch  *trafficEnvelope       `json:"batch"`
		}{reason, pending}); err != nil {
			return err
		}
		fmt.Printf("traffic batch quarantined for inspection: %s (%s)\n", archive, reason)
		next.Pending = nil
	default:
		return reason
	}
	if err := s.save(next); err != nil {
		return err
	}
	s.state = next
	return nil
}
