// Package geotiff implements a deliberately small, single-file GeoTIFF reader.
//
// Supported raster layout:
//   - classic TIFF (not BigTIFF)
//   - one sample per pixel
//   - 32-bit IEEE floating-point samples
//   - uncompressed data, without a predictor
//   - top-left image orientation
//   - tiled and stripped storage
//   - ModelTiepoint + ModelPixelScale, or ModelTransformation georeferencing
//
// Unsupported layouts are rejected instead of being silently interpreted as
// Float32 data. Coordinates passed to AtCoord are in the raster's model
// coordinate reference system; this package does not perform CRS conversion.
package geotiff

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// head contains the classic TIFF file header.
type head struct {
	byteOrder     binary.ByteOrder
	tIFIdentifier uint16
	iFDByteOffset uint32
}

// iFDEntry is one 12-byte classic TIFF directory entry.
type iFDEntry struct {
	Tag         Tag
	FType       fieldType
	Count       uint32
	ValueOffset uint32
}

func (ifd *iFDEntry) totalBytes() uint64 {
	return uint64(ifd.Count) * ifd.FType.bytes()
}

// value reads and decodes the value associated with an IFD entry. For inline
// values, ValueOffset is the file position of the four-byte value slot.
func (ifd *iFDEntry) value(r io.ReadSeeker, byteOrder binary.ByteOrder) (*tagData, error) {
	size := ifd.totalBytes()
	if size == 0 {
		return nil, fmt.Errorf("unsupported TIFF field type %d for tag %s", ifd.FType, ifd.Tag)
	}
	if size > uint64(int(^uint(0)>>1)) {
		return nil, fmt.Errorf("value for tag %s is too large", ifd.Tag)
	}
	if _, err := r.Seek(int64(ifd.ValueOffset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to value for tag %s: %w", ifd.Tag, err)
	}
	raw := make([]byte, int(size))
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, fmt.Errorf("read value for tag %s: %w", ifd.Tag, err)
	}
	return decodeTagData(ifd.FType, ifd.Count, raw, byteOrder)
}

// tagData stores the decoded value of a TIFF field.
type tagData struct {
	fType              fieldType
	length             uint32
	byteData           []uint8
	asciiData          string
	shortData          []uint16
	longData           []uint32
	floatData          []float32
	doubleData         []float64
	signedByteData     []int8
	signedShortData    []int16
	signedLongData     []int32
	rationalData       []float64
	signedRationalData []float64
}

func decodeTagData(ft fieldType, count uint32, raw []byte, order binary.ByteOrder) (*tagData, error) {
	if uint64(len(raw)) != uint64(count)*ft.bytes() {
		return nil, fmt.Errorf("invalid byte length for TIFF field type %s", ft)
	}
	t := &tagData{fType: ft, length: count}
	br := bytes.NewReader(raw)
	switch ft {
	case BYTE, UNDEFINED:
		t.byteData = append([]uint8(nil), raw...)
	case ASCII:
		t.asciiData = string(raw)
	case SHORT:
		t.shortData = make([]uint16, count)
		if err := binary.Read(br, order, &t.shortData); err != nil {
			return nil, err
		}
	case LONG, IFDType:
		t.longData = make([]uint32, count)
		if err := binary.Read(br, order, &t.longData); err != nil {
			return nil, err
		}
	case FLOAT:
		t.floatData = make([]float32, count)
		if err := binary.Read(br, order, &t.floatData); err != nil {
			return nil, err
		}
	case DOUBLE:
		t.doubleData = make([]float64, count)
		if err := binary.Read(br, order, &t.doubleData); err != nil {
			return nil, err
		}
	case SBYTE:
		t.signedByteData = make([]int8, count)
		for i, b := range raw {
			t.signedByteData[i] = int8(b)
		}
	case SSHORT:
		t.signedShortData = make([]int16, count)
		if err := binary.Read(br, order, &t.signedShortData); err != nil {
			return nil, err
		}
	case SLONG:
		t.signedLongData = make([]int32, count)
		if err := binary.Read(br, order, &t.signedLongData); err != nil {
			return nil, err
		}
	case RATIONAL:
		t.rationalData = make([]float64, count)
		for i := range t.rationalData {
			var numerator, denominator uint32
			if err := binary.Read(br, order, &numerator); err != nil {
				return nil, err
			}
			if err := binary.Read(br, order, &denominator); err != nil {
				return nil, err
			}
			if denominator == 0 {
				return nil, fmt.Errorf("zero denominator in RATIONAL TIFF value")
			}
			t.rationalData[i] = float64(numerator) / float64(denominator)
		}
	case SRATIONAL:
		t.signedRationalData = make([]float64, count)
		for i := range t.signedRationalData {
			var numerator, denominator int32
			if err := binary.Read(br, order, &numerator); err != nil {
				return nil, err
			}
			if err := binary.Read(br, order, &denominator); err != nil {
				return nil, err
			}
			if denominator == 0 {
				return nil, fmt.Errorf("zero denominator in SRATIONAL TIFF value")
			}
			t.signedRationalData[i] = float64(numerator) / float64(denominator)
		}
	default:
		return nil, fmt.Errorf("unsupported TIFF field type %d", ft)
	}
	return t, nil
}

