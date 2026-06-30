package storage

import (
	"encoding/binary"
	"time"
)

// CurrentVersion is the schema version written into the meta bucket.
const CurrentVersion = "v5.1"

// ByteOrder is used for all uint64 ↔ []byte conversions.
// BigEndian preserves numeric sort order in BoltDB's B+tree.
var ByteOrder = binary.BigEndian

// Bucket names as byte slices.
var (
	BucketMeta     = []byte("meta")
	BucketPathmap  = []byte("pathmap")
	BucketRPathmap = []byte("rpathmap")
	BucketFileMeta = []byte("filemeta")
	BucketInverted = []byte("inverted")
	BucketPending  = []byte("pending")
	BucketDead     = []byte("dead")
)

// Meta keys stored in the meta bucket.
var (
	MetaKeyVersion   = []byte("version")
	MetaKeyRoot      = []byte("root")
	MetaKeyNextFileID = []byte("nextFileId")
)

// FileID is the primary key for files in the index.
type FileID uint64

// FileMeta stores per-file metadata as JSON in the filemeta bucket.
type FileMeta struct {
	Size       int64     `json:"s"`
	MD5        string    `json:"m,omitempty"`
	QuickHash  uint64    `json:"q,omitempty"`
	ModTime    time.Time `json:"t"`
	RetryCount int       `json:"r"`
}

// PendingEntry is stored as JSON in the pending bucket.
type PendingEntry struct {
	Error      string `json:"e"`
	RetryCount int    `json:"r"`
}

// DeadEntry is stored as JSON in the dead bucket.
type DeadEntry struct {
	Error       string    `json:"e"`
	LastAttempt time.Time `json:"l"`
}

// SearchResult holds one search hit assembled from multiple buckets.
type SearchResult struct {
	FileID  FileID
	Path    string
	ModTime time.Time
	Size    int64
}

// Uint64ToBytes encodes a uint64 into an 8-byte big-endian slice.
func Uint64ToBytes(v uint64) []byte {
	var buf [8]byte
	ByteOrder.PutUint64(buf[:], v)
	return buf[:]
}

// BytesToUint64 decodes an 8-byte big-endian slice.
func BytesToUint64(b []byte) uint64 {
	return ByteOrder.Uint64(b)
}
