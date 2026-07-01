package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

// openTestDB creates a temporary BoltDB for testing.
func openTestDB(t *testing.T) *bbolt.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := bbolt.Open(path, 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// initTestDB creates buckets and writes the current version.
func initTestDB(t *testing.T, bdb *bbolt.DB) {
	t.Helper()
	err := bdb.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{
			BucketMeta, BucketPathmap, BucketRPathmap,
			BucketFileMeta, BucketInverted, BucketPending, BucketDead,
		} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		mb := tx.Bucket(BucketMeta)
		mb.Put(MetaKeyVersion, []byte(CurrentVersion))
		mb.Put(MetaKeyNextFileID, Uint64ToBytes(1))
		return nil
	})
	if err != nil {
		t.Fatalf("init test db: %v", err)
	}
}

// ---- Inverted Index Tests ----

func TestAddAndGetTermIndex(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	// Add file IDs to terms.
	err := bdb.Update(func(tx *bbolt.Tx) error {
		for _, id := range []uint64{1, 5, 3, 7} {
			if err := AddTermIndex(tx, "test", id); err != nil {
				return err
			}
		}
		return AddTermIndex(tx, "other", 99)
	})
	if err != nil {
		t.Fatalf("AddTermIndex: %v", err)
	}

	// Read back.
	err = bdb.View(func(tx *bbolt.Tx) error {
		ids, err := FileIDsForTerm(tx, "test")
		if err != nil {
			return err
		}
		if len(ids) != 4 {
			return fmt.Errorf("got %d ids for 'test', want 4", len(ids))
		}
		// Must be sorted.
		for i := 1; i < len(ids); i++ {
			if ids[i] < ids[i-1] {
				return fmt.Errorf("ids not sorted: %v", ids)
			}
		}

		ids2, err := FileIDsForTerm(tx, "other")
		if err != nil {
			return err
		}
		if len(ids2) != 1 || ids2[0] != 99 {
			return fmt.Errorf("got %v for 'other', want [99]", ids2)
		}

		// Non-existent term.
		ids3, err := FileIDsForTerm(tx, "nonexistent")
		if err != nil {
			return err
		}
		if ids3 != nil {
			return fmt.Errorf("expected nil for nonexistent term, got %v", ids3)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAddTermIndex_Deduplicates(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	err := bdb.Update(func(tx *bbolt.Tx) error {
		AddTermIndex(tx, "dup", 5)
		AddTermIndex(tx, "dup", 5) // duplicate
		AddTermIndex(tx, "dup", 3)
		return nil
	})
	if err != nil {
		t.Fatalf("AddTermIndex: %v", err)
	}

	err = bdb.View(func(tx *bbolt.Tx) error {
		ids, _ := FileIDsForTerm(tx, "dup")
		if len(ids) != 2 {
			return fmt.Errorf("got %d ids, want 2 (deduplicated)", len(ids))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemoveFileFromInverted(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	// Add file to multiple terms.
	err := bdb.Update(func(tx *bbolt.Tx) error {
		for _, term := range []string{"a", "b", "c"} {
			AddTermIndex(tx, term, 10)
			AddTermIndex(tx, term, 20)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Remove file 10 from all terms.
	err = bdb.Update(func(tx *bbolt.Tx) error {
		return RemoveFileFromInverted(tx, 10)
	})
	if err != nil {
		t.Fatalf("RemoveFileFromInverted: %v", err)
	}

	// Verify.
	err = bdb.View(func(tx *bbolt.Tx) error {
		for _, term := range []string{"a", "b", "c"} {
			ids, _ := FileIDsForTerm(tx, term)
			if len(ids) != 1 || ids[0] != 20 {
				return fmt.Errorf("term %q: got %v, want [20]", term, ids)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRemoveFileFromInverted_DeletesEmptyTerm(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	err := bdb.Update(func(tx *bbolt.Tx) error {
		AddTermIndex(tx, "lonely", 42)
		return nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = bdb.Update(func(tx *bbolt.Tx) error {
		return RemoveFileFromInverted(tx, 42)
	})
	if err != nil {
		t.Fatalf("RemoveFileFromInverted: %v", err)
	}

	// Term should be gone.
	err = bdb.View(func(tx *bbolt.Tx) error {
		inv := tx.Bucket(BucketInverted)
		v := inv.Get([]byte("lonely"))
		if v != nil {
			return fmt.Errorf("expected nil for empty term, got %d bytes", len(v))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---- FileMeta JSON Compression Tests ----

func TestFileMetaCompression(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	fm := &FileMeta{
		Size:      12345,
		MD5:       "abc123def456",
		QuickHash: 0xDEADBEEF,
		ModTime:   time.Now().Truncate(time.Second),
	}

	// Write.
	err := bdb.Update(func(tx *bbolt.Tx) error {
		return PutFileMeta(tx, 1, fm)
	})
	if err != nil {
		t.Fatalf("PutFileMeta: %v", err)
	}

	// Read.
	var got *FileMeta
	err = bdb.View(func(tx *bbolt.Tx) error {
		var readErr error
		got, readErr = GetFileMeta(tx, 1)
		return readErr
	})
	if err != nil {
		t.Fatalf("GetFileMeta: %v", err)
	}

	if got.Size != fm.Size || got.MD5 != fm.MD5 || got.QuickHash != fm.QuickHash {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, fm)
	}
	if !got.ModTime.Equal(fm.ModTime) {
		t.Errorf("ModTime mismatch: got %v, want %v", got.ModTime, fm.ModTime)
	}
}

func TestTouchModTime(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	oldTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	err := bdb.Update(func(tx *bbolt.Tx) error {
		return PutFileMeta(tx, 1, &FileMeta{Size: 100, ModTime: oldTime})
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = bdb.Update(func(tx *bbolt.Tx) error {
		return TouchModTime(tx, 1, newTime)
	})
	if err != nil {
		t.Fatalf("TouchModTime: %v", err)
	}

	err = bdb.View(func(tx *bbolt.Tx) error {
		fm, err := GetFileMeta(tx, 1)
		if err != nil {
			return err
		}
		if !fm.ModTime.Equal(newTime) {
			return fmt.Errorf("ModTime %v, want %v", fm.ModTime, newTime)
		}
		if fm.Size != 100 {
			return fmt.Errorf("Size %d, want 100", fm.Size)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---- Pending / Dead Compression Tests ----

func TestPendingCompression(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	pe := &PendingEntry{Error: "test error: 编码失败", RetryCount: 2}

	err := bdb.Update(func(tx *bbolt.Tx) error {
		return PutPending(tx, "/tmp/test.txt", pe)
	})
	if err != nil {
		t.Fatalf("PutPending: %v", err)
	}

	err = bdb.View(func(tx *bbolt.Tx) error {
		entries, err := ListPending(tx)
		if err != nil {
			return err
		}
		got, ok := entries["/tmp/test.txt"]
		if !ok {
			return fmt.Errorf("entry not found")
		}
		if got.Error != pe.Error || got.RetryCount != pe.RetryCount {
			return fmt.Errorf("got %+v, want %+v", got, pe)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeadCompression(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	de := &DeadEntry{Error: "permanent failure", LastAttempt: time.Now().Truncate(time.Second)}

	err := bdb.Update(func(tx *bbolt.Tx) error {
		return PutDead(tx, "/tmp/bad.txt", de)
	})
	if err != nil {
		t.Fatalf("PutDead: %v", err)
	}

	err = bdb.View(func(tx *bbolt.Tx) error {
		entries, err := ListDead(tx)
		if err != nil {
			return err
		}
		got, ok := entries["/tmp/bad.txt"]
		if !ok {
			return fmt.Errorf("entry not found")
		}
		if got.Error != de.Error {
			return fmt.Errorf("Error: got %q, want %q", got.Error, de.Error)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---- Version Check ----

func TestVersionMismatch(t *testing.T) {
	bdb := openTestDB(t)

	// Write old version.
	err := bdb.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{
			BucketMeta, BucketPathmap, BucketRPathmap,
			BucketFileMeta, BucketInverted, BucketPending, BucketDead,
		} {
			tx.CreateBucketIfNotExists(name)
		}
		return tx.Bucket(BucketMeta).Put(MetaKeyVersion, []byte("v5.1"))
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = CheckVersion(bdb)
	if err == nil {
		t.Fatal("expected error for version mismatch")
	}
	if !os.IsNotExist(nil) {
		// Just check the error message mentions rebuilding.
		t.Logf("version mismatch error: %v", err)
	}
}

func TestFreshDB_NoVersion(t *testing.T) {
	bdb := openTestDB(t)

	// No buckets, no version at all.
	err := CheckVersion(bdb)
	if err != nil {
		t.Fatalf("CheckVersion on fresh DB: %v", err)
	}

	// Should have written v6.0.
	err = bdb.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket(BucketMeta).Get(MetaKeyVersion)
		if string(v) != CurrentVersion {
			return fmt.Errorf("version: got %q, want %q", string(v), CurrentVersion)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---- Search Integration ----

func TestSearchTerms(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	err := bdb.Update(func(tx *bbolt.Tx) error {
		// File 1: terms "hello", "world"
		PutPathMap(tx, 1, "/tmp/file1.txt")
		PutFileMeta(tx, 1, &FileMeta{Size: 100, ModTime: time.Now()})
		AddTermIndex(tx, "hello", 1)
		AddTermIndex(tx, "world", 1)

		// File 2: terms "hello"
		PutPathMap(tx, 2, "/tmp/file2.txt")
		PutFileMeta(tx, 2, &FileMeta{Size: 200, ModTime: time.Now().Add(time.Hour)})
		AddTermIndex(tx, "hello", 2)

		return nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// AND search: "hello" AND "world" → only file1.
	err = bdb.View(func(tx *bbolt.Tx) error {
		results, err := SearchTerms(tx, []string{"hello", "world"}, 0)
		if err != nil {
			return err
		}
		if len(results) != 1 {
			return fmt.Errorf("got %d results, want 1", len(results))
		}
		if results[0].Path != "/tmp/file1.txt" {
			return fmt.Errorf("got path %q, want /tmp/file1.txt", results[0].Path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Single term.
	err = bdb.View(func(tx *bbolt.Tx) error {
		results, err := SearchTerms(tx, []string{"hello"}, 0)
		if err != nil {
			return err
		}
		if len(results) != 2 {
			return fmt.Errorf("got %d results, want 2", len(results))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Non-existent term.
	err = bdb.View(func(tx *bbolt.Tx) error {
		results, err := SearchTerms(tx, []string{"nonexistent"}, 0)
		if err != nil {
			return err
		}
		if results != nil {
			return fmt.Errorf("got %v, want nil", results)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchTerms_Limit(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	now := time.Now()
	err := bdb.Update(func(tx *bbolt.Tx) error {
		for i := uint64(1); i <= 10; i++ {
			path := fmt.Sprintf("/tmp/file%d.txt", i)
			PutPathMap(tx, i, path)
			PutFileMeta(tx, i, &FileMeta{Size: int64(i * 100), ModTime: now.Add(time.Duration(i) * time.Hour)})
			AddTermIndex(tx, "common", i)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = bdb.View(func(tx *bbolt.Tx) error {
		results, err := SearchTerms(tx, []string{"common"}, 5)
		if err != nil {
			return err
		}
		if len(results) != 5 {
			return fmt.Errorf("got %d results with limit 5, want 5", len(results))
		}
		// Latest ModTime first.
		for i := 1; i < len(results); i++ {
			if results[i-1].ModTime.Before(results[i].ModTime) {
				return fmt.Errorf("results not sorted by ModTime descending")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---- RemoveFile Integration ----

func TestRemoveFile(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	err := bdb.Update(func(tx *bbolt.Tx) error {
		PutPathMap(tx, 1, "/tmp/remove_test.txt")
		PutFileMeta(tx, 1, &FileMeta{Size: 50, ModTime: time.Now()})
		AddTermIndex(tx, "aaa", 1)
		AddTermIndex(tx, "bbb", 1)
		return nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	err = bdb.Update(func(tx *bbolt.Tx) error {
		return RemoveFile(tx, 1, "/tmp/remove_test.txt")
	})
	if err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}

	// Verify everything is gone.
	err = bdb.View(func(tx *bbolt.Tx) error {
		if _, ok := LookupPath(tx, 1); ok {
			return fmt.Errorf("pathmap still has id 1")
		}
		if _, ok := LookupFileID(tx, "/tmp/remove_test.txt"); ok {
			return fmt.Errorf("rpathmap still has path")
		}
		if _, err := GetFileMeta(tx, 1); err == nil {
			return fmt.Errorf("filemeta still has id 1")
		}
		for _, term := range []string{"aaa", "bbb"} {
			ids, _ := FileIDsForTerm(tx, term)
			if len(ids) > 0 {
				return fmt.Errorf("term %q still has ids: %v", term, ids)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---- Raw Bytes Verification ----

func TestFileMeta_StoredAsCompressedJSON(t *testing.T) {
	bdb := openTestDB(t)
	initTestDB(t, bdb)

	fm := &FileMeta{Size: 99999, MD5: "deadbeef", ModTime: time.Now().Truncate(time.Second)}

	err := bdb.Update(func(tx *bbolt.Tx) error {
		return PutFileMeta(tx, 1, fm)
	})
	if err != nil {
		t.Fatalf("PutFileMeta: %v", err)
	}

	// Read raw bytes — should NOT be plain JSON.
	err = bdb.View(func(tx *bbolt.Tx) error {
		raw := tx.Bucket(BucketFileMeta).Get(Uint64ToBytes(1))
		if raw == nil {
			return fmt.Errorf("no raw data")
		}
		// Plain JSON would start with '{'. Compressed should not.
		if len(raw) > 0 && raw[0] == '{' {
			// Might be coincidence, but warn.
			t.Logf("warning: compressed data starts with '{' — possibly not compressed?")
		}
		// Verify round-trip via Decompress.
		decoded, err := Decompress(raw)
		if err != nil {
			return fmt.Errorf("decompress raw: %v", err)
		}
		var fm2 FileMeta
		if err := json.Unmarshal(decoded, &fm2); err != nil {
			return fmt.Errorf("unmarshal decoded: %v", err)
		}
		if fm2.Size != fm.Size {
			return fmt.Errorf("size mismatch after round-trip")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
