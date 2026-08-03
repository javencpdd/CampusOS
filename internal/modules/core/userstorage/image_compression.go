package storage

import (
	"bytes"
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
	maxDecodedImagePixels    = 40_000_000
)

var ErrImageDimensions = errors.New("image dimensions exceed the safe processing limit")

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

	if mimeType == "image/webp" {
		return OptimizedImage{Data: data, MimeType: mimeType, Extension: extension}, nil
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return OptimizedImage{}, ErrImageUnsupported
	}
	if int64(config.Width)*int64(config.Height) > maxDecodedImagePixels {
		return OptimizedImage{}, ErrImageDimensions
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
