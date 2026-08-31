package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
)

const (
	DefaultImageMaxDimension = 1920
	DefaultJPEGQuality       = 82
	// MaxDecodedImagePixels bounds decoder work before an image is resized.
	// JPEG and PNG source images may be larger than DefaultImageMaxDimension:
	// they are safely downscaled after this source-pixel check.
	MaxDecodedImagePixels int64 = 40_000_000
)

var ErrImageDimensions = errors.New("image dimensions exceed the safe processing limit")

// ImageDimensionError retains safe, user-actionable source dimensions while
// preserving errors.Is(err, ErrImageDimensions) for callers that only need
// the classification. It never includes a source path or file content.
type ImageDimensionError struct {
	Width     int
	Height    int
	MaxPixels int64
}

func (e *ImageDimensionError) Error() string { return ErrImageDimensions.Error() }
func (e *ImageDimensionError) Unwrap() error { return ErrImageDimensions }

type OptimizedImage struct {
	Data       []byte
	MimeType   string
	Extension  string
	Width      int
	Height     int
	Compressed bool
}

// OptimizeImage removes JPEG metadata, applies a bounded JPEG quality, uses
// PNG best compression and downsizes oversized JPEG/PNG images. GIF and WebP
// are preserved byte-for-byte because transcoding them would discard animation
// or require a lossy format change.
func OptimizeImage(data []byte) (OptimizedImage, error) {
	return OptimizeImageWithin(data, DefaultImageMaxDimension)
}

// OptimizeImageWithin applies the shared safe decoder and compression policy
// while allowing callers such as Appearance branding to request a smaller
// maximum edge than ordinary content images.
func OptimizeImageWithin(data []byte, maxDimension int) (OptimizedImage, error) {
	if maxDimension <= 0 || maxDimension > DefaultImageMaxDimension {
		maxDimension = DefaultImageMaxDimension
	}
	mimeType := http.DetectContentType(data)
	extension := ""
	switch mimeType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/png":
		extension = ".png"
	case "image/gif":
		extension = ".gif"
	case "image/webp":
		extension = ".webp"
	default:
		return OptimizedImage{}, ErrImageUnsupported
	}

	config, err := imageConfigForMimeType(data, mimeType)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return OptimizedImage{}, ErrImageUnsupported
	}
	if exceedsDecodedPixelLimit(config.Width, config.Height, MaxDecodedImagePixels) {
		return OptimizedImage{}, &ImageDimensionError{
			Width:     config.Width,
			Height:    config.Height,
			MaxPixels: MaxDecodedImagePixels,
		}
	}
	if mimeType == "image/webp" {
		// WebP is preserved byte-for-byte to avoid a lossy format change, but its
		// RIFF dimensions are still validated before it reaches disk so all accepted
		// image types share the same decoded-pixel safety ceiling.
		return OptimizedImage{Data: data, MimeType: mimeType, Extension: extension, Width: config.Width, Height: config.Height}, nil
	}
	if mimeType == "image/gif" {
		return OptimizedImage{Data: data, MimeType: mimeType, Extension: extension, Width: config.Width, Height: config.Height}, nil
	}

	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return OptimizedImage{}, ErrImageUnsupported
	}
	width, height := scaledImageSize(config.Width, config.Height, maxDimension)
	if width != config.Width || height != config.Height {
		source = resizeBilinear(source, width, height)
	}
	var output bytes.Buffer
	switch mimeType {
	case "image/jpeg":
		err = jpeg.Encode(&output, source, &jpeg.Options{Quality: DefaultJPEGQuality})
	case "image/png":
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&output, source)
	}
	if err != nil {
		return OptimizedImage{}, err
	}
	return OptimizedImage{
		Data: output.Bytes(), MimeType: mimeType, Extension: extension,
		Width: width, Height: height, Compressed: true,
	}, nil
}

