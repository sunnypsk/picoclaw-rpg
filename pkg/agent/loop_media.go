// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/h2non/filetype"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/utils"
)

const inboundAttachmentStageDir = ".picoclaw/inbound_media"

type currentImagePromptRef struct {
	ref      string
	filename string
}

func normalizeInboundPromptMedia(
	userMessage string,
	sessionUserMessage string,
	workspace string,
	mediaRefs []string,
	store media.MediaStore,
) (string, string, []string) {
	if len(mediaRefs) == 0 || strings.TrimSpace(workspace) == "" || store == nil {
		return userMessage, sessionUserMessage, mediaRefs
	}

	keptMedia := make([]string, 0, len(mediaRefs))
	promptNotes := make([]string, 0, len(mediaRefs))
	sessionNotes := make([]string, 0, len(mediaRefs))
	currentImages := make([]currentImagePromptRef, 0, len(mediaRefs))
	seenImageRefs := make(map[string]struct{}, len(mediaRefs))

	for _, ref := range mediaRefs {
		if !strings.HasPrefix(ref, "media://") {
			keptMedia = append(keptMedia, ref)
			continue
		}

		localPath, meta, err := store.ResolveWithMeta(ref)
		if err != nil {
			logger.WarnCF("agent", "Failed to resolve inbound media ref for prompt preparation", map[string]any{
				"ref":   ref,
				"error": err.Error(),
			})
			continue
		}

		mediaType := utils.InferMediaType(meta.Filename, meta.ContentType)
		switch mediaType {
		case "image":
			keptMedia = append(keptMedia, ref)
			if _, seen := seenImageRefs[ref]; !seen {
				currentImages = append(currentImages, currentImagePromptRef{
					ref:      ref,
					filename: attachmentDisplayFilename(meta, localPath, "image"),
				})
				seenImageRefs[ref] = struct{}{}
			}
		case "audio", "video", "file":
			stagedPath, stageErr := stageInboundAttachment(localPath, meta, workspace, mediaType)
			if stageErr != nil {
				logger.WarnCF("agent", "Failed to stage inbound attachment", map[string]any{
					"path":  localPath,
					"kind":  mediaType,
					"error": stageErr.Error(),
				})
				keptMedia = append(keptMedia, ref)
				promptNotes = append(promptNotes, buildAttachmentPreparationFailureNote(meta, mediaType))
				sessionNotes = append(sessionNotes, buildAttachmentSessionNote(meta, mediaType, false))
				continue
			}

			promptNotes = append(promptNotes, buildAttachmentPromptNote(stagedPath, meta, mediaType))
			sessionNotes = append(sessionNotes, buildAttachmentSessionNote(meta, mediaType, true))
		default:
			// Non-image attachments are not serialized into OpenAI-compatible prompts.
		}
	}

	if len(currentImages) > 0 {
		promptNotes = append(promptNotes, buildCurrentImagePromptNote(currentImages))
	}

	return appendPromptSections(userMessage, promptNotes),
		appendPromptSections(sessionUserMessage, sessionNotes),
		keptMedia
}

func appendPromptSections(base string, sections []string) string {
	if len(sections) == 0 {
		return base
	}

	joined := strings.Join(sections, "\n\n")
	if strings.TrimSpace(base) == "" {
		return joined
	}

	return strings.TrimSpace(base) + "\n\n" + joined
}

func stageInboundAttachment(localPath string, meta media.MediaMeta, workspace, mediaType string) (string, error) {
	stageDir := filepath.Join(workspace, inboundAttachmentStageDir)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return "", fmt.Errorf("create inbound attachment staging dir: %w", err)
	}

	pattern := mediaType + "-*"
	if ext := inboundAttachmentExtension(localPath, meta); ext != "" {
		pattern += ext
	}

	dst, err := os.CreateTemp(stageDir, pattern)
	if err != nil {
		return "", fmt.Errorf("create staged attachment file: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = os.Remove(dst.Name())
		}
	}()

	src, err := os.Open(localPath)
	if err != nil {
		_ = dst.Close()
		return "", fmt.Errorf("open source attachment file: %w", err)
	}
	defer src.Close()

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return "", fmt.Errorf("copy attachment: %w", err)
	}

	if err := dst.Close(); err != nil {
		return "", fmt.Errorf("flush staged attachment file: %w", err)
	}

	success = true
	return dst.Name(), nil
}

