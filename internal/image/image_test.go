package image

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestDecodeAndValidateJPEG(t *testing.T) {
	data := encodeJPEG(t, 400, 200)

	img, info, err := DecodeAndValidate(data)
	if err != nil {
		t.Fatalf("decode and validate: %v", err)
	}

	if img == nil {
		t.Fatal("expected image")
	}

	if info.Format != "jpeg" {
		t.Fatalf("expected format jpeg, got %q", info.Format)
	}

	if info.ContentType != "image/jpeg" {
		t.Fatalf("expected content type image/jpeg, got %q", info.ContentType)
	}

	if info.Width != 400 || info.Height != 200 {
		t.Fatalf(
			"expected dimensions 400x200, got %dx%d",
			info.Width,
			info.Height,
		)
	}
}

func TestDecodeAndValidatePNG(t *testing.T) {
	data := encodePNG(t, 200, 300)

	_, info, err := DecodeAndValidate(data)
	if err != nil {
		t.Fatalf("decode and validate: %v", err)
	}

	if info.Format != "png" {
		t.Fatalf("expected format png, got %q", info.Format)
	}

	if info.ContentType != "image/png" {
		t.Fatalf("expected content type image/png, got %q", info.ContentType)
	}

	if info.Width != 200 || info.Height != 300 {
		t.Fatalf(
			"expected dimensions 200x300, got %dx%d",
			info.Width,
			info.Height,
		)
	}
}

func TestDecodeAndValidateRejectsEmptyFile(t *testing.T) {
	_, _, err := DecodeAndValidate(nil)
	if err != ErrEmptyFile {
		t.Fatalf("expected ErrEmptyFile, got %v", err)
	}
}

func TestDecodeAndValidateRejectsTooLargeFile(t *testing.T) {
	data := make([]byte, MaxFileSize+1)

	_, _, err := DecodeAndValidate(data)
	if err == nil {
		t.Fatal("expected error")
	}

	if !bytes.Contains([]byte(err.Error()), []byte(ErrFileTooLarge.Error())) {
		t.Fatalf("expected file too large error, got %v", err)
	}
}

func TestThumbnailProducesRequestedSize(t *testing.T) {
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 400, 200))

	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: 100,
				A: 255,
			})
		}
	}

	for _, size := range []int{Thumbnail100, Thumbnail300} {
		data, err := Thumbnail(img, size)
		if err != nil {
			t.Fatalf("create %dx%d thumbnail: %v", size, size, err)
		}

		result, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode thumbnail: %v", err)
		}

		bounds := result.Bounds()
		if bounds.Dx() != size || bounds.Dy() != size {
			t.Fatalf(
				"expected thumbnail %dx%d, got %dx%d",
				size,
				size,
				bounds.Dx(),
				bounds.Dy(),
			)
		}
	}
}

func TestThumbnailCenterCropsWideImage(t *testing.T) {
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 300, 200))

	for y := 0; y < 200; y++ {
		for x := 0; x < 300; x++ {
			switch {
			case x < 50:
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			case x >= 250:
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			default:
				img.Set(x, y, color.RGBA{G: 255, A: 255})
			}
		}
	}

	cropped := centerCropSquare(img)

	if cropped.Bounds().Dx() != 200 || cropped.Bounds().Dy() != 200 {
		t.Fatalf(
			"expected crop 200x200, got %dx%d",
			cropped.Bounds().Dx(),
			cropped.Bounds().Dy(),
		)
	}

	center := cropped.At(
		cropped.Bounds().Min.X+cropped.Bounds().Dx()/2,
		cropped.Bounds().Min.Y+cropped.Bounds().Dy()/2,
	)

	r, g, b, _ := center.RGBA()

	if g <= r || g <= b {
		t.Fatalf(
			"expected center of crop to come from center region, got r=%d g=%d b=%d",
			r,
			g,
			b,
		)
	}
}

func encodeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: 100,
				G: 150,
				B: 200,
				A: 255,
			})
		}
	}

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	return buffer.Bytes()
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{
				R: 100,
				G: 150,
				B: 200,
				A: 255,
			})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	return buffer.Bytes()
}
