package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sipeed/picoclaw/pkg/gamemode/turtlesoup"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/providers"
)

var (
	errTurtleSoupIllustrationUnavailable = errors.New("turtle soup illustration unavailable")
	errTurtleSoupIllustrationUnsafe      = errors.New("turtle soup illustration unsafe")
)

type turtleSoupStartIllustration struct {
	Media []string
	Note  string
}

type turtleSoupStartIllustratorRunner interface {
	IllustrateStart(ctx context.Context, start turtlesoup.StartResult) (turtleSoupStartIllustration, error)
}

type turtleSoupImageGenerator interface {
	Execute(ctx context.Context, args map[string]any) *ToolResult
}

type turtleSoupStartIllustrator struct {
	provider      providers.LLMProvider
	modelResolver func() string
	imageTool     turtleSoupImageGenerator
	mediaStore    func() media.MediaStore
}

func (t *TurtleSoupTool) SetStartIllustrationTool(imageTool *GenerateImageTool) {
	if t == nil || imageTool == nil || t.provider == nil {
		return
	}
	t.startIllustrator = turtleSoupStartIllustrator{
		provider:      t.provider,
		modelResolver: t.currentModel,
		imageTool:     imageTool,
		mediaStore: func() media.MediaStore {
			return imageTool.mediaStore
		},
	}
}

func (t *TurtleSoupTool) setStartIllustrator(illustrator turtleSoupStartIllustratorRunner) {
	if t == nil {
		return
	}
	t.startIllustrator = illustrator
}

func turtleSoupResult(response string, mediaRefs []string) *ToolResult {
	result := SilentResult(response)
	if len(mediaRefs) > 0 {
		result.Media = append([]string(nil), mediaRefs...)
	}
	return result
}

func turtleSoupIllustrationFailureNote(err error) string {
	if errors.Is(err, errTurtleSoupIllustrationUnsafe) {
		return "插圖未通過安全檢查，先用文字開湯。"
	}
	return "插圖暫時生成唔到，先用文字開湯。"
}

func (i turtleSoupStartIllustrator) IllustrateStart(
	ctx context.Context,
	start turtlesoup.StartResult,
) (turtleSoupStartIllustration, error) {
	if i.provider == nil || i.imageTool == nil {
		return turtleSoupStartIllustration{}, errTurtleSoupIllustrationUnavailable
	}
	if strings.TrimSpace(start.Surface) == "" || strings.TrimSpace(start.Solution) == "" {
		return turtleSoupStartIllustration{}, errTurtleSoupIllustrationUnavailable
	}

	prompt := buildTurtleSoupStartImagePrompt(start)
	if promptContainsHiddenSolution(prompt, start.Solution) {
		return turtleSoupStartIllustration{}, errTurtleSoupIllustrationUnsafe
	}
	if err := i.reviewPrompt(ctx, start, prompt); err != nil {
		return turtleSoupStartIllustration{}, err
	}

	imageResult := i.imageTool.Execute(ctx, map[string]any{
		"prompt":       prompt,
		"aspect_ratio": "4:3",
	})
	if imageResult == nil || imageResult.IsError || len(imageResult.Media) == 0 {
		return turtleSoupStartIllustration{}, errTurtleSoupIllustrationUnavailable
	}

	store := i.resolveMediaStore()
	if store == nil {
		i.discardMedia(imageResult.Media)
		return turtleSoupStartIllustration{}, errTurtleSoupIllustrationUnavailable
	}
	for _, ref := range imageResult.Media {
		if err := i.reviewGeneratedImage(ctx, start, prompt, store, ref); err != nil {
			i.discardMedia(imageResult.Media)
			return turtleSoupStartIllustration{}, err
		}
	}

	return turtleSoupStartIllustration{Media: append([]string(nil), imageResult.Media...)}, nil
}

func buildTurtleSoupStartImagePrompt(start turtlesoup.StartResult) string {
	var lines []string
	lines = append(lines,
		"Create one atmospheric illustration for a turtle soup / lateral-thinking mystery game.",
		"Base the image only on this public setup:",
		strings.TrimSpace(start.Surface),
	)
	if difficulty := strings.TrimSpace(start.Difficulty); difficulty != "" {
		lines = append(lines, "Public difficulty/style note: "+difficulty)
	}
	if len(start.Themes) > 0 {
		lines = append(lines, "Public theme tags: "+strings.Join(start.Themes, ", "))
	}
	lines = append(lines,
		"Make it an ambiguous mood-setting scene, not a solution diagram.",
		"Do not include readable text, labels, arrows, clue boards, hidden causes, hidden identities, or extra objects that explain the mystery.",
		"Depict only public elements implied by the setup; leave the true explanation visually unrevealed.",
		"Cinematic storybook illustration, 4:3 composition.",
	)
	return strings.Join(lines, "\n")
}

