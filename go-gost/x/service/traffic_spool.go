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
	Stream   string              `json:"stream"`
	Next     int64               `json:"next"`
	Pending  *trafficEnvelope    `json:"pending,omitempty"`
	Buffered []TrafficReportItem `json:"buffered,omitempty"`
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
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".traffic-*")
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
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
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