// value retains the original package's compact tag-value accessor.
func (t tagData) value() (fieldType, []interface{}) {
	switch t.fType {
	case SHORT:
		return t.fType, []interface{}{t.shortData}
	case LONG, IFDType:
		return t.fType, []interface{}{t.longData}
	case FLOAT:
		return t.fType, []interface{}{t.floatData}
	case DOUBLE:
		return t.fType, []interface{}{t.doubleData}
	case BYTE, UNDEFINED:
		return t.fType, []interface{}{t.byteData}
	case ASCII:
		return t.fType, []interface{}{t.asciiData}
	case SBYTE:
		return t.fType, []interface{}{t.signedByteData}
	case SSHORT:
		return t.fType, []interface{}{t.signedShortData}
	case SLONG:
		return t.fType, []interface{}{t.signedLongData}
	case RATIONAL:
		return t.fType, []interface{}{t.rationalData}
	case SRATIONAL:
		return t.fType, []interface{}{t.signedRationalData}
	default:
		return NONE, nil
	}
}

func (t tagData) String() string {
	_, values := t.value()
	if len(values) == 0 {
		return fmt.Sprintf("%s %d", t.fType, t.length)
	}
	return fmt.Sprintf("%s %d %v", t.fType, t.length, values[0])
}

// Tags maps TIFF tag identifiers to their values.
type Tags map[Tag]tagData

// IFD contains a parsed image file directory.
type IFD struct {
	Offset     uint32
	NextOffset int64
	Tags       Tags
}

var errByteOrder = errors.New("could not parse TIFF header byte order")
var errGeoTIFFData = errors.New("could not read GeoTIFF data")

func readHeader(r io.Reader) (head, error) {
	var h head
	var marker uint16
	if err := binary.Read(r, binary.BigEndian, &marker); err != nil {
		return h, fmt.Errorf("%w: %v", errByteOrder, err)
	}
	switch marker {
	case littleEndian:
		h.byteOrder = binary.LittleEndian
	case bigEndian:
		h.byteOrder = binary.BigEndian
	default:
		return h, errByteOrder
	}
	if err := binary.Read(r, h.byteOrder, &h.tIFIdentifier); err != nil {
		return h, fmt.Errorf("read TIFF identifier: %w", err)
	}
	if h.tIFIdentifier != tiffIdentifier {
		if h.tIFIdentifier == 43 {
			return h, errors.New("BigTIFF is not supported")
		}
		return h, fmt.Errorf("invalid TIFF identifier: expected %d, got %d", tiffIdentifier, h.tIFIdentifier)
	}
	if err := binary.Read(r, h.byteOrder, &h.iFDByteOffset); err != nil {
		return h, fmt.Errorf("read first IFD offset: %w", err)
	}
	return h, nil
}

// readTags reads every classic TIFF IFD. It appends each directory once,
// after all of its entries and its actual next-IFD pointer have been read.
func readTags(r io.ReadSeeker) ([]IFD, head, error) {
	ifds := make([]IFD, 0, 1)
	h, err := readHeader(r)
	if err != nil {
		return ifds, h, fmt.Errorf("failed to read TIFF header: %w", err)
	}
	fileSize, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return ifds, h, fmt.Errorf("determine TIFF file size: %w", err)
	}
	if fileSize < 8 {
		return ifds, h, errors.New("truncated TIFF header")
	}

	offset := h.iFDByteOffset
	visited := make(map[uint32]struct{})
	for offset != 0 {
		if _, exists := visited[offset]; exists {
			return ifds, h, fmt.Errorf("cyclic IFD pointer at offset %d", offset)
		}
		visited[offset] = struct{}{}
		if _, err := r.Seek(int64(offset), io.SeekStart); err != nil {
			return ifds, h, fmt.Errorf("seek to IFD at %d: %w", offset, err)
		}
		var count uint16
		if err := binary.Read(r, h.byteOrder, &count); err != nil {
			return ifds, h, fmt.Errorf("read IFD entry count at %d: %w", offset, err)
		}
		if count == 0 {
			return ifds, h, fmt.Errorf("IFD at offset %d contains no entries", offset)
		}

		current := IFD{Offset: offset, Tags: make(Tags)}
		for i := uint16(0); i < count; i++ {
			entryStart, seekErr := r.Seek(0, io.SeekCurrent)
			if seekErr != nil {
				return ifds, h, seekErr
			}
			var entry iFDEntry
			if err := binary.Read(r, h.byteOrder, &entry); err != nil {
				return ifds, h, fmt.Errorf("read IFD entry %d at %d: %w", i, offset, err)
			}
			entryEnd := entryStart + 12
			if entry.FType.bytes() == 0 {
				// Unknown field types cannot be safely decoded, but the 12-byte
				// entry itself is known, so skip it and continue with other tags.
				continue
			}
			valueOffset := int64(entry.ValueOffset)
			if entry.totalBytes() <= fourByte {
				// The value is stored in the entry's final four bytes. Preserve
				// its byte sequence exactly; interpreting ValueOffset as an
				// integer would reverse inline ASCII/BYTE values on one endian.
				valueOffset = entryStart + 8
				if valueOffset > math.MaxUint32 {
					return ifds, h, fmt.Errorf("inline value for tag %s has an invalid offset", entry.Tag)
				}
				entry.ValueOffset = uint32(valueOffset)
			}
			valueSize := entry.totalBytes()
			if valueOffset < 0 || valueOffset > fileSize || valueSize > uint64(fileSize-valueOffset) {
				return ifds, h, fmt.Errorf("value for tag %s lies outside the TIFF file", entry.Tag)
			}
			value, err := entry.value(r, h.byteOrder)
			if err != nil {
				return ifds, h, err
			}
			current.Tags[entry.Tag] = *value
			if _, err := r.Seek(entryEnd, io.SeekStart); err != nil {
				return ifds, h, fmt.Errorf("restore IFD position: %w", err)
			}
		}
		var next uint32
		if err := binary.Read(r, h.byteOrder, &next); err != nil {
			return ifds, h, fmt.Errorf("read next IFD offset after %d: %w", offset, err)
		}
		current.NextOffset = int64(next)
		ifds = append(ifds, current)
		offset = next
	}
	return ifds, h, nil
}