func inboundAttachmentExtension(localPath string, meta media.MediaMeta) string {
	if ext := normalizedInboundExtension(strings.TrimSpace(meta.Filename)); ext != "" {
		return ext
	}
	if ext := normalizedInboundExtension(strings.TrimSpace(localPath)); ext != "" {
		return ext
	}
	if ext := utils.PreferredExtensionForContentType(meta.ContentType); ext != "" {
		return ext
	}
	if ext := detectInboundAttachmentExtension(localPath); ext != "" {
		return ext
	}
	return ".bin"
}

func normalizedInboundExtension(pathOrFilename string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(pathOrFilename)))
	if ext == "" || ext == ".bin" {
		return ""
	}
	return ext
}

func detectInboundAttachmentExtension(localPath string) string {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return ""
	}
	return utils.PreferredExtensionForBytes(data)
}

func buildAttachmentPromptNote(stagedPath string, meta media.MediaMeta, mediaType string) string {
	filename := attachmentDisplayFilename(meta, stagedPath, mediaType+" attachment")

	if mediaType == "audio" {
		return fmt.Sprintf(
			"[Audio attachment available]\nLocal file: %s\nOriginal filename: %s\nIf the user's request depends on the spoken content, you may read skills/stt/SKILL.md and use the stt skill to transcribe this file. Keep any caption or text in this message as the primary intent signal.",
			stagedPath,
			filename,
		)
	}

	instructions := "This file is available for this turn if the user's request depends on it. Use relevant tools or skills only when needed."
	if skillHint := attachmentSkillHint(meta, mediaType); skillHint != "" {
		instructions = skillHint
	}

	return fmt.Sprintf(
		"[%s attachment available]\nLocal file: %s\nOriginal filename: %s\n%s Keep any caption or text in this message as the primary intent signal.",
		attachmentLabel(mediaType),
		stagedPath,
		filename,
		instructions,
	)
}

func buildAttachmentPreparationFailureNote(meta media.MediaMeta, mediaType string) string {
	filename := attachmentDisplayFilename(meta, "", mediaType+" attachment")

	if mediaType == "audio" {
		return fmt.Sprintf(
			"[Audio attachment received]\nThe file %q could not be prepared for transcription in this turn. Continue using the available caption or text instead of failing the request.",
			filename,
		)
	}

	return fmt.Sprintf(
		"[%s attachment received]\nThe file %q could not be prepared for this turn. Continue using the available caption or text instead of failing the request.",
		attachmentLabel(mediaType),
		filename,
	)
}

func buildAttachmentSessionNote(meta media.MediaMeta, mediaType string, prepared bool) string {
	filename := attachmentDisplayFilename(meta, "", mediaType+" attachment")

	if prepared {
		return fmt.Sprintf("[%s attachment available for this turn: %s]", attachmentLabel(mediaType), filename)
	}

	return fmt.Sprintf("[%s attachment could not be prepared for this turn: %s]", attachmentLabel(mediaType), filename)
}

func attachmentLabel(mediaType string) string {
	switch mediaType {
	case "audio":
		return "Audio"
	case "video":
		return "Video"
	default:
		return "File"
	}
}

func attachmentDisplayFilename(meta media.MediaMeta, localPath, fallback string) string {
	filename := strings.TrimSpace(meta.Filename)
	if filename != "" {
		return filename
	}
	if trimmed := strings.TrimSpace(localPath); trimmed != "" {
		if base := filepath.Base(trimmed); strings.TrimSpace(base) != "" && base != "." {
			return base
		}
	}
	return fallback
}

func buildCurrentImagePromptNote(images []currentImagePromptRef) string {
	lines := []string{
		"[Image attachments available for this turn]",
		"Use one exact media ref when you need to edit an uploaded image with generate_image:",
	}
	for _, image := range images {
		lines = append(lines, fmt.Sprintf("- %s => %s", image.filename, image.ref))
	}
	lines = append(lines,
		"Pass exactly one of these refs via image, input_image, or input_images.",
		"If exactly one current image is listed, media://current is also supported. These refs are valid only for this turn.",
	)
	return strings.Join(lines, "\n")
}

