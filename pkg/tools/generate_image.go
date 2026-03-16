package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/utils"
)

const defaultImageTimeout = 300 * time.Second

type GenerateImageTool struct {
	workspace  string
	mediaStore media.MediaStore
	httpClient *http.Client
	getenv     func(string) string
	tempDir    func() string
}

func NewGenerateImageTool(workspace string) *GenerateImageTool {
	return &GenerateImageTool{
		workspace: workspace,
		httpClient: &http.Client{
			Timeout: defaultImageTimeout,
		},
		getenv:  os.Getenv,
		tempDir: os.TempDir,
	}
}

func (t *GenerateImageTool) SetMediaStore(store media.MediaStore) {
	t.mediaStore = store
}

func (t *GenerateImageTool) Name() string {
	return "generate_image"
}

func (t *GenerateImageTool) Description() string {
	return "Generate or edit an image through CPA chat completions and return it as chat media."
}

func (t *GenerateImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Instruction describing the image to generate or edit.",
			},
			"image": map[string]any{
				"type":        "string",
				"description": "Optional local file path or media:// ref for one input image.",
			},
			"input_image": map[string]any{
				"type":        "string",
				"description": "Optional alias for image.",
			},
			"input_images": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
				"description": "Optional explicit list of input images. Only one is supported safely.",
			},
			"size": map[string]any{
				"type":        "string",
				"description": "Optional output resolution such as 1024x1024 or 1536x1024, passed through to CPA.",
			},
			"aspect_ratio": map[string]any{
				"type":        "string",
				"description": "Optional aspect ratio such as 1:1, 4:3, 3:4, 16:9, or 9:16, passed through to CPA when supported.",
			},
			"quality": map[string]any{
				"type":        "string",
				"description": "Optional quality passed through to CPA.",
			},
			"style": map[string]any{
				"type":        "string",
				"description": "Optional style passed through to CPA.",
			},
			"background": map[string]any{
				"type":        "string",
				"description": "Optional background passed through to CPA.",
			},
			"timeout_seconds": map[string]any{
				"type":        "number",
				"description": "Optional timeout in seconds. Defaults to 300.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *GenerateImageTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	prompt := strings.TrimSpace(imageStringArg(args, "prompt"))
	if prompt == "" {
		return ErrorResult("prompt is required")
	}
	if t.mediaStore == nil {
		return ErrorResult("image generation is not configured with a media store")
	}

	apiKey := strings.TrimSpace(t.getenv("CPA_API_KEY"))
	if apiKey == "" {
		return ErrorResult("CPA_API_KEY is required")
	}
	apiBase := strings.TrimSpace(t.getenv("CPA_API_BASE"))
	if apiBase == "" {
		return ErrorResult("CPA_API_BASE is required")
	}
	model := strings.TrimSpace(t.getenv("CPA_IMAGE_MODEL"))
	if model == "" {
		return ErrorResult("CPA_IMAGE_MODEL is required")
	}

	timeout := durationArg(args, "timeout_seconds", defaultImageTimeout)
	client := *t.httpClient
	client.Timeout = timeout

	resolvedInputs, err := t.resolveInputImages(args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	payload, err := buildImagePayload(model, prompt, resolvedInputs, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	responseData, err := t.sendCPARequest(ctx, &client, strings.TrimRight(apiBase, "/"), apiKey, payload)
	if err != nil {
		return ErrorResult(err.Error())
	}

	outputs, err := t.collectOutputs(responseData)
	if err != nil {
		return ErrorResult(err.Error())
	}

	refs := make([]string, 0, len(outputs))
	for _, output := range outputs {
		ref, err := t.storeOutput(ctx, output)
		if err != nil {
			return ErrorResult(err.Error())
		}
		refs = append(refs, ref)
	}

	summary := fmt.Sprintf("Generated %d image(s) with model %s.", len(refs), model)
	return MediaResult(summary, refs)
}

type imageOutput struct {
	filename    string
	contentType string
	localPath   string
	data        []byte
	remoteURL   string
}

func (t *GenerateImageTool) resolveInputImages(args map[string]any) ([]string, error) {
	var candidates []string
	for _, key := range []string{"image", "input_image"} {
		if value := strings.TrimSpace(imageStringArg(args, key)); value != "" {
			candidates = append(candidates, value)
		}
	}
	if raw, ok := args["input_images"]; ok {
		list, err := stringSlice(raw)
		if err != nil {
			return nil, fmt.Errorf("input_images must be an array of strings")
		}
		for _, item := range list {
			item = strings.TrimSpace(item)
			if item != "" {
				candidates = append(candidates, item)
			}
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) > 1 {
		return nil, fmt.Errorf("multiple input images provided; pass only one explicit input image")
	}

	resolved, err := t.resolveInputRef(candidates[0])
	if err != nil {
		return nil, err
	}
	return []string{resolved}, nil
}

func (t *GenerateImageTool) resolveInputRef(value string) (string, error) {
	if strings.HasPrefix(value, "media://") {
		path, _, err := t.mediaStore.ResolveWithMeta(value)
		if err != nil {
			return "", fmt.Errorf("resolve input media %q: %w", value, err)
		}
		return path, nil
	}
	path := value
	if !filepath.IsAbs(path) && t.workspace != "" {
		path = filepath.Join(t.workspace, path)
	}
	path = filepath.Clean(path)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("input image not found: %s", path)
	}
	return path, nil
}

func buildImagePayload(model, prompt string, inputImages []string, args map[string]any) (map[string]any, error) {
	parts := []map[string]any{{
		"type": "text",
		"text": prompt,
	}}

	for _, key := range []string{"size", "aspect_ratio", "quality", "style", "background"} {
		if value := strings.TrimSpace(imageStringArg(args, key)); value != "" {
			parts = append(parts, map[string]any{
				"type": "text",
				"text": fmt.Sprintf("%s: %s", key, value),
			})
		}
	}

	if len(inputImages) == 1 {
		dataURL, err := encodeImageAsDataURL(inputImages[0])
		if err != nil {
			return nil, err
		}
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": dataURL,
			},
		})
	}

	return map[string]any{
		"model": model,
		"messages": []map[string]any{{
			"role":    "user",
			"content": parts,
		}},
	}, nil
}

func encodeImageAsDataURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read input image %s: %w", path, err)
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType == "" {
		contentType = "image/png"
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (t *GenerateImageTool) sendCPARequest(
	ctx context.Context,
	client *http.Client,
	apiBase, apiKey string,
	payload map[string]any,
) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal image request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build image request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call CPA image endpoint: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read CPA image response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("CPA API error %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var decoded map[string]any
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("CPA API returned non-JSON response")
	}
	return decoded, nil
}

func (t *GenerateImageTool) collectOutputs(responseData map[string]any) ([]imageOutput, error) {
	var outputs []imageOutput
	visitImageCandidates(responseData, &outputs)
	if len(outputs) == 0 {
		return nil, fmt.Errorf("CPA image response did not include any usable image output")
	}
	return dedupeOutputs(outputs), nil
}

func visitImageCandidates(value any, outputs *[]imageOutput) {
	switch typed := value.(type) {
	case string:
		if output, ok := stringToOutput(typed); ok {
			*outputs = append(*outputs, output)
		}
	case []any:
		for _, item := range typed {
			visitImageCandidates(item, outputs)
		}
	case map[string]any:
		if output, ok := mapToOutput(typed); ok {
			*outputs = append(*outputs, output)
		}
		for _, item := range typed {
			visitImageCandidates(item, outputs)
		}
	}
}

func stringToOutput(value string) (imageOutput, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return imageOutput{}, false
	}
	if strings.HasPrefix(strings.ToLower(text), "data:image/") {
		data, contentType, err := decodeDataURL(text)
		if err != nil {
			return imageOutput{}, false
		}
		return imageOutput{data: data, contentType: contentType, filename: filenameForContentType(contentType)}, true
	}
	if strings.HasPrefix(strings.ToLower(text), "http://") || strings.HasPrefix(strings.ToLower(text), "https://") {
		return imageOutput{remoteURL: text, filename: filenameFromURL(text)}, true
	}
	if hasImageExt(text) {
		return imageOutput{
			localPath:   text,
			filename:    filepath.Base(text),
			contentType: mime.TypeByExtension(strings.ToLower(filepath.Ext(text))),
		}, true
	}
	return imageOutput{}, false
}

func mapToOutput(value map[string]any) (imageOutput, bool) {
	if raw, ok := value["b64_json"].(string); ok && strings.TrimSpace(raw) != "" {
		data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
		if err == nil {
			contentType := contentTypeFromMap(value)
			if contentType == "" {
				contentType = "image/png"
			}
			return imageOutput{data: data, contentType: contentType, filename: filenameForContentType(contentType)}, true
		}
	}
	for _, key := range []string{"url", "image_url", "path", "file", "name", "image"} {
		if raw, ok := value[key].(string); ok {
			if output, ok := stringToOutput(raw); ok {
				if output.contentType == "" {
					output.contentType = contentTypeFromMap(value)
				}
				if output.filename == "" {
					output.filename = filenameFromMap(value)
				}
				return output, true
			}
		}
	}
	return imageOutput{}, false
}

