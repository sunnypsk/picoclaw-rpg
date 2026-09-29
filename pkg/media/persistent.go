package media

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

type ImageOrigin struct {
	Channel   string `json:"channel"`
	Chat      string `json:"chat"`
	Sender    string `json:"sender"`
	MessageID string `json:"message_id"`
}

type SavedImage struct {
	Ref      string      `json:"ref"`
	File     string      `json:"file"`
	Origin   ImageOrigin `json:"origin"`
	MIME     string      `json:"mime"`
	Received time.Time   `json:"received"`
	Expires  time.Time   `json:"expires"`
}

// EnablePersistence restores only managed, unexpired image copies. Call before Start.
func (s *FileMediaStore) EnablePersistence(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	s.persistentDir = dir
	s.saved = map[string]SavedImage{}
	s.inUse = map[string]int{}
	data, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var records []SavedImage
	if err = json.Unmarshal(data, &records); err != nil {
		return err
	}
	for _, r := range records {
		if !strings.HasPrefix(r.Ref, "media://") || filepath.Base(r.File) != r.File || !strings.HasPrefix(r.File, "image-") {
			return fmt.Errorf("invalid managed image index")
		}
		path := filepath.Join(dir, r.File)
		if !s.nowFunc().Before(r.Expires) {
			_ = os.Remove(path)
			continue
		}
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		s.saved[r.Ref] = r
		s.refs[r.Ref] = mediaEntry{path: path, meta: MediaMeta{Filename: r.File, ContentType: r.MIME, Source: r.Origin.Channel, Owned: true, Origin: &r.Origin}, storedAt: r.Received}
	}
	return s.saveIndexLocked()
}

func (s *FileMediaStore) saveIndexLocked() error {
	if s.persistentDir == "" {
		return nil
	}
	records := make([]SavedImage, 0, len(s.saved))
	for _, r := range s.saved {
		records = append(records, r)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Ref < records[j].Ref })
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(filepath.Join(s.persistentDir, "index.json"), data, 0600)
}

func (s *FileMediaStore) RememberImage(ref string, origin ImageOrigin) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persistentDir == "" {
		return nil
	}
	if old, ok := s.saved[ref]; ok {
		if old.Origin != origin {
			return fmt.Errorf("image origin mismatch")
		}
		return nil
	}
	entry, ok := s.refs[ref]
	if !ok {
		return fmt.Errorf("unknown image reference")
	}
	input, err := ReadImage(entry.path)
	if err != nil {
		return err
	}
	name := "image-" + strings.TrimPrefix(ref, "media://") + input.Extension
	if filepath.Base(name) != name {
		return fmt.Errorf("invalid image reference")
	}
	path := filepath.Join(s.persistentDir, name)
	if err := fileutil.WriteFileAtomic(path, input.Data, 0600); err != nil {
		return err
	}
	r := SavedImage{Ref: ref, File: name, Origin: origin, MIME: input.MIME, Received: entry.storedAt, Expires: entry.storedAt.Add(3 * time.Hour)}
	s.saved[ref] = r
	if err := s.saveIndexLocked(); err != nil {
		delete(s.saved, ref)
		_ = os.Remove(path)
		return err
	}
	oldPath := entry.path
	oldOwned := entry.meta.Owned
	entry.meta.Owned = true
	entry.path = path
	entry.meta.Origin = &origin
	entry.meta.ContentType = input.MIME
	entry.meta.Filename = name
	s.refs[ref] = entry
	if oldOwned && oldPath != path {
		_ = os.Remove(oldPath)
	}
	return nil
}

func (s *FileMediaStore) RecentImages(origin ImageOrigin) []SavedImage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []SavedImage
	for _, r := range s.saved {
		if r.Origin.Channel == origin.Channel && r.Origin.Chat == origin.Chat && r.Origin.Sender == origin.Sender && s.nowFunc().Before(r.Expires) {
			result = append(result, r)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Received.After(result[j].Received) })
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}

// AcquireImage pins a scoped reference until release. Expiry never extends on use.
func (s *FileMediaStore) AcquireImage(ref string, origin ImageOrigin) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.saved[ref]; ok {
		if r.Origin.Channel != origin.Channel || r.Origin.Chat != origin.Chat || r.Origin.Sender != origin.Sender {
			return nil, fmt.Errorf("image belongs to another conversation or sender")
		}
		if !s.nowFunc().Before(r.Expires) {
			return nil, fmt.Errorf("image expired; please resend it")
		}
	}
	entry, ok := s.refs[ref]
	if !ok {
		return nil, fmt.Errorf("image unavailable; please resend it")
	}
	if entry.meta.Origin != nil {
		owner := entry.meta.Origin
		if owner.Channel != origin.Channel || owner.Chat != origin.Chat || owner.Sender != origin.Sender {
			return nil, fmt.Errorf("image belongs to another conversation or sender")
		}
	}
	if _, durable := s.saved[ref]; !durable && s.cleanerCfg.MaxAge > 0 && !s.nowFunc().Before(entry.storedAt.Add(s.cleanerCfg.MaxAge)) {
		return nil, fmt.Errorf("image expired; please resend it")
	}
	if info, err := os.Lstat(entry.path); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("image file unavailable")
	}
	if s.inUse == nil {
		s.inUse = map[string]int{}
	}
	s.inUse[ref]++
	var once sync.Once
	return func() { once.Do(func() { s.mu.Lock(); defer s.mu.Unlock(); s.inUse[ref]-- }) }, nil
}

// IsManagedPath prevents filesystem paths from bypassing scoped media references.
func (s *FileMediaStore) IsManagedPath(path string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.persistentDir == "" {
		return false
	}
	root, err := filepath.Abs(s.persistentDir)
	if err != nil {
		return true
	}
	candidate, err := filepath.Abs(path)
	if err != nil {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
		candidate = resolved
	}
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func (s *FileMediaStore) ImagesForMessage(origin ImageOrigin, messageID string) []SavedImage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []SavedImage
	if messageID == "" {
		return result
	}
	for _, r := range s.saved {
		if r.Origin.Channel == origin.Channel && r.Origin.Chat == origin.Chat && r.Origin.Sender == origin.Sender && r.Origin.MessageID == messageID && s.nowFunc().Before(r.Expires) {
			result = append(result, r)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Ref < result[j].Ref })
	return result
}
