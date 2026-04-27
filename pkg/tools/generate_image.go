package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/utils"
)

const defaultImageTimeout = 300 * time.Second

const defaultImageUserAgent = "picoclaw/1.0"

type GenerateImageTool struct {
	workspace  string
	restrict   bool
	mediaStore media.MediaStore
	httpClient *http.Client
	getenv     func(string) string
	tempDir    func() string
}

func NewGenerateImageTool(workspace string, restrict bool) *GenerateImageTool {
	return &GenerateImageTool{
		workspace: workspace,
		restrict:  restrict,
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
	return "Generate or edit an image through CPA image APIs or chat completions and return it as chat media. Pass exactly one source image via image, input_image, or input_images; media://current works when exactly one current inbound image is available."
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
				"description": "Optional local file path or media:// ref for one input image. media://current is supported when exactly one current inbound image is available.",
			},
			"input_image": map[string]any{
				"type":        "string",
				"description": "Optional alias for image. Do not combine different values across aliases.",
			},
			"input_images": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
				"description": "Optional explicit list of input images. Only one unique explicit input image is supported safely.",
			},
			"aspect_ratio": map[string]any{
				"type":        "string",
				"description": "Optional aspect ratio such as 1:1, 4:3, 3:4, 16:9, or 9:16, passed through to CPA when supported.",
			},
			"quality": map[string]any{
				"type":        "string",
				"description": "Optional quality passed through to CPA.",
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

	apiKey := strings.TrimSpace(t.lookupEnv("CPA_API_KEY"))
	if apiKey == "" {
		return ErrorResult("CPA_API_KEY is required")
	}
	apiBase := strings.TrimSpace(t.lookupEnv("CPA_API_BASE"))
	if apiBase == "" {
		return ErrorResult("CPA_API_BASE is required")
	}
	model := strings.TrimSpace(t.lookupEnv("CPA_IMAGE_MODEL"))
	if model == "" {
		return ErrorResult("CPA_IMAGE_MODEL is required")
	}

	timeout := durationArg(args, "timeout_seconds", defaultImageTimeout)
	client := *t.httpClient
	client.Timeout = timeout

	resolvedInputs, err := t.resolveInputImages(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	responseData, err := t.sendCPARequest(
		ctx,
		&client,
		strings.TrimRight(apiBase, "/"),
		apiKey,
		model,
		prompt,
		resolvedInputs,
		args,
	)
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

func (t *GenerateImageTool) lookupEnv(name string) string {
	if value := strings.TrimSpace(t.getenv(name)); value != "" {
		return value
	}
	envFile := filepath.Join(picoclawHomeFromGetenv(t.getenv), ".env")
	values, err := loadSimpleEnvFile(envFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values[name])
}

func picoclawHomeFromGetenv(getenv func(string) string) string {
	if home := strings.TrimSpace(getenv("PICOCLAW_HOME")); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".picoclaw")
}

func loadSimpleEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

type imageOutput struct {
	filename    string
	contentType string
	localPath   string
	data        []byte
	remoteURL   string
}

func (t *GenerateImageTool) resolveInputImages(ctx context.Context, args map[string]any) ([]string, error) {
	var candidates []string
	seen := make(map[string]struct{}, 3)
	appendCandidate := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		candidates = append(candidates, value)
		seen[value] = struct{}{}
	}
	for _, key := range []string{"image", "input_image"} {
		appendCandidate(imageStringArg(args, key))
	}
	if raw, ok := args["input_images"]; ok {
		list, err := stringSlice(raw)
		if err != nil {
			return nil, fmt.Errorf("input_images must be an array of strings")
		}
		for _, item := range list {
			appendCandidate(item)
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) > 1 {
		return nil, fmt.Errorf("multiple input images provided; pass only one explicit input image")
	}

	resolved, err := t.resolveInputRefWithContext(ctx, candidates[0])
	if err != nil {
		return nil, err
	}
	return []string{resolved}, nil
}

func (t *GenerateImageTool) resolveInputRefWithContext(ctx context.Context, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "media://current" {
		mediaRefs := ToolMediaRefs(ctx)
		switch len(mediaRefs) {
		case 0:
			return "", fmt.Errorf("resolve input media %q: no current inbound image is available; pass an exact media:// ref from this turn", value)
		case 1:
			return t.resolveInputRef(mediaRefs[0])
		default:
			return "", fmt.Errorf("resolve input media %q: multiple current inbound images are available; pass one exact media:// ref from this turn", value)
		}
	}
	return t.resolveInputRef(value)
}

func (t *GenerateImageTool) resolveInputRef(value string) (string, error) {
	if strings.HasPrefix(value, "media://") {
		path, _, err := t.mediaStore.ResolveWithMeta(value)
		if err != nil {
			return "", fmt.Errorf("resolve input media %q: %w", value, err)
		}
		path = filepath.Clean(path)
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("input image not found: %s", path)
		}
		return path, nil
	}
	path, err := validatePath(value, t.workspace, t.restrict)
	if err != nil {
		return "", err
	}
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

	for _, key := range []string{"aspect_ratio", "quality", "background"} {
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
	contentType := detectInputImageContentType(path, data)
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func detectInputImageContentType(path string, data []byte) string {
	if contentType := normalizeMIMEType(mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))); isImageMIME(contentType) {
		return contentType
	}
	if contentType := normalizeMIMEType(http.DetectContentType(data)); isImageMIME(contentType) {
		return contentType
	}
	if ext := utils.PreferredExtensionForBytes(data); ext != "" {
		if contentType := normalizeMIMEType(mime.TypeByExtension(ext)); isImageMIME(contentType) {
			return contentType
		}
	}
	return "image/png"
}