// tagUint32Slice returns a tag as unsigned integers, accepting TIFF SHORT
// and LONG (and the classic IFD type).
func tagUint32Slice(tags Tags, tag Tag) ([]uint32, error) {
	v, ok := tags[tag]
	if !ok {
		return nil, fmt.Errorf("%w: missing tag %s", errGeoTIFFData, tag)
	}
	switch v.fType {
	case SHORT:
		out := make([]uint32, len(v.shortData))
		for i, x := range v.shortData {
			out[i] = uint32(x)
		}
		return out, nil
	case LONG, IFDType:
		return append([]uint32(nil), v.longData...), nil
	default:
		return nil, fmt.Errorf("%w: tag %s has type %s, expected SHORT or LONG", errGeoTIFFData, tag, v.fType)
	}
}

func optionalTagUint32(tags Tags, tag Tag, defaultValue uint32) ([]uint32, error) {
	if _, ok := tags[tag]; !ok {
		return []uint32{defaultValue}, nil
	}
	return tagUint32Slice(tags, tag)
}

func oneTagUint32(tags Tags, tag Tag, defaultValue uint32) (uint32, error) {
	values, err := optionalTagUint32(tags, tag, defaultValue)
	if err != nil {
		return 0, err
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("tag %s must contain exactly one value, got %d", tag, len(values))
	}
	return values[0], nil
}

func tagDoubleSlice(tags Tags, tag Tag) ([]float64, error) {
	v, ok := tags[tag]
	if !ok {
		return nil, fmt.Errorf("%w: missing tag %s", errGeoTIFFData, tag)
	}
	if v.fType != DOUBLE {
		return nil, fmt.Errorf("%w: tag %s has type %s, expected DOUBLE", errGeoTIFFData, tag, v.fType)
	}
	return append([]float64(nil), v.doubleData...), nil
}

// validateRasterLayout deliberately limits decoding to a known-safe TIFF
// layout. TIFF defaults are applied where the specification defines them.
func validateRasterLayout(tags Tags) error {
	bits, err := tagUint32Slice(tags, BitsPerSample)
	if err != nil {
		return err
	}
	if len(bits) != 1 || bits[0] != 32 {
		return fmt.Errorf("unsupported BitsPerSample %v: only one 32-bit sample is supported", bits)
	}
	samples, err := oneTagUint32(tags, SamplesPerPixel, 1)
	if err != nil {
		return err
	}
	if samples != 1 {
		return fmt.Errorf("unsupported SamplesPerPixel %d: only single-band rasters are supported", samples)
	}
	sampleFormat, err := oneTagUint32(tags, SampleFormat, 1)
	if err != nil {
		return err
	}
	if sampleFormat != 3 {
		return fmt.Errorf("unsupported SampleFormat %d: expected IEEE floating point (3)", sampleFormat)
	}
	compression, err := compressionFromTags(tags)
	if err != nil {
		return err
	}
	if _, ok := compressionNames[compression]; !ok {
		return fmt.Errorf("%w: unsupported compression %d", errGeoTIFFData, sampleFormat)
	}
	planar, err := oneTagUint32(tags, PlanarConfiguration, 1)
	if err != nil {
		return err
	}
	if planar != 1 {
		return fmt.Errorf("unsupported PlanarConfiguration %d: only chunky/contiguous layout is supported", planar)
	}
	predictor, err := oneTagUint32(tags, Predictor, 1)
	if err != nil {
		return err
	}
	if predictor != 1 {
		return fmt.Errorf("unsupported Predictor %d: only predictor 1 is supported", predictor)
	}
	fillOrder, err := oneTagUint32(tags, FillOrder, 1)
	if err != nil {
		return err
	}
	if fillOrder != 1 {
		return fmt.Errorf("unsupported FillOrder %d: only fill order 1 is supported", fillOrder)
	}
	orientation, err := oneTagUint32(tags, Orientation, 1)
	if err != nil {
		return err
	}
	if orientation != 1 {
		return fmt.Errorf("unsupported Orientation %d: only top-left orientation (1) is supported", orientation)
	}
	return nil
}

func readFloat32Block(r io.ReadSeeker, offset, byteCount uint32, expectedValues uint64, order binary.ByteOrder, label string) ([]float32, error) {
	expectedBytes := expectedValues * 4
	if expectedBytes > math.MaxUint32 || uint64(byteCount) != expectedBytes {
		return nil, fmt.Errorf("%s has %d bytes; expected %d for %d Float32 samples", label, byteCount, expectedBytes, expectedValues)
	}
	if expectedValues > uint64(int(^uint(0)>>1)) {
		return nil, fmt.Errorf("%s is too large for this platform", label)
	}
	if _, err := r.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("%w: seek to %s at %d: %v", errGeoTIFFData, label, offset, err)
	}
	values := make([]float32, int(expectedValues))
	if err := binary.Read(r, order, values); err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", errGeoTIFFData, label, err)
	}
	return values, nil
}

