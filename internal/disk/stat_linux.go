//go:build linux

package disk

import "syscall"

// GetInfo returns total and free bytes available in a directory, e.g. `/`.
func GetInfo(path string) (Info, error) {
	s := syscall.Statfs_t{}
	if err := syscall.Statfs(path, &s); err != nil {
		return Info{}, err
	}

	reservedBlocks := s.Bfree - s.Bavail
	info := Info{
		Total: uint64(s.Frsize) * (s.Blocks - reservedBlocks),
		Free:  uint64(s.Frsize) * s.Bavail,
	}

	return info, nil
}
