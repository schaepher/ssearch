package indexer

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// QuickHash reads the first 4 KB of a file and returns the first 8 bytes
// of its MD5 hash as a uint64. This provides a fast fingerprint for
// detecting content changes without reading the entire file.
func QuickHash(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("quickhash open: %w", err)
	}
	defer f.Close()

	h := md5.New()
	// Read up to 4 KB.
	if _, err := io.CopyN(h, f, 4096); err != nil && err != io.EOF {
		return 0, fmt.Errorf("quickhash read: %w", err)
	}

	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8]), nil
}

// FullMD5 reads the entire file and returns the hex-encoded MD5 hash.
func FullMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("fullmd5 open: %w", err)
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("fullmd5 read: %w", err)
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
