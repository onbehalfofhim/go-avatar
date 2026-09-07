package image

import (
	"bytes"
	"errors"
	"fmt"
	stdimage "image"
	"image/jpeg"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxFileSize = 10 * 1024 * 1024

	Thumbnail100 = 100
	Thumbnail300 = 300
)

var (
	ErrEmptyFile       = errors.New("image file is empty")
	ErrFileTooLarge    = errors.New("image file is too large")
	ErrUnsupportedType = errors.New("unsupported image type")
)

type Info struct {
	Format      string
	ContentType string
	Width       int
	Height      int
}

func DecodeAndValidate(data []byte) (stdimage.Image, Info, error) {
	if len(data) == 0 {
		return nil, Info{}, ErrEmptyFile
	}

	if len(data) > MaxFileSize {
		return nil, Info{}, fmt.Errorf(
			"%w: maximum size is %d bytes",
			ErrFileTooLarge,
			MaxFileSize,
		)
	}

	format, err := detectFormat(data)
	if err != nil {
		return nil, Info{}, err
	}

	img, err := decode(data, format)
	if err != nil {
		return nil, Info{}, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()

	return img, Info{
		Format:      format,
		ContentType: contentType(format),
		Width:       bounds.Dx(),
		Height:      bounds.Dy(),
	}, nil
}

func Thumbnail(img stdimage.Image, size int) ([]byte, error) {
	if img == nil {
		return nil, ErrEmptyFile
	}

	if size <= 0 {
		return nil, fmt.Errorf("thumbnail size must be positive")
	}

	cropped := centerCropSquare(img)

	resized := stdimage.NewRGBA(stdimage.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(
		resized,
		resized.Bounds(),
		cropped,
		cropped.Bounds(),
		draw.Over,
		nil,
	)

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, resized, &jpeg.Options{Quality: 90}); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}

	return buffer.Bytes(), nil
}

func detectFormat(data []byte) (string, error) {
	_, format, err := stdimage.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("detect image format: %w", err)
	}

	switch format {
	case "jpeg", "png", "webp":
		return format, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedType, format)
	}
}

func decode(data []byte, format string) (stdimage.Image, error) {
	reader := bytes.NewReader(data)

	img, detectedFormat, err := stdimage.Decode(reader)
	if err != nil {
		return nil, err
	}

	if detectedFormat != format {
		return nil, fmt.Errorf(
			"detected format %q does not match expected format %q",
			detectedFormat,
			format,
		)
	}

	return img, nil
}

func contentType(format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}

func centerCropSquare(img stdimage.Image) stdimage.Image {
	bounds := img.Bounds()

	width := bounds.Dx()
	height := bounds.Dy()

	size := width
	if height < size {
		size = height
	}

	x := bounds.Min.X + (width-size)/2
	y := bounds.Min.Y + (height-size)/2

	cropRect := stdimage.Rect(
		x,
		y,
		x+size,
		y+size,
	)

	cropped := stdimage.NewRGBA(stdimage.Rect(0, 0, size, size))

	draw.Draw(
		cropped,
		cropped.Bounds(),
		img,
		cropRect.Min,
		draw.Src,
	)

	return cropped
}
