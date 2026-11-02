package geotiff

import "fmt"

const (
	littleEndian   uint16 = 0x4949 // "II"
	bigEndian      uint16 = 0x4D4D // "MM"
	tiffIdentifier uint16 = 42
	fourByte              = 4
)

// TIFF field types (TIFF 6.0, plus the IFD extension).
type fieldType uint16

const (
	NONE      fieldType = 0
	BYTE      fieldType = 1
	ASCII     fieldType = 2
	SHORT     fieldType = 3
	LONG      fieldType = 4
	RATIONAL  fieldType = 5
	SBYTE     fieldType = 6
	UNDEFINED fieldType = 7
	SSHORT    fieldType = 8
	SLONG     fieldType = 9
	SRATIONAL fieldType = 10
	FLOAT     fieldType = 11
	DOUBLE    fieldType = 12
	IFDType   fieldType = 13
)

func (t fieldType) bytes() uint64 {
	switch t {
	case BYTE, ASCII, SBYTE, UNDEFINED:
		return 1
	case SHORT, SSHORT:
		return 2
	case LONG, SLONG, FLOAT, IFDType:
		return 4
	case RATIONAL, SRATIONAL, DOUBLE:
		return 8
	default:
		return 0
	}
}

func (t fieldType) String() string {
	names := map[fieldType]string{
		NONE: "NONE", BYTE: "BYTE", ASCII: "ASCII", SHORT: "SHORT",
		LONG: "LONG", RATIONAL: "RATIONAL", SBYTE: "SBYTE",
		UNDEFINED: "UNDEFINED", SSHORT: "SSHORT", SLONG: "SLONG",
		SRATIONAL: "SRATIONAL", FLOAT: "FLOAT", DOUBLE: "DOUBLE",
		IFDType: "IFD",
	}
	if name, ok := names[t]; ok {
		return name
	}
	return fmt.Sprintf("fieldType(%d)", t)
}

// Tag identifies a TIFF or GeoTIFF directory entry.
type Tag uint16

const (
	NewSubfileType            Tag = 254
	ImageWidth                Tag = 256
	ImageLength               Tag = 257
	BitsPerSample             Tag = 258
	Compression               Tag = 259
	PhotometricInterpretation Tag = 262
	FillOrder                 Tag = 266
	ImageDescription          Tag = 270
	StripOffsets              Tag = 273
	Orientation               Tag = 274
	SamplesPerPixel           Tag = 277
	RowsPerStrip              Tag = 278
	StripByteCounts           Tag = 279
	PlanarConfiguration       Tag = 284
	Predictor                 Tag = 317
	TileWidth                 Tag = 322
	TileLength                Tag = 323
	TileOffsets               Tag = 324
	TileByteCounts            Tag = 325
	SampleFormat              Tag = 339
	ModelPixelScale           Tag = 33550
	ModelTiepoint             Tag = 33922
	ModelTransformation       Tag = 34264
	GeoKeyDirectory           Tag = 34735
	GDALNoData                Tag = 42113
)

func (t Tag) String() string {
	names := map[Tag]string{
		NewSubfileType: "NewSubfileType", ImageWidth: "ImageWidth",
		ImageLength: "ImageLength", BitsPerSample: "BitsPerSample",
		Compression: "Compression", PhotometricInterpretation: "PhotometricInterpretation",
		FillOrder: "FillOrder", ImageDescription: "ImageDescription",
		StripOffsets: "StripOffsets", Orientation: "Orientation",
		SamplesPerPixel: "SamplesPerPixel", RowsPerStrip: "RowsPerStrip",
		StripByteCounts: "StripByteCounts", PlanarConfiguration: "PlanarConfiguration",
		Predictor: "Predictor", TileWidth: "TileWidth", TileLength: "TileLength",
		TileOffsets: "TileOffsets", TileByteCounts: "TileByteCounts",
		SampleFormat: "SampleFormat", ModelPixelScale: "ModelPixelScale",
		ModelTiepoint: "ModelTiepoint", ModelTransformation: "ModelTransformation",
		GeoKeyDirectory: "GeoKeyDirectory", GDALNoData: "GDALNoData",
	}
	if name, ok := names[t]; ok {
		return name
	}
	return fmt.Sprintf("Tag(%d)", uint16(t))
}
