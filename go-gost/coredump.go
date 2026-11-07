package main

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// The agent runs from its data directory, and so do the nft, kernel and other
// helpers it starts. With a relative kernel core_pattern ("core"), every crash
// of a helper left a core file there; nft 1.0.6 in older agent images crashed
// every minute on newer host kernels and filled the disk.

var coreDumpName = regexp.MustCompile(`^core(\.[0-9]+)?$`)

// removeCoreDumps deletes core files left in dir by earlier crashes. Only
// regular files whose ELF header says ET_CORE are removed.
func removeCoreDumps(dir string) (removed int, freed int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !coreDumpName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil || !isCoreDump(path) {
			continue
		}
		if os.Remove(path) == nil {
			removed++
			freed += info.Size()
		}
	}
	return removed, freed
}

func isCoreDump(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var header [18]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return false
	}
	if string(header[:4]) != "\x7fELF" {
		return false
	}
	var order binary.ByteOrder
	switch header[5] {
	case 1:
		order = binary.LittleEndian
	case 2:
		order = binary.BigEndian
	default:
		return false
	}
	const etCore = 4
	return order.Uint16(header[16:18]) == etCore
}
