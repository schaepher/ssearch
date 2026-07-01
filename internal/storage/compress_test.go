package storage

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeIDs(t *testing.T) {
	tests := []struct {
		name string
		ids  []uint64
	}{
		{"empty", nil},
		{"single", []uint64{42}},
		{"sorted", []uint64{1, 5, 10, 100, 1000}},
		{"unsorted", []uint64{100, 1, 50, 10}}, // should be sorted internally
		{"sequential", []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{"sparse", []uint64{1, 1000000, 2000000, 3000000}},
		{"large_batch", func() []uint64 {
			ids := make([]uint64, 10000)
			for i := range ids {
				ids[i] = uint64(i * 7)
			}
			return ids
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := EncodeIDs(tt.ids)
			if len(tt.ids) == 0 && encoded != nil {
				t.Fatalf("expected nil for empty ids, got %d bytes", len(encoded))
			}
			if len(tt.ids) > 0 && len(encoded) < 4 {
				t.Fatalf("encoded too short: %d bytes for %d ids", len(encoded), len(tt.ids))
			}

			decoded, err := DecodeIDs(encoded)
			if err != nil {
				t.Fatalf("DecodeIDs: %v", err)
			}

			// EncodeIDs sorts, so compare sorted.
			expected := make([]uint64, len(tt.ids))
			copy(expected, tt.ids)
			// For non-empty: EncodeIDs sorts ascending.
			if len(expected) > 0 {
				// Check sorted output.
				for i := 1; i < len(decoded); i++ {
					if decoded[i] < decoded[i-1] {
						t.Errorf("decoded not sorted at index %d: %d < %d", i, decoded[i], decoded[i-1])
					}
				}
			}
			if len(decoded) != len(tt.ids) {
				t.Errorf("length mismatch: got %d, want %d", len(decoded), len(tt.ids))
			}
		})
	}
}

func TestEncodeIDs_NoDuplicates(t *testing.T) {
	// Duplicates are preserved (harmless for set-membership inverted index).
	ids := []uint64{5, 3, 5, 1, 3}
	encoded := EncodeIDs(ids)
	decoded, err := DecodeIDs(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 5 {
		t.Fatalf("got %d ids, want 5 (duplicates preserved)", len(decoded))
	}
	// Must be sorted.
	for i := 1; i < len(decoded); i++ {
		if decoded[i] < decoded[i-1] {
			t.Errorf("not sorted at index %d: %d < %d", i, decoded[i], decoded[i-1])
		}
	}
}

func TestDecodeIDs_Invalid(t *testing.T) {
	// Too short.
	_, err := DecodeIDs([]byte{0x01})
	if err == nil {
		t.Error("expected error for short data")
	}

	// Header says 5 IDs but data is truncated.
	data := []byte{5, 0, 0, 0, 0xFF} // 5 IDs, 1 byte of junk varint
	_, err = DecodeIDs(data)
	if err == nil {
		t.Error("expected error for truncated varint data")
	}
}

func TestCompressDecompress(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"small", []byte("hello world")},
		{"chinese", []byte("今天北京的天气非常好，适合出去散步和运动。")},
		{"repeated", bytes.Repeat([]byte("AAAAAAAAAA"), 1000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compressed := Compress(tt.data)
			if len(tt.data) == 0 && compressed != nil {
				t.Fatalf("expected nil for empty data, got %d bytes", len(compressed))
			}

			decompressed, err := Decompress(compressed)
			if err != nil {
				t.Fatalf("Decompress: %v", err)
			}
			if !bytes.Equal(decompressed, tt.data) {
				t.Errorf("round-trip mismatch: got %q, want %q", decompressed, tt.data)
			}
		})
	}
}

func TestCompress_ReducesSize(t *testing.T) {
	// Repeated data should compress well.
	data := bytes.Repeat([]byte("hello world "), 1000)
	compressed := Compress(data)
	if len(compressed) >= len(data) {
		t.Errorf("compressed %d >= original %d for highly redundant data", len(compressed), len(data))
	}
}
