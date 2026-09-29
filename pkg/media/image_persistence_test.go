package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func validPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestValidateImageContent(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{validPNG(t), jpg.Bytes()} {
		result, err := ValidateImage(data)
		if err != nil || result.MIME == "" {
			t.Fatalf("valid image: %v", err)
		}
	}
	for _, data := range [][]byte{[]byte("PK\x03\x04animation/animation.json"), []byte(`{"image":true}`), validPNG(t)[:30], make([]byte, MaxImageBytes+1)} {
		if _, err := ValidateImage(data); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
}

func TestWebPAndPixelLimit(t *testing.T) {
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := ValidateImage(webp); err != nil || result.MIME != "image/webp" {
		t.Fatalf("WebP: %v", err)
	}
	oversized := validPNG(t)
	binary.BigEndian.PutUint32(oversized[16:20], 10000)
	binary.BigEndian.PutUint32(oversized[20:24], 10000)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	if _, err := ValidateImage(oversized); err == nil || err.Error() != "input image exceeds 40 megapixels" {
		t.Fatalf("pixel limit: %v", err)
	}
}

func TestPersistentImageRestartScopeExpiryAndLease(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(t.TempDir(), "photo.bin")
	if err := os.WriteFile(original, validPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cfg := MediaCleanerConfig{Enabled: true, MaxAge: 3 * time.Hour, Interval: 5 * time.Minute}
	store := NewFileMediaStoreWithCleanup(cfg)
	store.nowFunc = func() time.Time { return now }
	if err := store.EnablePersistence(dir); err != nil {
		t.Fatal(err)
	}
	ref, err := store.Store(original, MediaMeta{}, "turn")
	if err != nil {
		t.Fatal(err)
	}
	origin := ImageOrigin{Channel: "whatsapp_native", Chat: "a@g.us", Sender: "alice", MessageID: "m1"}
	if err := store.RememberImage(ref, origin); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseAll("turn"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	restored := NewFileMediaStoreWithCleanup(cfg)
	restored.nowFunc = func() time.Time { return now }
	if err := restored.EnablePersistence(dir); err != nil {
		t.Fatal(err)
	}
	records := restored.RecentImages(origin)
	if len(records) != 1 {
		t.Fatal("not restored")
	}
	for _, wrong := range []ImageOrigin{{Channel: origin.Channel, Chat: "b@g.us", Sender: "alice"}, {Channel: origin.Channel, Chat: origin.Chat, Sender: "bob"}} {
		if _, err := restored.AcquireImage(ref, wrong); err == nil {
			t.Fatal("cross scope allowed")
		}
	}
	release, err := restored.AcquireImage(ref, origin)
	if err != nil {
		t.Fatal(err)
	}
	path, err := restored.Resolve(ref)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if restored.CleanExpired() != 0 {
		t.Fatal("deleted in-use image")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.AcquireImage(ref, origin); err == nil {
		t.Fatal("expiry extended on restart")
	}
	release()
	if restored.CleanExpired() != 1 {
		t.Fatal("expired image not deleted")
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal("unowned original deleted")
	}
}

func TestDurableTTLAndQuotedOlderImage(t *testing.T) {
	now := time.Now()
	store := NewFileMediaStoreWithCleanup(MediaCleanerConfig{Enabled: true, MaxAge: 30 * time.Minute})
	store.nowFunc = func() time.Time { return now }
	if err := store.EnablePersistence(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	origin := ImageOrigin{Channel: "test", Chat: "group", Sender: "alice", MessageID: "old"}
	path := filepath.Join(t.TempDir(), "source.png")
	if err := os.WriteFile(path, validPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	var first string
	for i := 0; i < 12; i++ {
		ref, err := store.Store(path, MediaMeta{}, "turn")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = ref
		} else {
			origin.MessageID = "recent"
		}
		if err := store.RememberImage(ref, origin); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	now = now.Add(time.Hour)
	if got := store.ImagesForMessage(origin, "old"); len(got) != 1 || got[0].Ref != first {
		t.Fatalf("quoted image missing: %+v", got)
	}
	if len(store.RecentImages(origin)) != 10 {
		t.Fatal("recent bound changed")
	}
	release, err := store.AcquireImage(first, origin)
	if err != nil {
		t.Fatal("short cleanup ended durable TTL:", err)
	}
	release()
	wrong := origin
	wrong.Chat = "other"
	if len(store.ImagesForMessage(wrong, "old")) != 0 {
		t.Fatal("quoted lookup crossed groups")
	}
}
