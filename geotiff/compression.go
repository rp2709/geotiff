package geotiff

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"

	tiffLZW "golang.org/x/image/tiff/lzw"
)

const (
	compressionNone       uint16 = 1
	compressionLZW        uint16 = 5
	compressionDeflate    uint16 = 8
	compressionDeflateOld uint16 = 32946
)

var compressionNames = map[uint16]string{
	compressionNone:       "none",
	compressionLZW:        "lzw",
	compressionDeflate:    "deflate",
	compressionDeflateOld: "deflate-old",
}

// compressionFromTags returns the TIFF Compression tag.
// If the tag is absent, TIFF's default value (no compression) is used.
func compressionFromTags(tags Tags) (uint16, error) {
	if _, ok := tags[Compression]; !ok {
		return compressionNone, nil
	}

	values, err := tagUint32Slice(tags, Compression)
	if err != nil {
		return 0, err
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("invalid Compression tag: expected one value, got %d", len(values))
	}
	if values[0] > uint32(^uint16(0)) {
		return 0, fmt.Errorf("invalid Compression value: %d", values[0])
	}

	return uint16(values[0]), nil
}

// expectedFloat32Bytes returns the expected byte count for a block
// containing width * height Float32 samples.
func expectedFloat32Bytes(width, height uint32) (uint64, error) {
	if width == 0 || height == 0 {
		return 0, errors.New("invalid block dimensions")
	}

	pixels := uint64(width) * uint64(height)
	if pixels > ^uint64(0)/4 {
		return 0, errors.New("decoded block size overflows uint64")
	}

	return pixels * 4, nil
}

// decompressTIFFBlock decompresses one TIFF block and verifies its decoded size.
func decompressTIFFBlock(
	compression uint16,
	src []byte,
	expectedBytes uint64,
) ([]byte, error) {
	maxInt := uint64(^uint(0) >> 1)
	if expectedBytes > maxInt-1 {
		return nil, fmt.Errorf("decoded block size %d exceeds platform limit", expectedBytes)
	}

	switch compression {
	case compressionNone:
		if uint64(len(src)) != expectedBytes {
			return nil, fmt.Errorf(
				"invalid uncompressed block size: expected %d bytes, got %d",
				expectedBytes, len(src),
			)
		}
		return src, nil

	case compressionLZW:
		// TIFF uses MSB-first LZW with an 8-bit literal width.
		reader := tiffLZW.NewReader(bytes.NewReader(src), tiffLZW.MSB, 8)
		defer reader.Close()

		decoded, err := io.ReadAll(
			io.LimitReader(reader, int64(expectedBytes)+1),
		)
		if err != nil {
			return nil, fmt.Errorf("LZW decompression failed: %w", err)
		}
		if uint64(len(decoded)) != expectedBytes {
			return nil, fmt.Errorf(
				"invalid LZW block size: expected %d decoded bytes, got %d",
				expectedBytes, len(decoded),
			)
		}
		return decoded, nil

	case compressionDeflate, compressionDeflateOld:
		reader, err := zlib.NewReader(bytes.NewReader(src))
		if err != nil {
			return nil, fmt.Errorf("Deflate decompression failed: %w", err)
		}
		defer reader.Close()

		decoded, err := io.ReadAll(
			io.LimitReader(reader, int64(expectedBytes)+1),
		)
		if err != nil {
			return nil, fmt.Errorf("Deflate decompression failed: %w", err)
		}
		if uint64(len(decoded)) != expectedBytes {
			return nil, fmt.Errorf(
				"invalid Deflate block size: expected %d decoded bytes, got %d",
				expectedBytes, len(decoded),
			)
		}
		return decoded, nil

	default:
		return nil, fmt.Errorf("unsupported TIFF compression value: %d", compression)
	}
}

// readTIFFBlock reads the stored bytes at offset and decompresses them.
func readTIFFBlock(
	r io.ReadSeeker,
	offset uint32,
	byteCount uint32,
	compression uint16,
	expectedBytes uint64,
) ([]byte, error) {
	maxInt := uint64(^uint(0) >> 1)
	if uint64(byteCount) > maxInt {
		return nil, fmt.Errorf("stored block size %d exceeds platform limit", byteCount)
	}

	if _, err := r.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, fmt.Errorf(
			"%w: could not seek to block offset %d: %v",
			errGeoTIFFData, offset, err,
		)
	}

	stored := make([]byte, int(byteCount))
	if _, err := io.ReadFull(r, stored); err != nil {
		return nil, fmt.Errorf(
			"%w: could not read %d stored block bytes: %v",
			errGeoTIFFData, byteCount, err,
		)
	}

	return decompressTIFFBlock(compression, stored, expectedBytes)
}