// readData reads tiled, uncompressed, single-band Float32 data.
// readData reads the data from a tiled GeoTIFF file
// into a 2D 32-bit float array (one slice per tile).
func readData(r io.ReadSeeker, tags Tags, header head) ([][]float32, error) {
	bitsPerSampleVals, err := tagUint32Slice(tags, BitsPerSample)
	if err != nil {
		return nil, err
	}
	imageWidthVals, err := tagUint32Slice(tags, ImageWidth)
	if err != nil {
		return nil, err
	}
	imageLengthVals, err := tagUint32Slice(tags, ImageLength)
	if err != nil {
		return nil, err
	}
	tileWidthVals, err := tagUint32Slice(tags, TileWidth)
	if err != nil {
		return nil, err
	}
	tileLengthVals, err := tagUint32Slice(tags, TileLength)
	if err != nil {
		return nil, err
	}
	offsets, err := tagUint32Slice(tags, TileOffsets)
	if err != nil {
		return nil, err
	}
	byteCounts, err := tagUint32Slice(tags, TileByteCounts)
	if err != nil {
		return nil, err
	}

	compression, err := compressionFromTags(tags)
	if err != nil {
		return nil, err
	}

	imageWidth := imageWidthVals[0]
	imageLength := imageLengthVals[0]
	tileWidth := tileWidthVals[0]
	tileLength := tileLengthVals[0]
	bitsPerSample := bitsPerSampleVals[0]

	// This reader currently interprets samples as Float32.
	if bitsPerSample != 32 {
		return nil, fmt.Errorf(
			"unsupported BitsPerSample value: %d (only 32-bit samples are supported)",
			bitsPerSample,
		)
	}

	if tileWidth == 0 || tileLength == 0 {
		return nil, errors.New("invalid tile dimensions")
	}

	// From the TIFF 6.0 Specification (p. 67).
	tilesAcross := (imageWidth + tileWidth - 1) / tileWidth
	tilesDown := (imageLength + tileLength - 1) / tileLength
	tilesPerImage := tilesAcross * tilesDown

	if int(tilesPerImage) != len(offsets) {
		return nil, errors.New("invalid number of offsets for tiles")
	}
	if len(byteCounts) != len(offsets) {
		return nil, errors.New("number of tile byte counts does not match tile offsets")
	}

	// TIFF tiles are stored at their full tile dimensions, including
	// padding on the right and bottom edges of the image.
	expectedBytes, err := expectedFloat32Bytes(tileWidth, tileLength)
	if err != nil {
		return nil, err
	}

	numPixels := int(expectedBytes / 4)
	data := make([][]float32, 0, tilesPerImage)

	for i, offset := range offsets {
		raw, err := readTIFFBlock(
			r,
			offset,
			byteCounts[i],
			compression,
			expectedBytes,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to read tile %d: %w", i, err)
		}

		tileData := make([]float32, numPixels)
		if err := binary.Read(
			bytes.NewReader(raw),
			header.byteOrder,
			&tileData,
		); err != nil {
			return nil, fmt.Errorf(
				"%w: could not decode tile %d samples: %v",
				errGeoTIFFData, i, err,
			)
		}

		data = append(data, tileData)
	}

	return data, nil
}

// readStripData reads the data from a striped GeoTIFF file into a 2D 32-bit
// float array (one slice per strip).
//
// The returned layout is compatible with loc(): each strip is treated as a
// "tile" whose effective width equals imageWidth and effective height equals
// rowsPerStrip.
func readStripData(r io.ReadSeeker, tags Tags, header head) ([][]float32, error) {
	bitsPerSampleVals, err := tagUint32Slice(tags, BitsPerSample)
	if err != nil {
		return nil, err
	}
	imageWidthVals, err := tagUint32Slice(tags, ImageWidth)
	if err != nil {
		return nil, err
	}
	imageLengthVals, err := tagUint32Slice(tags, ImageLength)
	if err != nil {
		return nil, err
	}
	rowsPerStripVals, err := tagUint32Slice(tags, RowsPerStrip)
	if err != nil {
		return nil, err
	}
	offsets, err := tagUint32Slice(tags, StripOffsets)
	if err != nil {
		return nil, err
	}
	byteCounts, err := tagUint32Slice(tags, StripByteCounts)
	if err != nil {
		return nil, err
	}

	compression, err := compressionFromTags(tags)
	if err != nil {
		return nil, err
	}

	imageWidth := imageWidthVals[0]
	imageLength := imageLengthVals[0]
	rowsPerStrip := rowsPerStripVals[0]
	bitsPerSample := bitsPerSampleVals[0]

	if bitsPerSample != 32 {
		return nil, fmt.Errorf(
			"unsupported BitsPerSample value: %d (only 32-bit samples are supported)",
			bitsPerSample,
		)
	}

	if imageWidth == 0 || imageLength == 0 || rowsPerStrip == 0 {
		return nil, errors.New("invalid image or strip dimensions")
	}

	// From the TIFF 6.0 Specification (p. 38).
	numStrips := (uint64(imageLength) + uint64(rowsPerStrip) - 1) /
		uint64(rowsPerStrip)

	if numStrips != uint64(len(offsets)) {
		return nil, errors.New("invalid number of offsets for strips")
	}
	if len(byteCounts) != len(offsets) {
		return nil, errors.New("number of strip byte counts does not match strip offsets")
	}

	data := make([][]float32, 0, len(offsets))

	for i, offset := range offsets {
		// Determine how many rows are actually present in this strip.
		rowStart := uint64(i) * uint64(rowsPerStrip)
		remainingRows := uint64(imageLength) - rowStart

		rowsInStrip := uint64(rowsPerStrip)
		if rowsInStrip > remainingRows {
			rowsInStrip = remainingRows
		}

		expectedBytes, err := expectedFloat32Bytes(
			imageWidth,
			uint32(rowsInStrip),
		)
		if err != nil {
			return nil, fmt.Errorf("invalid strip %d dimensions: %w", i, err)
		}

		raw, err := readTIFFBlock(
			r,
			offset,
			byteCounts[i],
			compression,
			expectedBytes,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to read strip %d: %w", i, err)
		}

		stripData := make([]float32, int(expectedBytes/4))
		if err := binary.Read(
			bytes.NewReader(raw),
			header.byteOrder,
			&stripData,
		); err != nil {
			return nil, fmt.Errorf(
				"%w: could not decode strip %d samples: %v",
				errGeoTIFFData, i, err,
			)
		}

		data = append(data, stripData)
	}

	return data, nil
}