func (i turtleSoupStartIllustrator) reviewPrompt(
	ctx context.Context,
	start turtlesoup.StartResult,
	prompt string,
) error {
	payload := map[string]any{
		"public_surface":        start.Surface,
		"hidden_solution":       start.Solution,
		"image_prompt_to_check": prompt,
	}
	return i.review(ctx, turtleSoupPromptReviewSystemPrompt, payload, nil)
}

func (i turtleSoupStartIllustrator) reviewGeneratedImage(
	ctx context.Context,
	start turtlesoup.StartResult,
	prompt string,
	store media.MediaStore,
	ref string,
) error {
	localPath, _, err := store.ResolveWithMeta(ref)
	if err != nil {
		return fmt.Errorf("%w: resolve generated image", errTurtleSoupIllustrationUnavailable)
	}
	dataURL, err := encodeImageAsDataURL(localPath)
	if err != nil {
		return fmt.Errorf("%w: encode generated image", errTurtleSoupIllustrationUnavailable)
	}
	payload := map[string]any{
		"public_surface":        start.Surface,
		"hidden_solution":       start.Solution,
		"image_prompt_checked":  prompt,
		"review_instruction":    "Decide whether the attached image reveals facts behind the puzzle before players discover them.",
		"safe_image_definition": "Safe means the image is only an ambiguous illustration of the public setup and does not visually answer the hidden solution.",
	}
	return i.review(ctx, turtleSoupImageReviewSystemPrompt, payload, []string{dataURL})
}

const turtleSoupPromptReviewSystemPrompt = `You are a safety reviewer for a turtle soup game illustration prompt.
The image will be shown to players before they solve the mystery.
Return JSON only: {"safe":true} or {"safe":false}.
Mark unsafe if the prompt reveals, confirms, or strongly hints at the hidden solution, hidden cause, hidden identity, hidden location, hidden object, or any fact not already public.
Mark safe only if the prompt stays atmospheric and uses only the public setup plus broad visual style.`

const turtleSoupImageReviewSystemPrompt = `You are a safety reviewer for a turtle soup game illustration.
The attached image would be shown to players before they solve the mystery.
Return JSON only: {"safe":true} or {"safe":false}.
If you cannot directly inspect the attached image, return {"safe":false}.
Mark unsafe if the image reveals, confirms, or strongly hints at the hidden solution, hidden cause, hidden identity, hidden location, hidden object, or any fact not already public.
Mark unsafe if the image visually answers why the public mystery happened.
Mark safe only if the image is an ambiguous mood illustration of the public setup.`

func (i turtleSoupStartIllustrator) review(
	ctx context.Context,
	system string,
	payload map[string]any,
	mediaRefs []string,
) error {
	if i.provider == nil {
		return errTurtleSoupIllustrationUnavailable
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%w: encode review payload", errTurtleSoupIllustrationUnavailable)
	}
	messages := []providers.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: string(payloadBytes), Media: append([]string(nil), mediaRefs...)},
	}
	resp, err := i.provider.Chat(ctx, messages, nil, i.currentModel(), map[string]any{
		"max_tokens":  80,
		"temperature": 0,
	})
	if err != nil || resp == nil {
		return fmt.Errorf("%w: review failed", errTurtleSoupIllustrationUnavailable)
	}
	safe, err := parseTurtleSoupIllustrationReview(resp.Content)
	if err != nil {
		return err
	}
	if !safe {
		return errTurtleSoupIllustrationUnsafe
	}
	return nil
}

func parseTurtleSoupIllustrationReview(raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, errTurtleSoupIllustrationUnavailable
	}
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= start {
			raw = raw[start : end+1]
		}
	}
	var verdict struct {
		Safe *bool `json:"safe"`
	}
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil || verdict.Safe == nil {
		return false, errTurtleSoupIllustrationUnavailable
	}
	return *verdict.Safe, nil
}

func (i turtleSoupStartIllustrator) currentModel() string {
	if i.modelResolver == nil {
		return ""
	}
	return strings.TrimSpace(i.modelResolver())
}

func (i turtleSoupStartIllustrator) resolveMediaStore() media.MediaStore {
	if i.mediaStore == nil {
		return nil
	}
	return i.mediaStore()
}

func (i turtleSoupStartIllustrator) discardMedia(refs []string) {
	store := i.resolveMediaStore()
	if store == nil {
		return
	}
	for _, ref := range refs {
		localPath, meta, err := store.ResolveWithMeta(ref)
		if err != nil || !meta.Owned {
			continue
		}
		_ = os.Remove(localPath)
	}
}

func promptContainsHiddenSolution(prompt, solution string) bool {
	prompt = normalizeIllustrationLeakText(prompt)
	solution = normalizeIllustrationLeakText(solution)
	return solution != "" && strings.Contains(prompt, solution)
}

func normalizeIllustrationLeakText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
