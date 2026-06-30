package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"go.etcd.io/bbolt"
)

// DB wraps a BoltDB connection and provides typed access to all buckets.
type DB struct {
	bdb *bbolt.DB
}

// Open opens (or creates) the BoltDB database at path and ensures
// all buckets exist and the version is compatible.
func Open(path string) (*DB, error) {
	bdb, err := bbolt.Open(path, 0600, &bbolt.Options{
		Timeout:      1 * time.Second,
		FreelistType: bbolt.FreelistMapType,
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db := &DB{bdb: bdb}
	if err := db.init(); err != nil {
		bdb.Close()
		return nil, err
	}
	return db, nil
}

// Close closes the underlying BoltDB connection.
func (db *DB) Close() error {
	return db.bdb.Close()
}

// Path returns the database file path.
func (db *DB) PathDB() string {
	return db.bdb.Path()
}

// Bolt returns the underlying *bbolt.DB for external transaction use.
func (db *DB) Bolt() *bbolt.DB {
	return db.bdb
}

// init creates all buckets on first open; on subsequent opens it verifies
// version compatibility.
func (db *DB) init() error {
	return db.bdb.Update(func(tx *bbolt.Tx) error {
		// Create all expected buckets.
		for _, name := range [][]byte{
			BucketMeta, BucketPathmap, BucketRPathmap,
			BucketFileMeta, BucketInverted, BucketPending, BucketDead,
		} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %q: %w", string(name), err)
			}
		}

		mb := tx.Bucket(BucketMeta)
		ver := mb.Get(MetaKeyVersion)
		if ver == nil {
			// Fresh database: write version and init nextFileId = 1.
			if err := mb.Put(MetaKeyVersion, []byte(CurrentVersion)); err != nil {
				return err
			}
			return mb.Put(MetaKeyNextFileID, Uint64ToBytes(1))
		}
		if string(ver) != CurrentVersion {
			return fmt.Errorf("%w: db version %q, expected %q — run 'ssearch index' to rebuild",
				ErrVersionMismatch, string(ver), CurrentVersion)
		}
		return nil
	})
}

// CheckVersion validates that the database version matches. If the DB
// has no version yet (fresh from Open), it is initialized.
func CheckVersion(bdb *bbolt.DB) error {
	return bdb.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{
			BucketMeta, BucketPathmap, BucketRPathmap,
			BucketFileMeta, BucketInverted, BucketPending, BucketDead,
		} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return fmt.Errorf("create bucket %q: %w", string(name), err)
			}
		}

		mb := tx.Bucket(BucketMeta)
		ver := mb.Get(MetaKeyVersion)
		if ver == nil {
			if err := mb.Put(MetaKeyVersion, []byte(CurrentVersion)); err != nil {
				return err
			}
			return mb.Put(MetaKeyNextFileID, Uint64ToBytes(1))
		}
		if string(ver) != CurrentVersion {
			return fmt.Errorf("%w: db version %q, expected %q — run 'ssearch index' to rebuild",
				ErrVersionMismatch, string(ver), CurrentVersion)
		}
		return nil
	})
}

// ---- File ID Allocation ----

// NextFileID atomically allocates the next file ID.
// Must be called inside a writable transaction.
func NextFileID(tx *bbolt.Tx) uint64 {
	mb := tx.Bucket(BucketMeta)
	v := mb.Get(MetaKeyNextFileID)
	id := BytesToUint64(v)
	_ = mb.Put(MetaKeyNextFileID, Uint64ToBytes(id+1))
	return id
}

// ---- Path Map (bidirectional) ----

// LookupFileID resolves a file path to its numeric ID via the reverse map.
func LookupFileID(tx *bbolt.Tx, path string) (uint64, bool) {
	v := tx.Bucket(BucketRPathmap).Get([]byte(path))
	if v == nil {
		return 0, false
	}
	return BytesToUint64(v), true
}

// LookupPath resolves a file ID to its path via the forward map.
func LookupPath(tx *bbolt.Tx, id uint64) (string, bool) {
	v := tx.Bucket(BucketPathmap).Get(Uint64ToBytes(id))
	if v == nil {
		return "", false
	}
	return string(v), true
}

// PutPathMap writes both forward and reverse mappings.
func PutPathMap(tx *bbolt.Tx, id uint64, path string) error {
	if err := tx.Bucket(BucketPathmap).Put(Uint64ToBytes(id), []byte(path)); err != nil {
		return err
	}
	return tx.Bucket(BucketRPathmap).Put([]byte(path), Uint64ToBytes(id))
}