func imageConfigForMimeType(data []byte, mimeType string) (image.Config, error) {
	if mimeType == "image/webp" {
		return webpConfig(data)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	return config, err
}

// webpConfig reads only the WebP RIFF headers needed to enforce the same
// source-pixel ceiling used for JPEG, PNG, and GIF. The standard library does
// not decode WebP, and this function deliberately does not rasterize it: WebP
// remains byte-for-byte preserved after its dimensions are bounded.
func webpConfig(data []byte) (image.Config, error) {
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return image.Config{}, ErrImageUnsupported
	}
	for offset := 12; offset+8 <= len(data); {
		chunkType := string(data[offset : offset+4])
		chunkLength := int64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := int64(chunkStart) + chunkLength
		if chunkEnd > int64(len(data)) {
			return image.Config{}, ErrImageUnsupported
		}
		chunk := data[chunkStart:int(chunkEnd)]
		switch chunkType {
		case "VP8X":
			if len(chunk) < 10 {
				return image.Config{}, ErrImageUnsupported
			}
			return image.Config{
				Width:  1 + int(chunk[4]) + int(chunk[5])<<8 + int(chunk[6])<<16,
				Height: 1 + int(chunk[7]) + int(chunk[8])<<8 + int(chunk[9])<<16,
			}, nil
		case "VP8 ":
			// Key-frame start code followed by 14-bit little-endian width/height.
			if len(chunk) < 10 || chunk[3] != 0x9d || chunk[4] != 0x01 || chunk[5] != 0x2a {
				return image.Config{}, ErrImageUnsupported
			}
			return image.Config{
				Width:  int(binary.LittleEndian.Uint16(chunk[6:8]) & 0x3fff),
				Height: int(binary.LittleEndian.Uint16(chunk[8:10]) & 0x3fff),
			}, nil
		case "VP8L":
			if len(chunk) < 5 || chunk[0] != 0x2f {
				return image.Config{}, ErrImageUnsupported
			}
			bits := binary.LittleEndian.Uint32(chunk[1:5])
			return image.Config{
				Width:  1 + int(bits&0x3fff),
				Height: 1 + int((bits>>14)&0x3fff),
			}, nil
		}
		// RIFF chunks are padded to an even byte boundary.
		next := chunkEnd
		if chunkLength%2 != 0 {
			next++
		}
		if next > int64(len(data)) || next > int64(^uint(0)>>1) {
			return image.Config{}, ErrImageUnsupported
		}
		offset = int(next)
	}
	return image.Config{}, ErrImageUnsupported
}

func exceedsDecodedPixelLimit(width, height int, limit int64) bool {
	if width <= 0 || height <= 0 || limit <= 0 {
		return false
	}
	// Avoid integer overflow when a hostile image declares extreme dimensions.
	return int64(width) > limit/int64(height)
}

func scaledImageSize(width, height, limit int) (int, int) {
	if limit <= 0 || (width <= limit && height <= limit) {
		return width, height
	}
	scale := math.Min(float64(limit)/float64(width), float64(limit)/float64(height))
	return max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
}

func resizeBilinear(source image.Image, targetWidth, targetHeight int) *image.NRGBA {
	bounds := source.Bounds()
	result := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	xScale := float64(bounds.Dx()) / float64(targetWidth)
	yScale := float64(bounds.Dy()) / float64(targetHeight)
	for y := 0; y < targetHeight; y++ {
		sy := (float64(y)+0.5)*yScale - 0.5
		y0 := int(math.Floor(sy))
		fy := sy - float64(y0)
		if y0 < 0 {
			y0, fy = 0, 0
		}
		y1 := min(y0+1, bounds.Dy()-1)
		for x := 0; x < targetWidth; x++ {
			sx := (float64(x)+0.5)*xScale - 0.5
			x0 := int(math.Floor(sx))
			fx := sx - float64(x0)
			if x0 < 0 {
				x0, fx = 0, 0
			}
			x1 := min(x0+1, bounds.Dx()-1)
			c00 := color.NRGBAModel.Convert(source.At(bounds.Min.X+x0, bounds.Min.Y+y0)).(color.NRGBA)
			c10 := color.NRGBAModel.Convert(source.At(bounds.Min.X+x1, bounds.Min.Y+y0)).(color.NRGBA)
			c01 := color.NRGBAModel.Convert(source.At(bounds.Min.X+x0, bounds.Min.Y+y1)).(color.NRGBA)
			c11 := color.NRGBAModel.Convert(source.At(bounds.Min.X+x1, bounds.Min.Y+y1)).(color.NRGBA)
			result.SetNRGBA(x, y, color.NRGBA{
				R: interpolateChannel(c00.R, c10.R, c01.R, c11.R, fx, fy),
				G: interpolateChannel(c00.G, c10.G, c01.G, c11.G, fx, fy),
				B: interpolateChannel(c00.B, c10.B, c01.B, c11.B, fx, fy),
				A: interpolateChannel(c00.A, c10.A, c01.A, c11.A, fx, fy),
			})
		}
	}
	return result
}

func interpolateChannel(c00, c10, c01, c11 uint8, fx, fy float64) uint8 {
	top := float64(c00)*(1-fx) + float64(c10)*fx
	bottom := float64(c01)*(1-fx) + float64(c11)*fx
	value := top*(1-fy) + bottom*fy
	return uint8(math.Round(math.Max(0, math.Min(255, value))))
}