func currentTurnImageMediaRefs(mediaRefs []string, store media.MediaStore) []string {
	if len(mediaRefs) == 0 || store == nil {
		return nil
	}

	refs := make([]string, 0, len(mediaRefs))
	seen := make(map[string]struct{}, len(mediaRefs))
	for _, ref := range mediaRefs {
		ref = strings.TrimSpace(ref)
		if !strings.HasPrefix(ref, "media://") {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		_, meta, err := store.ResolveWithMeta(ref)
		if err != nil {
			continue
		}
		if utils.InferMediaType(meta.Filename, meta.ContentType) != "image" {
			continue
		}
		refs = append(refs, ref)
		seen[ref] = struct{}{}
	}
	return refs
}

func attachmentSkillHint(meta media.MediaMeta, mediaType string) string {
	if mediaType != "file" {
		return ""
	}

	ext := normalizedInboundExtension(strings.TrimSpace(meta.Filename))
	if ext == "" {
		ext = utils.PreferredExtensionForContentType(meta.ContentType)
	}

	switch ext {
	case ".pdf":
		return "If the user needs the document contents, you may read skills/pdf-parse/SKILL.md and use the pdf-parse skill to extract the text locally."
	case ".txt", ".md", ".csv", ".tsv", ".docx", ".pptx", ".xlsx":
		return "If the user needs the document contents, you may read skills/office-parse/SKILL.md and use the office-parse skill to extract the contents locally."
	default:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(meta.ContentType)), "text/") {
			return "If the user needs the document contents, you may read skills/office-parse/SKILL.md and use the office-parse skill to extract the contents locally."
		}
		return ""
	}
}

// resolveMediaRefs replaces media:// refs in message Media fields with base64 data URLs.
// Uses streaming base64 encoding (file handle -> encoder -> buffer) to avoid holding
// both raw bytes and encoded string in memory simultaneously.
// Returns a new slice; original messages are not mutated.
func resolveMediaRefs(messages []providers.Message, store media.MediaStore, maxSize int) []providers.Message {
	if store == nil {
		return messages
	}

	result := make([]providers.Message, len(messages))
	copy(result, messages)

	for i, m := range result {
		if len(m.Media) == 0 {
			continue
		}

		resolved := make([]string, 0, len(m.Media))
		for _, ref := range m.Media {
			if !strings.HasPrefix(ref, "media://") {
				resolved = append(resolved, ref)
				continue
			}

			localPath, meta, err := store.ResolveWithMeta(ref)
			if err != nil {
				logger.WarnCF("agent", "Failed to resolve media ref", map[string]any{
					"ref":   ref,
					"error": err.Error(),
				})
				continue
			}

			info, err := os.Stat(localPath)
			if err != nil {
				logger.WarnCF("agent", "Failed to stat media file", map[string]any{
					"path":  localPath,
					"error": err.Error(),
				})
				continue
			}
			if info.Size() > int64(maxSize) {
				logger.WarnCF("agent", "Media file too large, skipping", map[string]any{
					"path":     localPath,
					"size":     info.Size(),
					"max_size": maxSize,
				})
				continue
			}

			mime := strings.TrimSpace(meta.ContentType)
			if mime == "" || isGenericBinaryMIME(mime) {
				kind, ftErr := filetype.MatchFile(localPath)
				if ftErr != nil || kind == filetype.Unknown {
					if mime == "" {
						logger.WarnCF("agent", "Unknown media type, skipping", map[string]any{
							"path": localPath,
						})
						continue
					}
				} else {
					mime = kind.MIME.Value
				}
			}

			f, err := os.Open(localPath)
			if err != nil {
				logger.WarnCF("agent", "Failed to open media file", map[string]any{
					"path":  localPath,
					"error": err.Error(),
				})
				continue
			}

			prefix := "data:" + mime + ";base64,"
			encodedLen := base64.StdEncoding.EncodedLen(int(info.Size()))
			var buf bytes.Buffer
			buf.Grow(len(prefix) + encodedLen)
			buf.WriteString(prefix)

			encoder := base64.NewEncoder(base64.StdEncoding, &buf)
			if _, err := io.Copy(encoder, f); err != nil {
				_ = f.Close()
				logger.WarnCF("agent", "Failed to encode media file", map[string]any{
					"path":  localPath,
					"error": err.Error(),
				})
				continue
			}
			_ = encoder.Close()
			_ = f.Close()

			resolved = append(resolved, buf.String())
		}

		result[i].Media = resolved
	}

	return result
}

func isGenericBinaryMIME(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if i := strings.Index(value, ";"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	return value == "application/octet-stream"
}