// DeletePathMap removes both forward and reverse mappings.
func DeletePathMap(tx *bbolt.Tx, id uint64, oldPath string) error {
	if err := tx.Bucket(BucketPathmap).Delete(Uint64ToBytes(id)); err != nil {
		return err
	}
	return tx.Bucket(BucketRPathmap).Delete([]byte(oldPath))
}

// AllPathIDs returns all file IDs currently in pathmap.
func AllPathIDs(tx *bbolt.Tx) ([]uint64, error) {
	var ids []uint64
	b := tx.Bucket(BucketPathmap)
	c := b.Cursor()
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		ids = append(ids, BytesToUint64(k))
	}
	return ids, nil
}

// ---- File Metadata ----

// GetFileMeta retrieves file metadata by ID.
func GetFileMeta(tx *bbolt.Tx, id uint64) (*FileMeta, error) {
	v := tx.Bucket(BucketFileMeta).Get(Uint64ToBytes(id))
	if v == nil {
		return nil, fmt.Errorf("%w: filemeta for id %d", ErrNotFound, id)
	}
	var fm FileMeta
	if err := json.Unmarshal(v, &fm); err != nil {
		return nil, fmt.Errorf("unmarshal filemeta id %d: %w", id, err)
	}
	return &fm, nil
}

// PutFileMeta stores file metadata.
func PutFileMeta(tx *bbolt.Tx, id uint64, fm *FileMeta) error {
	data, err := json.Marshal(fm)
	if err != nil {
		return fmt.Errorf("marshal filemeta id %d: %w", id, err)
	}
	return tx.Bucket(BucketFileMeta).Put(Uint64ToBytes(id), data)
}

// TouchModTime updates only the ModTime field of an existing FileMeta entry.
func TouchModTime(tx *bbolt.Tx, id uint64, modTime time.Time) error {
	fm, err := GetFileMeta(tx, id)
	if err != nil {
		return err
	}
	fm.ModTime = modTime
	return PutFileMeta(tx, id, fm)
}

// ---- Inverted Index ----

// AddTermIndex inserts fileID into the term's sub-bucket.
// The sub-bucket is created on demand if it does not exist.
func AddTermIndex(tx *bbolt.Tx, term string, fileID uint64) error {
	inv := tx.Bucket(BucketInverted)
	tb, err := inv.CreateBucketIfNotExists([]byte(term))
	if err != nil {
		return fmt.Errorf("create term bucket %q: %w", term, err)
	}
	return tb.Put(Uint64ToBytes(fileID), nil)
}

// RemoveFileFromInverted deletes fileID from every term sub-bucket.
func RemoveFileFromInverted(tx *bbolt.Tx, fileID uint64) error {
	inv := tx.Bucket(BucketInverted)
	key := Uint64ToBytes(fileID)
	return inv.ForEach(func(term, v []byte) error {
		if v != nil {
			return nil // skip direct values (should not exist)
		}
		tb := inv.Bucket(term)
		if tb != nil {
			_ = tb.Delete(key)
		}
		return nil
	})
}

// FileIDsForTerm returns all file IDs that contain the given term.
func FileIDsForTerm(tx *bbolt.Tx, term string) ([]uint64, error) {
	inv := tx.Bucket(BucketInverted)
	tb := inv.Bucket([]byte(term))
	if tb == nil {
		return nil, nil
	}

	var ids []uint64
	c := tb.Cursor()
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		ids = append(ids, BytesToUint64(k))
	}
	return ids, nil
}

// ---- Pending Bucket ----

// ListPending returns all pending entries keyed by file path.
func ListPending(tx *bbolt.Tx) (map[string]*PendingEntry, error) {
	b := tx.Bucket(BucketPending)
	entries := make(map[string]*PendingEntry)
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var pe PendingEntry
		if err := json.Unmarshal(v, &pe); err != nil {
			return nil, fmt.Errorf("unmarshal pending %q: %w", string(k), err)
		}
		entries[string(k)] = &pe
	}
	return entries, nil
}

// PutPending stores a pending entry.
func PutPending(tx *bbolt.Tx, path string, pe *PendingEntry) error {
	data, err := json.Marshal(pe)
	if err != nil {
		return err
	}
	return tx.Bucket(BucketPending).Put([]byte(path), data)
}

