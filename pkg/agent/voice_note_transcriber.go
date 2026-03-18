package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
)

const voiceNoteTranscriptionTimeout = 2 * time.Minute

type voiceNoteTranscriber interface {
	Transcribe(ctx context.Context, workspace, audioPath string) (string, error)
}

type sttSkillVoiceNoteTranscriber struct{}

func (t *sttSkillVoiceNoteTranscriber) Transcribe(ctx context.Context, workspace, audioPath string) (string, error) {
	scriptPath, err := resolveVoiceNoteSTTHelperScript(workspace)
	if err != nil {
		return "", err
	}

	pythonCmd, pythonArgs, err := resolveVoiceNotePythonCommand()
	if err != nil {
		return "", err
	}

	args := make([]string, 0, len(pythonArgs)+2)
	args = append(args, pythonArgs...)
	args = append(args, scriptPath, audioPath)

	cmd := exec.CommandContext(ctx, pythonCmd, args...)
	if strings.TrimSpace(workspace) != "" {
		cmd.Dir = workspace
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("run stt helper: %s", detail)
	}

	transcript := strings.TrimSpace(stdout.String())
	if transcript == "" {
		return "", fmt.Errorf("stt helper returned empty transcript")
	}

	return transcript, nil
}

func (al *AgentLoop) prepareVoiceNoteMessage(
	ctx context.Context,
	agent *AgentInstance,
	msg bus.InboundMessage,
) bus.InboundMessage {
	if agent == nil || al.voiceNoteTranscriber == nil {
		return msg
	}
	if strings.TrimSpace(msg.Metadata[bus.MetadataMessageSubtype]) != bus.MessageSubtypeVoiceNote {
		return msg
	}

	audioIndex, audioPath, found := al.findFirstAudioMedia(msg.Media)
	if !found {
		return msg
	}

	transcribeCtx, cancel := context.WithTimeout(ctx, voiceNoteTranscriptionTimeout)
	defer cancel()

	transcript, err := al.voiceNoteTranscriber.Transcribe(transcribeCtx, agent.Workspace, audioPath)
	if err != nil {
		logger.WarnCF("agent", "Voice note transcription failed; falling back to attachment handling", map[string]any{
			"channel": msg.Channel,
			"chat_id": msg.ChatID,
			"error":   err.Error(),
		})
		return msg
	}

	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		logger.WarnCF("agent", "Voice note transcription returned empty transcript; falling back to attachment handling", map[string]any{
			"channel": msg.Channel,
			"chat_id": msg.ChatID,
		})
		return msg
	}

	prepared := msg
	prepared.Content = mergeVoiceNoteTranscript(msg.Content, transcript)
	prepared.Media = removeMediaAtIndex(msg.Media, audioIndex)
	return prepared
}

func (al *AgentLoop) findFirstAudioMedia(mediaRefs []string) (int, string, bool) {
	for i, ref := range mediaRefs {
		if strings.HasPrefix(ref, "media://") {
			if al.mediaStore == nil {
				continue
			}
			localPath, meta, err := al.mediaStore.ResolveWithMeta(ref)
			if err != nil {
				logger.WarnCF("agent", "Failed to resolve voice note media", map[string]any{
					"ref":   ref,
					"error": err.Error(),
				})
				continue
			}
			if inferMediaType(meta.Filename, meta.ContentType) == "audio" {
				return i, localPath, true
			}
			continue
		}

		if inferMediaType(ref, "") == "audio" {
			return i, ref, true
		}
	}

	return -1, "", false
}

func mergeVoiceNoteTranscript(content, transcript string) string {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return strings.TrimSpace(content)
	}

	companion := stripVoiceNotePlaceholders(content)
	if companion == "" {
		return transcript
	}
	if strings.EqualFold(companion, transcript) {
		return transcript
	}

	return transcript + "\n\n[Accompanying text from the same message]\n" + companion
}

func stripVoiceNotePlaceholders(content string) string {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isVoiceNotePlaceholderLine(trimmed) {
			continue
		}
		if trimmed == "" {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func isVoiceNotePlaceholderLine(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "[voice]", "[audio]", "[voice note]", "[voicenote]":
		return true
	default:
		return false
	}
}

func removeMediaAtIndex(mediaRefs []string, idx int) []string {
	if idx < 0 || idx >= len(mediaRefs) {
		return mediaRefs
	}

	result := make([]string, 0, len(mediaRefs)-1)
	result = append(result, mediaRefs[:idx]...)
	result = append(result, mediaRefs[idx+1:]...)
	return result
}

func resolveVoiceNoteSTTHelperScript(workspace string) (string, error) {
	candidates := make([]string, 0, 3)
	if strings.TrimSpace(workspace) != "" {
		candidates = append(candidates, filepath.Join(workspace, "skills", "stt", "scripts", "transcribe_audio.py"))
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "workspace", "skills", "stt", "scripts", "transcribe_audio.py"))
		candidates = append(candidates, filepath.Join(wd, "cmd", "picoclaw", "internal", "onboard", "workspace", "skills", "stt", "scripts", "transcribe_audio.py"))
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("stt helper script not found")
}

func resolveVoiceNotePythonCommand() (string, []string, error) {
	if path, err := exec.LookPath("python3"); err == nil {
		return path, nil, nil
	}
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath("py"); err == nil {
			return path, []string{"-3"}, nil
		}
	}
	if path, err := exec.LookPath("python"); err == nil {
		return path, nil, nil
	}
	return "", nil, fmt.Errorf("python runtime not found for stt helper")
}