func decodeDataURL(value string) ([]byte, string, error) {
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid data url")
	}
	head := parts[0]
	if !strings.HasSuffix(strings.ToLower(head), ";base64") {
		return nil, "", fmt.Errorf("unsupported data url encoding")
	}
	contentType := strings.TrimPrefix(head, "data:")
	contentType = strings.TrimSuffix(contentType, ";base64")
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, "", err
	}
	return decoded, contentType, nil
}

func dedupeOutputs(outputs []imageOutput) []imageOutput {
	seen := make(map[string]struct{}, len(outputs))
	result := make([]imageOutput, 0, len(outputs))
	for _, output := range outputs {
		key := output.remoteURL + "|" + output.localPath + "|" + output.filename + "|" + output.contentType +
			"|" + fmt.Sprintf("%d", len(output.data))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, output)
	}
	return result
}

func (t *GenerateImageTool) storeOutput(ctx context.Context, output imageOutput) (string, error) {
	localPath := output.localPath
	if output.remoteURL != "" {
		filename := output.filename
		if filename == "" {
			filename = "generated-image.png"
		}
		localPath = utils.DownloadFile(output.remoteURL, filename, utils.DownloadOptions{LoggerPrefix: "image"})
		if localPath == "" {
			return "", fmt.Errorf("failed to download generated image")
		}
	}
	if len(output.data) > 0 {
		filename := output.filename
		if filename == "" {
			filename = "generated-image.png"
		}
		var err error
		localPath, err = t.writeTempImage(filename, output.data)
		if err != nil {
			return "", err
		}
	}
	if localPath == "" {
		return "", fmt.Errorf("generated image did not resolve to a local file")
	}

	filename := output.filename
	if filename == "" {
		filename = filepath.Base(localPath)
	}
	contentType := output.contentType
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
	}
	scope := fmt.Sprintf("tool:generate_image:%s:%s:%s", ToolChannel(ctx), ToolChatID(ctx), uuid.NewString())
	ref, err := t.mediaStore.Store(localPath, media.MediaMeta{
		Filename:    filename,
		ContentType: contentType,
		Source:      "tool:generate_image",
	}, scope)
	if err != nil {
		return "", fmt.Errorf("store generated image: %w", err)
	}
	return ref, nil
}

func (t *GenerateImageTool) writeTempImage(filename string, data []byte) (string, error) {
	dir := filepath.Join(t.tempDir(), "picoclaw_media")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create temp media dir: %w", err)
	}
	path := filepath.Join(dir, uuid.NewString()[:8]+"_"+utils.SanitizeFilename(filename))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write generated image: %w", err)
	}
	return path, nil
}

func imageStringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func durationArg(args map[string]any, key string, fallback time.Duration) time.Duration {
	value, ok := args[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		if typed > 0 {
			return time.Duration(typed * float64(time.Second))
		}
	case int:
		if typed > 0 {
			return time.Duration(typed) * time.Second
		}
	}
	return fallback
}

func stringSlice(value any) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed, nil
		}
		return nil, fmt.Errorf("not a string array")
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("not a string array")
		}
		out = append(out, text)
	}
	return out, nil
}

func contentTypeFromMap(value map[string]any) string {
	for _, key := range []string{"mime_type", "content_type", "type"} {
		if text, ok := value[key].(string); ok && strings.Contains(text, "/") {
			return text
		}
	}
	return ""
}

func filenameFromMap(value map[string]any) string {
	for _, key := range []string{"filename", "name", "path", "file"} {
		if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
			return filepath.Base(text)
		}
	}
	return ""
}

func filenameForContentType(contentType string) string {
	exts, _ := mime.ExtensionsByType(contentType)
	ext := ".png"
	if len(exts) > 0 {
		ext = exts[0]
	}
	return "generated-image" + ext
}

func filenameFromURL(rawURL string) string {
	base := filepath.Base(strings.SplitN(rawURL, "?", 2)[0])
	if hasImageExt(base) {
		return base
	}
	return "generated-image.png"
}

func hasImageExt(value string) bool {
	ext := strings.ToLower(filepath.Ext(value))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp":
		return true
	default:
		return false
	}
}