// DeletePending removes a pending entry.
func DeletePending(tx *bbolt.Tx, path string) error {
	return tx.Bucket(BucketPending).Delete([]byte(path))
}

// ClearPending removes all entries from the pending bucket.
func ClearPendingTx(tx *bbolt.Tx) error {
	return tx.DeleteBucket(BucketPending)
}

// ---- Dead Bucket ----

// ListDead returns all dead entries keyed by file path.
func ListDead(tx *bbolt.Tx) (map[string]*DeadEntry, error) {
	b := tx.Bucket(BucketDead)
	entries := make(map[string]*DeadEntry)
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var de DeadEntry
		if err := json.Unmarshal(v, &de); err != nil {
			return nil, fmt.Errorf("unmarshal dead %q: %w", string(k), err)
		}
		entries[string(k)] = &de
	}
	return entries, nil
}

// PutDead stores a dead entry.
func PutDead(tx *bbolt.Tx, path string, de *DeadEntry) error {
	data, err := json.Marshal(de)
	if err != nil {
		return err
	}
	return tx.Bucket(BucketDead).Put([]byte(path), data)
}

// DeleteDead removes a dead entry.
func DeleteDead(tx *bbolt.Tx, path string) error {
	return tx.Bucket(BucketDead).Delete([]byte(path))
}

// MovePendingToDead promotes a pending entry to dead (retry exhausted).
func MovePendingToDead(tx *bbolt.Tx, path string, de *DeadEntry) error {
	if err := DeletePending(tx, path); err != nil {
		return err
	}
	return PutDead(tx, path, de)
}

// ---- Meta Bucket ----

// GetMeta retrieves a string value from the meta bucket.
func GetMeta(tx *bbolt.Tx, key string) string {
	return string(tx.Bucket(BucketMeta).Get([]byte(key)))
}

// PutMeta stores a string value in the meta bucket.
func PutMeta(tx *bbolt.Tx, key, value string) error {
	return tx.Bucket(BucketMeta).Put([]byte(key), []byte(value))
}

// Root returns the project root path stored in meta.
func Root(tx *bbolt.Tx) string {
	return GetMeta(tx, "root")
}

// ---- Full File Removal ----

// RemoveFile performs a complete removal of a file from all indices.
func RemoveFile(tx *bbolt.Tx, id uint64, oldPath string) error {
	if err := RemoveFileFromInverted(tx, id); err != nil {
		return fmt.Errorf("remove inverted: %w", err)
	}
	if err := DeletePathMap(tx, id, oldPath); err != nil {
		return fmt.Errorf("remove pathmap: %w", err)
	}
	if err := tx.Bucket(BucketFileMeta).Delete(Uint64ToBytes(id)); err != nil {
		return fmt.Errorf("remove filemeta: %w", err)
	}
	// Best-effort cleanup of pending/dead.
	_ = DeletePending(tx, oldPath)
	_ = DeleteDead(tx, oldPath)
	return nil
}

// ---- Batch Index Writing ----

// IndexEntry is the pre-processed representation of one file for batch writing.
type IndexEntry struct {
	FileID   uint64 // 0 for new files
	Path     string
	FileMeta FileMeta
	Terms    []string
}