// GeoTIFF stores decoded Float32 samples in TIFF block order. Dimensions use
// uint32 because classic TIFF dimensions are not restricted to uint16.
type GeoTIFF struct {
	tags        Tags
	data        [][]float32
	imageWidth  uint32
	imageLength uint32
	tileWidth   uint32
	tileLength  uint32
	PixelScaleX float64
	PixelScaleY float64

	// affine maps raster coordinates (i,j) to model coordinates:
	// X = a*i + b*j + c; Y = d*i + e*j + f.
	affine       [6]float64
	hasAffine    bool
	pixelIsPoint bool
}

func (g *GeoTIFF) setRasterType() error {
	g.pixelIsPoint = false // GeoTIFF default is RasterPixelIsArea.
	v, ok := g.tags[GeoKeyDirectory]
	if !ok {
		return nil
	}
	if v.fType != SHORT || len(v.shortData) < 4 {
		return errors.New("invalid GeoKeyDirectoryTag")
	}
	keys := v.shortData
	numKeys := int(keys[3])
	if len(keys) < 4+numKeys*4 {
		return errors.New("truncated GeoKeyDirectoryTag")
	}
	const rasterTypeKey uint16 = 1025
	for i := 0; i < numKeys; i++ {
		base := 4 + i*4
		keyID, location, count, valueOffset := keys[base], keys[base+1], keys[base+2], keys[base+3]
		if keyID != rasterTypeKey {
			continue
		}
		if location != 0 || count != 1 {
			return errors.New("unsupported non-inline GTRasterTypeGeoKey")
		}
		switch valueOffset {
		case 1:
			g.pixelIsPoint = false
		case 2:
			g.pixelIsPoint = true
		default:
			return fmt.Errorf("invalid GTRasterTypeGeoKey value %d", valueOffset)
		}
	}
	return nil
}

func (g *GeoTIFF) ensureAffine() error {
	if g.hasAffine {
		return nil
	}
	if g.tags == nil {
		return errors.New("missing georeferencing tags")
	}
	if err := g.setRasterType(); err != nil {
		return err
	}

	if _, ok := g.tags[ModelTransformation]; ok {
		m, err := tagDoubleSlice(g.tags, ModelTransformation)
		if err != nil {
			return err
		}
		if len(m) != 16 {
			return fmt.Errorf("%s must contain 16 DOUBLE values, got %d", ModelTransformation, len(m))
		}
		for _, x := range m {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return errors.New("ModelTransformation contains a non-finite value")
			}
		}
		// This implementation supports affine 2D transformations. Perspective
		// terms and the homogeneous denominator are not used.
		if math.Abs(m[12]) > 1e-12 || math.Abs(m[13]) > 1e-12 || math.Abs(m[15]-1) > 1e-12 {
			return errors.New("unsupported projective ModelTransformation")
		}
		g.affine = [6]float64{m[0], m[1], m[3], m[4], m[5], m[7]}
		g.PixelScaleX = math.Hypot(m[0], m[4])
		g.PixelScaleY = math.Hypot(m[1], m[5])
	} else {
		scale, err := tagDoubleSlice(g.tags, ModelPixelScale)
		if err != nil {
			return fmt.Errorf("missing or invalid %s: %w", ModelPixelScale, err)
		}
		if len(scale) != 3 {
			return fmt.Errorf("%s must contain three DOUBLE values, got %d", ModelPixelScale, len(scale))
		}
		if scale[0] <= 0 || scale[1] <= 0 || math.IsNaN(scale[0]) || math.IsNaN(scale[1]) || math.IsInf(scale[0], 0) || math.IsInf(scale[1], 0) {
			return errors.New("ModelPixelScale X and Y values must be finite and greater than zero")
		}
		tie, err := tagDoubleSlice(g.tags, ModelTiepoint)
		if err != nil {
			return fmt.Errorf("missing or invalid %s: %w", ModelTiepoint, err)
		}
		if len(tie) != 6 {
			return fmt.Errorf("%s must contain exactly six DOUBLE values, got %d", ModelTiepoint, len(tie))
		}
		for _, x := range tie {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return errors.New("ModelTiepoint contains a non-finite value")
			}
		}
		// X = X0 + (i-I0)*ScaleX; Y = Y0 - (j-J0)*ScaleY.
		g.affine = [6]float64{scale[0], 0, tie[3] - tie[0]*scale[0], 0, -scale[1], tie[4] + tie[1]*scale[1]}
		g.PixelScaleX, g.PixelScaleY = scale[0], scale[1]
	}
	det := g.affine[0]*g.affine[4] - g.affine[1]*g.affine[3]
	if math.Abs(det) < 1e-18 || math.IsNaN(det) || math.IsInf(det, 0) {
		return errors.New("georeferencing transform is singular")
	}
	g.hasAffine = true
	return nil
}

