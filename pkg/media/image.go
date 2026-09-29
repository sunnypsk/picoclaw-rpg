package media

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	_ "golang.org/x/image/webp"
)

const MaxImageBytes = 32 << 20
const MaxImagePixels = 40_000_000

// ValidatedImage contains the exact bytes that were decoded, ready for upload.
type ValidatedImage struct {
	Data      []byte
	MIME      string
	Extension string
}

func ReadImage(path string) (ValidatedImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return ValidatedImage{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if err != nil {
		return ValidatedImage{}, err
	}
	return ValidateImage(data)
}

func ValidateImage(data []byte) (ValidatedImage, error) {
	if len(data) > MaxImageBytes {
		return ValidatedImage{}, fmt.Errorf("input image exceeds 32 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ValidatedImage{}, fmt.Errorf("input must be a valid JPEG, PNG or WebP image: %w", err)
	}
	ext := map[string]string{"jpeg": ".jpg", "png": ".png", "webp": ".webp"}[format]
	if ext == "" {
		return ValidatedImage{}, fmt.Errorf("unsupported input image format: %s", format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width) > MaxImagePixels/int64(cfg.Height) {
		return ValidatedImage{}, fmt.Errorf("input image exceeds 40 megapixels")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return ValidatedImage{}, fmt.Errorf("corrupt input image: %w", err)
	}
	return ValidatedImage{Data: data, MIME: "image/" + format, Extension: ext}, nil
}
