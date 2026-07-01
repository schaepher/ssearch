package storage

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/klauspost/compress/zstd"
)

// Zstd encoder/decoder pools for reuse.
var (
	zstdEncoder *zstd.Encoder
	zstdDecoder *zstd.Decoder
)

func init() {
	var err error
	zstdEncoder, err = zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedDefault),
	)
	if err != nil {
		panic(fmt.Sprintf("zstd encoder init: %v", err))
	}
	zstdDecoder, err = zstd.NewReader(nil)
	if err != nil {
		panic(fmt.Sprintf("zstd decoder init: %v", err))
	}
}

// ---- Zstd compression ----

// Compress compresses data with zstd. Returns nil for empty input.
func Compress(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}
	return zstdEncoder.EncodeAll(data, nil)
}

// Decompress decompresses zstd-compressed data.
func Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	return zstdDecoder.DecodeAll(data, nil)
}

// ---- Delta + Varint ID encoding ----

// EncodeIDs encodes a sorted slice of uint64 IDs to delta+varint bytes.
// Format: [numIDs: 4 bytes LE][varint(delta0)][varint(delta1)]...
func EncodeIDs(ids []uint64) []byte {
	if len(ids) == 0 {
		return nil
	}

	// Defensive sort.
	sorted := make([]uint64, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Allocate buffer: 4 bytes header + worst-case 10 bytes per varint.
	buf := make([]byte, 4, 4+len(sorted)*binary.MaxVarintLen64)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(len(sorted)))

	var prev uint64
	for _, id := range sorted {
		delta := id - prev
		buf = binary.AppendUvarint(buf, delta)
		prev = id
	}
	return buf
}

// DecodeIDs decodes delta+varint bytes back to a sorted slice of uint64 IDs.
func DecodeIDs(data []byte) ([]uint64, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) < 4 {
		return nil, fmt.Errorf("decode ids: data too short (%d bytes)", len(data))
	}

	numIDs := int(binary.LittleEndian.Uint32(data[0:4]))
	if numIDs == 0 {
		return nil, nil
	}

	ids := make([]uint64, 0, numIDs)
	rest := data[4:]
	var prev uint64

	for len(rest) > 0 && len(ids) < numIDs {
		delta, n := binary.Uvarint(rest)
		if n <= 0 {
			return nil, fmt.Errorf("decode ids: invalid varint at offset %d", len(data)-len(rest))
		}
		id := prev + delta
		ids = append(ids, id)
		prev = id
		rest = rest[n:]
	}

	return ids, nil
}