func (g *GeoTIFF) rasterToModel(i, j float64) (x, y float64) {
	a := g.affine
	return a[0]*i + a[1]*j + a[2], a[3]*i + a[4]*j + a[5]
}

func (g *GeoTIFF) modelToRaster(x, y float64) (i, j float64, err error) {
	if err = g.ensureAffine(); err != nil {
		return 0, 0, err
	}
	a := g.affine
	det := a[0]*a[4] - a[1]*a[3]
	dx, dy := x-a[2], y-a[5]
	i = (dx*a[4] - a[1]*dy) / det
	j = (a[0]*dy - dx*a[3]) / det
	return i, j, nil
}

func (g *GeoTIFF) AtCoord(x, y float64, interp bool) (float32, error) {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return 0, errors.New("requested coordinates must be finite")
	}
	if g.imageWidth == 0 || g.imageLength == 0 {
		return 0, errors.New("image dimensions are zero")
	}
	i, j, err := g.modelToRaster(x, y)
	if err != nil {
		return 0, err
	}

	const eps = 1e-9
	if g.pixelIsPoint {
		if i < -eps || j < -eps || i > float64(g.imageWidth-1)+eps || j > float64(g.imageLength-1)+eps {
			return 0, fmt.Errorf("requested point %s lies outside the raster", (Point{Lon: x, Lat: y}))
		}
	} else {
		// PixelIsArea has half-open bounds: the right and bottom edges are
		// outside, preventing an edge coordinate from becoming index width/height.
		if i < -eps || j < -eps || i >= float64(g.imageWidth) || j >= float64(g.imageLength) {
			return 0, fmt.Errorf("requested point %s lies outside the raster", (Point{Lon: x, Lat: y}))
		}
	}
	if i < 0 {
		i = 0
	}
	if j < 0 {
		j = 0
	}

	if interp {
		ci, cj := i, j
		if !g.pixelIsPoint {
			ci -= 0.5
			cj -= 0.5
		}
		return g.interpolateRaster(ci, cj)
	}

	var px, py int
	if g.pixelIsPoint {
		px, py = int(math.Round(i)), int(math.Round(j))
	} else {
		px, py = int(math.Floor(i)), int(math.Floor(j))
	}
	if px >= int(g.imageWidth) {
		px = int(g.imageWidth) - 1
	}
	if py >= int(g.imageLength) {
		py = int(g.imageLength) - 1
	}
	return g.loc(px, py)
}

// interpolateRaster performs true bilinear interpolation between adjacent
// sample centres. At the outermost half-cell it extends the nearest edge value.
// NoData and NaN samples are omitted and remaining weights are renormalized.
func (g *GeoTIFF) interpolateRaster(x, y float64) (float32, error) {
	if g.imageWidth == 0 || g.imageLength == 0 {
		return 0, errors.New("image dimensions are zero")
	}
	maxX, maxY := float64(g.imageWidth-1), float64(g.imageLength-1)
	if x < 0 {
		x = 0
	}
	if x > maxX {
		x = maxX
	}
	if y < 0 {
		y = 0
	}
	if y > maxY {
		y = maxY
	}
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	x1, y1 := x0+1, y0+1
	if x1 >= int(g.imageWidth) {
		x1 = int(g.imageWidth) - 1
	}
	if y1 >= int(g.imageLength) {
		y1 = int(g.imageLength) - 1
	}
	tx, ty := x-float64(x0), y-float64(y0)
	coords := [4][2]int{{x0, y0}, {x1, y0}, {x0, y1}, {x1, y1}}
	weights := [4]float64{(1 - tx) * (1 - ty), tx * (1 - ty), (1 - tx) * ty, tx * ty}
	var weighted, totalWeight float64
	for k, coord := range coords {
		if weights[k] == 0 {
			continue
		}
		v, err := g.loc(coord[0], coord[1])
		if err != nil {
			return 0, err
		}
		if g.isNoData(v) || math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			continue
		}
		weighted += float64(v) * weights[k]
		totalWeight += weights[k]
	}
	if totalWeight == 0 {
		return 0, errors.New("cannot interpolate: all surrounding samples are NoData")
	}
	return float32(weighted / totalWeight), nil
}

// AtPoints returns image values for a sequence of model-coordinate points.
func (g *GeoTIFF) AtPoints(points []Point, interp bool) ([]float32, error) {
	values := make([]float32, len(points))
	for i, p := range points {
		v, err := g.AtCoord(p.Lon, p.Lat, interp)
		if err != nil {
			return nil, fmt.Errorf("point %d (%s): %w", i, p, err)
		}
		values[i] = v
	}
	return values, nil
}