// IndexBatch writes a batch of pre-processed IndexEntry values in one transaction.
func IndexBatch(tx *bbolt.Tx, entries []IndexEntry) error {
	for _, e := range entries {
		id := e.FileID
		if id == 0 {
			id = NextFileID(tx)
		}

		if err := PutPathMap(tx, id, e.Path); err != nil {
			return err
		}
		if err := PutFileMeta(tx, id, &e.FileMeta); err != nil {
			return err
		}
		for _, term := range e.Terms {
			if err := AddTermIndex(tx, term, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- Clear All (Full Rebuild) ----

// ClearAll drops and recreates every bucket. Used by the index command.
func ClearAll(tx *bbolt.Tx) error {
	names := [][]byte{
		BucketMeta, BucketPathmap, BucketRPathmap,
		BucketFileMeta, BucketInverted, BucketPending, BucketDead,
	}
	for _, name := range names {
		if err := tx.DeleteBucket(name); err != nil && err != bbolt.ErrBucketNotFound {
			return err
		}
	}
	for _, name := range names {
		if _, err := tx.CreateBucket(name); err != nil {
			return err
		}
	}
	// Re-initialize meta.
	mb := tx.Bucket(BucketMeta)
	if err := mb.Put(MetaKeyVersion, []byte(CurrentVersion)); err != nil {
		return err
	}
	return mb.Put(MetaKeyNextFileID, Uint64ToBytes(1))
}

// ---- Search ----

// SearchTerms performs an AND intersection of all terms and returns results
// sorted by ModTime descending, limited to limit (0 = no limit).
func SearchTerms(tx *bbolt.Tx, terms []string, limit int) ([]SearchResult, error) {
	if len(terms) == 0 {
		return nil, nil
	}

	// Collect file ID lists for all terms; find the shortest.
	type termList struct {
		term string
		ids  []uint64
	}
	lists := make([]termList, 0, len(terms))
	for _, term := range terms {
		ids, err := FileIDsForTerm(tx, term)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return nil, nil // any term has no matches → empty result
		}
		lists = append(lists, termList{term: term, ids: ids})
	}

	// Sort lists by length ascending; use the shortest as the base set.
	sort.Slice(lists, func(i, j int) bool {
		return len(lists[i].ids) < len(lists[j].ids)
	})

	// Build a set from the shortest list.
	set := make(map[uint64]struct{}, len(lists[0].ids))
	for _, id := range lists[0].ids {
		set[id] = struct{}{}
	}

	// Intersect with remaining lists.
	for _, tl := range lists[1:] {
		termSet := make(map[uint64]struct{}, len(tl.ids))
		for _, id := range tl.ids {
			termSet[id] = struct{}{}
		}
		for id := range set {
			if _, ok := termSet[id]; !ok {
				delete(set, id)
			}
		}
		if len(set) == 0 {
			break
		}
	}

	if len(set) == 0 {
		return nil, nil
	}

	// Assemble results with metadata.
	results := make([]SearchResult, 0, len(set))
	pathmap := tx.Bucket(BucketPathmap)
	filemeta := tx.Bucket(BucketFileMeta)
	for id := range set {
		pathBytes := pathmap.Get(Uint64ToBytes(id))
		if pathBytes == nil {
			continue
		}
		fmBytes := filemeta.Get(Uint64ToBytes(id))
		if fmBytes == nil {
			continue
		}
		var fm FileMeta
		if err := json.Unmarshal(fmBytes, &fm); err != nil {
			continue
		}
		results = append(results, SearchResult{
			FileID:  FileID(id),
			Path:    string(pathBytes),
			ModTime: fm.ModTime,
			Size:    fm.Size,
		})
	}

	// Sort by ModTime descending (newest first).
	sort.Slice(results, func(i, j int) bool {
		return results[i].ModTime.After(results[j].ModTime)
	})

	// Apply limit.
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// ---- Compaction ----

// CompactDB compacts the database at srcPath. It closes the source DB,
// writes a compacted copy, and replaces the original.
func CompactDB(srcPath string) (int64, int64, error) {
	// Get original size.
	fi, err := os.Stat(srcPath)
	if err != nil {
		return 0, 0, fmt.Errorf("stat db: %w", err)
	}
	beforeSize := fi.Size()

	dstPath := srcPath + ".compact.tmp"
	_ = os.Remove(dstPath)

	// Open source read-only.
	src, err := bbolt.Open(srcPath, 0400, &bbolt.Options{
		ReadOnly: true,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		return beforeSize, 0, fmt.Errorf("open src for compact: %w", err)
	}

	// Open destination.
	dst, err := bbolt.Open(dstPath, 0600, &bbolt.Options{
		FreelistType: bbolt.FreelistMapType,
		Timeout:      5 * time.Second,
	})
	if err != nil {
		src.Close()
		return beforeSize, 0, fmt.Errorf("open dst for compact: %w", err)
	}

	// Compact: amount 0 means no page size limit.
	if err := bbolt.Compact(dst, src, 0); err != nil {
		src.Close()
		dst.Close()
		os.Remove(dstPath)
		return beforeSize, 0, fmt.Errorf("compact: %w", err)
	}

	src.Close()
	dst.Close()

	// Replace original with compacted copy.
	if err := os.Rename(dstPath, srcPath); err != nil {
		os.Remove(dstPath)
		return beforeSize, 0, fmt.Errorf("replace db with compacted: %w", err)
	}

	// Get new size.
	fi, err = os.Stat(srcPath)
	if err != nil {
		return beforeSize, 0, nil
	}
	return beforeSize, fi.Size(), nil
}