func normalizeMIMEType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, _, err := mime.ParseMediaType(value); err == nil {
		value = parsed
	}
	return strings.ToLower(value)
}

func isImageMIME(value string) bool {
	return strings.HasPrefix(normalizeMIMEType(value), "image/")
}

func imageModelUsesImageAPI(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(normalized, "gpt-image-") || strings.HasPrefix(normalized, "dall-e-")
}

func buildImageAPIPrompt(prompt string, args map[string]any) string {
	lines := []string{strings.TrimSpace(prompt)}

	if aspectRatio := normalizeAspectRatio(imageStringArg(args, "aspect_ratio")); aspectRatio != "" {
		lines = append(lines, fmt.Sprintf("Target aspect ratio: %s", aspectRatio))
	}

	return strings.Join(lines, "\n")
}

func resolveImageAPISize(args map[string]any) string {
	switch normalizeAspectRatio(imageStringArg(args, "aspect_ratio")) {
	case "1:1":
		return "1024x1024"
	case "4:3":
		return "1360x1024"
	case "3:4":
		return "1024x1360"
	case "16:9":
		return "1536x864"
	case "9:16":
		return "864x1536"
	default:
		return ""
	}
}

func normalizeAspectRatio(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, " ", ""))
}

func buildImageAPIRequest(model, prompt string, inputImages []string, args map[string]any) (string, string, []byte, error) {
	return buildImageAPIRequestWithOverrides(model, prompt, inputImages, args, "")
}