// loc returns a sample by integer column and row, independent of storage type.
func (g *GeoTIFF) loc(x, y int) (float32, error) {
	if x < 0 || x >= int(g.imageWidth) || y < 0 || y >= int(g.imageLength) {
		return 0, errors.New("point lies outside image")
	}
	if g.tileWidth == 0 || g.tileLength == 0 {
		return 0, errors.New("invalid zero tile dimensions")
	}
	tilesAcross := (uint64(g.imageWidth) + uint64(g.tileWidth) - 1) / uint64(g.tileWidth)
	idAcross, idDown := uint64(x)/uint64(g.tileWidth), uint64(y)/uint64(g.tileLength)
	tileNum := tilesAcross*idDown + idAcross
	localX := uint64(x) % uint64(g.tileWidth)
	localY := uint64(y) % uint64(g.tileLength)
	index := localY*uint64(g.tileWidth) + localX
	if tileNum >= uint64(len(g.data)) {
		return 0, fmt.Errorf("missing data block %d", tileNum)
	}
	if index >= uint64(len(g.data[tileNum])) {
		return 0, fmt.Errorf("sample index %d is outside data block %d", index, tileNum)
	}
	return g.data[tileNum][index], nil
}

// Bounds returns the four transformed raster corners. For rotated rasters
// these are the actual corners, not an axis-aligned bounding-box rectangle.
func (g *GeoTIFF) Bounds() (*CornerCoordinates, error) {
	if g.imageWidth == 0 || g.imageLength == 0 {
		return nil, errors.New("image dimensions are zero")
	}
	if err := g.ensureAffine(); err != nil {
		return nil, err
	}
	var right, bottom float64
	if g.pixelIsPoint {
		right, bottom = float64(g.imageWidth-1), float64(g.imageLength-1)
	} else {
		right, bottom = float64(g.imageWidth), float64(g.imageLength)
	}
	ulx, uly := g.rasterToModel(0, 0)
	urx, ury := g.rasterToModel(right, 0)
	llx, lly := g.rasterToModel(0, bottom)
	lrx, lry := g.rasterToModel(right, bottom)
	return &CornerCoordinates{
		UpperLeft: Point{Lon: ulx, Lat: uly}, UpperRight: Point{Lon: urx, Lat: ury},
		LowerLeft: Point{Lon: llx, Lat: lly}, LowerRight: Point{Lon: lrx, Lat: lry},
	}, nil
}

// Point contains model X and Y coordinates. Lon/Lat field names are retained
// for compatibility; values need not be geographic degrees.
type Point struct{ Lon, Lat float64 }

func (p Point) String() string        { return fmt.Sprintf("%011.7f,  %011.7f", p.Lon, p.Lat) }
func (p *Point) Equals(px Point) bool { return p.Lon == px.Lon && p.Lat == px.Lat }

// Distance returns the haversine distance in metres. It is meaningful only
// when both points are longitude/latitude coordinates in degrees.
func (p *Point) Distance(px Point) float64 {
	const earthRadiusInMetres = 6371008.8
	toRadians := func(deg float64) float64 { return math.Pi * deg / 180 }
	lat1, lat2 := toRadians(p.Lat), toRadians(px.Lat)
	dLat, dLon := lat2-lat1, toRadians(px.Lon-p.Lon)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusInMetres * math.Asin(math.Sqrt(a))
}

// CornerCoordinates contains the transformed raster corners.
type CornerCoordinates struct{ UpperLeft, LowerLeft, UpperRight, LowerRight Point }

// Contains tests the axis-aligned envelope of the four corners. For a rotated
// raster this is only a coarse test; AtCoord performs the exact inverse test.
func (cc CornerCoordinates) Contains(p Point) bool {
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, q := range []Point{cc.UpperLeft, cc.UpperRight, cc.LowerLeft, cc.LowerRight} {
		minX = math.Min(minX, q.Lon)
		maxX = math.Max(maxX, q.Lon)
		minY = math.Min(minY, q.Lat)
		maxY = math.Max(maxY, q.Lat)
	}
	return p.Lon >= minX && p.Lon <= maxX && p.Lat >= minY && p.Lat <= maxY
}

func (cc CornerCoordinates) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Upper Left   ( %s )\n", cc.UpperLeft)
	fmt.Fprintf(&sb, "Lower Left   ( %s )\n", cc.LowerLeft)
	fmt.Fprintf(&sb, "Upper Right  ( %s )\n", cc.UpperRight)
	fmt.Fprintf(&sb, "Lower Right  ( %s )\n", cc.LowerRight)
	return sb.String()
}

func (g *GeoTIFF) noDataValue() (float32, bool) {
	v, ok := g.tags[GDALNoData]
	if !ok || v.fType != ASCII {
		return 0, false
	}
	s := strings.TrimSpace(strings.TrimRight(v.asciiData, "\x00"))
	parsed, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0, false
	}
	return float32(parsed), true
}

func (g *GeoTIFF) isNoData(v float32) bool {
	nd, ok := g.noDataValue()
	if !ok {
		return false
	}
	if math.IsNaN(float64(nd)) {
		return math.IsNaN(float64(v))
	}
	return v == nd
}

