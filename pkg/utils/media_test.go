package utils

import "testing"

func TestPreferredExtensionForContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        string
	}{
		{name: "pdf", contentType: "application/pdf", want: ".pdf"},
		{name: "audio mpeg", contentType: "audio/mpeg", want: ".mp3"},
		{name: "audio ogg params", contentType: "audio/ogg; codecs=opus", want: ".ogg"},
		{name: "empty", contentType: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := PreferredExtensionForContentType(tc.contentType); got != tc.want {
				t.Fatalf("PreferredExtensionForContentType(%q) = %q, want %q", tc.contentType, got, tc.want)
			}
		})
	}
}

func TestPreferredExtensionForBytes(t *testing.T) {
	pngHeader := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02,
		0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE,
	}

	if got := PreferredExtensionForBytes(pngHeader); got != ".png" {
		t.Fatalf("PreferredExtensionForBytes(png) = %q, want .png", got)
	}
	if got := PreferredExtensionForBytes([]byte("not-media")); got != "" {
		t.Fatalf("PreferredExtensionForBytes(unknown) = %q, want empty", got)
	}
}

func TestInferMediaType(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		contentType string
		want        string
	}{
		{name: "image by content type", filename: "x.bin", contentType: "image/png", want: "image"},
		{name: "audio by content type", filename: "x.bin", contentType: "audio/ogg; codecs=opus", want: "audio"},
		{name: "video by extension", filename: "clip.mp4", want: "video"},
		{name: "file fallback", filename: "report.pdf", want: "file"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := InferMediaType(tc.filename, tc.contentType); got != tc.want {
				t.Fatalf("InferMediaType(%q, %q) = %q, want %q", tc.filename, tc.contentType, got, tc.want)
			}
		})
	}
}