func buildImageAPIRequestWithOverrides(
	model, prompt string,
	inputImages []string,
	args map[string]any,
	qualityOverride string,
) (string, string, []byte, error) {
	if len(inputImages) == 0 {
		payload := map[string]any{
			"model":  model,
			"prompt": buildImageAPIPrompt(prompt, args),
		}
		for _, key := range []string{"quality", "background"} {
			if key == "quality" && strings.TrimSpace(qualityOverride) != "" {
				continue
			}
			if value := strings.TrimSpace(imageStringArg(args, key)); value != "" {
				payload[key] = value
			}
		}
		if strings.TrimSpace(qualityOverride) != "" {
			payload["quality"] = strings.TrimSpace(qualityOverride)
		}
		if size := resolveImageAPISize(args); size != "" {
			payload["size"] = size
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return "", "", nil, fmt.Errorf("marshal image generation request: %w", err)
		}
		return "/images/generations", "application/json", body, nil
	}

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	writeField := func(name, value string) error {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		if err := writer.WriteField(name, value); err != nil {
			return fmt.Errorf("write multipart field %s: %w", name, err)
		}
		return nil
	}

	if err := writeField("model", model); err != nil {
		return "", "", nil, err
	}
	if err := writeField("prompt", buildImageAPIPrompt(prompt, args)); err != nil {
		return "", "", nil, err
	}
	for _, key := range []string{"quality", "background"} {
		if key == "quality" && strings.TrimSpace(qualityOverride) != "" {
			continue
		}
		if err := writeField(key, imageStringArg(args, key)); err != nil {
			return "", "", nil, err
		}
	}
	if strings.TrimSpace(qualityOverride) != "" {
		if err := writeField("quality", strings.TrimSpace(qualityOverride)); err != nil {
			return "", "", nil, err
		}
	}
	if err := writeField("size", resolveImageAPISize(args)); err != nil {
		return "", "", nil, err
	}

	for _, inputImage := range inputImages {
		if err := addMultipartImage(writer, "image[]", inputImage); err != nil {
			return "", "", nil, err
		}
	}

	if err := writer.Close(); err != nil {
		return "", "", nil, fmt.Errorf("finalize multipart image edit request: %w", err)
	}

	return "/images/edits", writer.FormDataContentType(), buffer.Bytes(), nil
}

func addMultipartImage(writer *multipart.Writer, fieldName, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open input image %s: %w", path, err)
	}
	defer file.Close()

	sample := make([]byte, 512)
	n, readErr := file.Read(sample)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("read input image header %s: %w", path, readErr)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind input image %s: %w", path, err)
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     fieldName,
		"filename": filepath.Base(path),
	}))
	header.Set("Content-Type", detectInputImageContentType(path, sample[:n]))

	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create multipart image part for %s: %w", path, err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("copy input image %s: %w", path, err)
	}

	return nil
}

func (t *GenerateImageTool) sendCPARequest(
	ctx context.Context,
	client *http.Client,
	apiBase, apiKey string,
	model, prompt string,
	inputImages []string,
	args map[string]any,
) (map[string]any, error) {
	return t.sendCPARequestAttempt(ctx, client, apiBase, apiKey, model, prompt, inputImages, args, "")
}

func (t *GenerateImageTool) sendCPARequestAttempt(
	ctx context.Context,
	client *http.Client,
	apiBase, apiKey string,
	model, prompt string,
	inputImages []string,
	args map[string]any,
	qualityOverride string,
) (map[string]any, error) {
	var (
		endpoint    string
		contentType string
		body        []byte
		err         error
	)
	if imageModelUsesImageAPI(model) {
		endpoint, contentType, body, err = buildImageAPIRequestWithOverrides(
			model,
			prompt,
			inputImages,
			args,
			qualityOverride,
		)
	} else {
		payload, payloadErr := buildImagePayload(model, prompt, inputImages, args)
		if payloadErr != nil {
			return nil, payloadErr
		}
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal image request: %w", err)
		}
		endpoint = "/chat/completions"
		contentType = "application/json"
	}
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build image request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("User-Agent", defaultImageUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		if shouldRetryImageRequest(model, args, qualityOverride, err, 0) {
			return t.sendCPARequestAttempt(ctx, client, apiBase, apiKey, model, prompt, inputImages, args, "low")
		}
		return nil, fmt.Errorf("call CPA image endpoint %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read CPA image response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if shouldRetryImageRequest(model, args, qualityOverride, nil, resp.StatusCode) {
			return t.sendCPARequestAttempt(ctx, client, apiBase, apiKey, model, prompt, inputImages, args, "low")
		}
		return nil, fmt.Errorf("CPA API error %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var decoded map[string]any
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("CPA API returned non-JSON response")
	}
	return decoded, nil
}

func shouldRetryImageRequest(model string, args map[string]any, qualityOverride string, err error, statusCode int) bool {
	if !imageModelUsesImageAPI(model) {
		return false
	}
	if strings.TrimSpace(qualityOverride) != "" {
		return false
	}
	if strings.ToLower(strings.TrimSpace(imageStringArg(args, "quality"))) == "low" {
		return false
	}
	if statusCode == http.StatusRequestTimeout || statusCode == http.StatusGatewayTimeout || statusCode == 524 {
		return true
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
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
		Owned:       true,
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