// Read parses a classic GeoTIFF containing one uncompressed Float32 band.
func Read(r io.ReadSeeker) (*GeoTIFF, error) {
	ifds, header, err := readTags(r)
	if err != nil {
		return nil, err
	}
	if len(ifds) == 0 {
		return nil, errors.New("TIFF contains no image file directory")
	}
	// This implementation intentionally reads the first IFD only.
	tags := ifds[0].Tags
	if err := validateRasterLayout(tags); err != nil {
		return nil, err
	}
	imageWidth, err := oneTagUint32(tags, ImageWidth, 0)
	if err != nil {
		return nil, err
	}
	imageLength, err := oneTagUint32(tags, ImageLength, 0)
	if err != nil {
		return nil, err
	}
	if imageWidth == 0 || imageLength == 0 {
		return nil, errors.New("image dimensions must be greater than zero")
	}

	g := &GeoTIFF{tags: tags, imageWidth: imageWidth, imageLength: imageLength}
	_, hasTileWidth := tags[TileWidth]
	_, hasTileLength := tags[TileLength]
	if hasTileWidth != hasTileLength {
		return nil, errors.New("TIFF must contain both TileWidth and TileLength")
	}
	if hasTileWidth {
		g.data, err = readData(r, tags, header)
		if err != nil {
			return nil, err
		}
		g.tileWidth, err = oneTagUint32(tags, TileWidth, 0)
		if err != nil {
			return nil, err
		}
		g.tileLength, err = oneTagUint32(tags, TileLength, 0)
		if err != nil {
			return nil, err
		}
	} else {
		g.data, err = readStripData(r, tags, header)
		if err != nil {
			return nil, err
		}
		g.tileWidth = imageWidth
		g.tileLength, err = oneTagUint32(tags, RowsPerStrip, math.MaxUint32)
		if err != nil {
			return nil, err
		}
		if g.tileLength > imageLength {
			g.tileLength = imageLength
		}
	}
	if err := g.ensureAffine(); err != nil {
		return nil, fmt.Errorf("invalid GeoTIFF georeferencing: %w", err)
	}
	return g, nil
}

// New creates a GeoTIFF in memory. Each supplied tile must contain the full
// tileWidth*tileLength samples, including padding beyond image edges.
func New(data [][]float32, imageWidth, imageLength, tileWidth, tileLength uint32, pixelScaleX, pixelScaleY float64, tags Tags) (*GeoTIFF, error) {
	if imageWidth == 0 || imageLength == 0 || tileWidth == 0 || tileLength == 0 {
		return nil, errors.New("image and tile dimensions must be greater than zero")
	}
	if pixelScaleX <= 0 || pixelScaleY <= 0 || math.IsNaN(pixelScaleX) || math.IsNaN(pixelScaleY) || math.IsInf(pixelScaleX, 0) || math.IsInf(pixelScaleY, 0) {
		return nil, errors.New("pixel scale values must be finite and greater than zero")
	}
	tilesAcross := (uint64(imageWidth) + uint64(tileWidth) - 1) / uint64(tileWidth)
	tilesDown := (uint64(imageLength) + uint64(tileLength) - 1) / uint64(tileLength)
	tilesPerImage := tilesAcross * tilesDown
	if tilesPerImage != uint64(len(data)) {
		return nil, errors.New("invalid number of tiles for data")
	}
	expected := uint64(tileWidth) * uint64(tileLength)
	for i, tile := range data {
		if uint64(len(tile)) != expected {
			return nil, fmt.Errorf("tile %d has %d samples; expected %d", i, len(tile), expected)
		}
	}
	if tags == nil {
		tags = make(Tags)
	}
	g := &GeoTIFF{
		data: data, imageWidth: imageWidth, imageLength: imageLength,
		tileWidth: tileWidth, tileLength: tileLength,
		PixelScaleX: pixelScaleX, PixelScaleY: pixelScaleY, tags: tags,
	}
	if err := g.setRasterType(); err != nil {
		return nil, err
	}
	// A New-created object may be useful for pixel-only operations without
	// georeferencing tags; Bounds/AtCoord will then return a clear error.
	if _, ok := tags[ModelTransformation]; ok {
		if err := g.ensureAffine(); err != nil {
			return nil, err
		}
	} else if _, hasScale := tags[ModelPixelScale]; hasScale {
		if _, hasTie := tags[ModelTiepoint]; hasTie {
			if err := g.ensureAffine(); err != nil {
				return nil, err
			}
		}
	}
	return g, nil
}

// GeoTIFFStats contains summary statistics for valid image samples.
type GeoTIFFStats struct {
	Min    float32
	Max    float32
	Mean   float32
	StdDev float32
	Count  uint64
}

func (gs GeoTIFFStats) String() string {
	return fmt.Sprintf("Minimum=%.3f, Maximum=%.3f, Mean=%.3f, StdDev=%.3f", gs.Min, gs.Max, gs.Mean, gs.StdDev)
}

// Stats computes population statistics over actual image pixels. Zero is a
// valid value; GDAL_NODATA and non-finite samples are excluded. Padded pixels
// in the final tile are not included.
func (g *GeoTIFF) Stats() GeoTIFFStats {
	stats := GeoTIFFStats{Min: float32(math.Inf(1)), Max: float32(math.Inf(-1))}
	var mean, m2 float64
	for y := 0; y < int(g.imageLength); y++ {
		for x := 0; x < int(g.imageWidth); x++ {
			v, err := g.loc(x, y)
			if err != nil || g.isNoData(v) || math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				continue
			}
			value := float64(v)
			if stats.Count == 0 || v < stats.Min {
				stats.Min = v
			}
			if stats.Count == 0 || v > stats.Max {
				stats.Max = v
			}
			stats.Count++
			delta := value - mean
			mean += delta / float64(stats.Count)
			m2 += delta * (value - mean)
		}
	}
	if stats.Count == 0 {
		nan := float32(math.NaN())
		stats.Min, stats.Max, stats.Mean, stats.StdDev = nan, nan, nan, nan
		return stats
	}
	stats.Mean = float32(mean)
	stats.StdDev = float32(math.Sqrt(m2 / float64(stats.Count)))
	return stats
}
