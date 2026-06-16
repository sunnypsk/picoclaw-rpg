package tools

import (
	"archive/zip"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/utils"
)

//go:embed presentation_assets/anime.umd.min.js presentation_assets/anime.LICENSE.md
var presentationAssets embed.FS

const (
	defaultPresentationTheme      = "executive"
	defaultPresentationOutput     = "offline_zip"
	defaultPresentationAspect     = "16:9"
	maxPresentationSlides         = 40
	generatedPresentationsDirName = "generated-presentations"
)

var (
	presentationLayouts = map[string]struct{}{
		"cover":         {},
		"section":       {},
		"title-bullets": {},
		"two-column":    {},
		"image-hero":    {},
		"comparison":    {},
		"timeline":      {},
		"metrics":       {},
		"quote":         {},
		"closing":       {},
	}
	presentationAnimations = map[string]struct{}{
		"auto":      {},
		"none":      {},
		"fade-up":   {},
		"stagger":   {},
		"scale-in":  {},
		"draw-line": {},
		"count-up":  {},
	}
	presentationThemes = map[string]struct{}{
		"executive": {},
		"studio":    {},
		"signal":    {},
	}
	slugUnsafePattern = regexp.MustCompile(`[^a-z0-9]+`)
)

type GeneratePresentationTool struct {
	workspace  string
	restrict   bool
	mediaStore media.MediaStore
	now        func() time.Time
}

type presentationDeck struct {
	Title       string              `json:"title"`
	Slug        string              `json:"slug,omitempty"`
	Theme       string              `json:"theme"`
	Output      string              `json:"output"`
	AspectRatio string              `json:"aspect_ratio"`
	Language    string              `json:"language,omitempty"`
	Slides      []presentationSlide `json:"slides"`
}

type presentationSlide struct {
	Layout    string             `json:"layout"`
	Title     string             `json:"title,omitempty"`
	Subtitle  string             `json:"subtitle,omitempty"`
	Body      string             `json:"body,omitempty"`
	Bullets   []string           `json:"bullets,omitempty"`
	Items     []presentationItem `json:"items,omitempty"`
	Image     *presentationImage `json:"image,omitempty"`
	Animation string             `json:"animation"`

	Number int    `json:"-"`
	Accent string `json:"-"`
}

type presentationImage struct {
	Src         string `json:"src"`
	Alt         string `json:"alt,omitempty"`
	ResolvedSrc string `json:"-"`
}

type presentationItem struct {
	Label string `json:"label,omitempty"`
	Title string `json:"title,omitempty"`
	Value string `json:"value,omitempty"`
	Body  string `json:"body,omitempty"`
}

type presentationOutput struct {
	Deck       presentationDeck
	DeckDir    string
	IndexPath  string
	ZipPath    string
	ZipMedia   string
	OutputMode string
}

func NewGeneratePresentationTool(workspace string, restrict bool) *GeneratePresentationTool {
	return &GeneratePresentationTool{
		workspace: workspace,
		restrict:  restrict,
		now:       time.Now,
	}
}

func (t *GeneratePresentationTool) SetMediaStore(store media.MediaStore) {
	t.mediaStore = store
}

func (t *GeneratePresentationTool) Name() string {
	return "generate_presentation"
}

func (t *GeneratePresentationTool) Description() string {
	return "Generate an elegant offline animated HTML presentation package from a structured slide spec. " +
		"Outputs a durable workspace folder and, by default, a ZIP that opens locally on Mac and Windows."
}

func (t *GeneratePresentationTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Presentation title.",
			},
			"slug": map[string]any{
				"type":        "string",
				"description": "Optional safe output slug. Defaults to a slug generated from title.",
			},
			"theme": map[string]any{
				"type":        "string",
				"enum":        []string{"executive", "studio", "signal"},
				"description": "Visual theme. Defaults to executive.",
			},
			"output": map[string]any{
				"type":        "string",
				"enum":        []string{"offline_zip", "folder"},
				"description": "Output mode. Defaults to offline_zip.",
			},
			"aspect_ratio": map[string]any{
				"type":        "string",
				"enum":        []string{"16:9"},
				"description": "Deck aspect ratio. V1 supports 16:9.",
			},
			"language": map[string]any{
				"type":        "string",
				"description": "Optional language hint stored in the deck metadata.",
			},
			"slides": map[string]any{
				"type":        "array",
				"description": "Structured slide objects using approved layouts and animation presets.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"layout": map[string]any{
							"type": "string",
							"enum": []string{
								"cover", "section", "title-bullets", "two-column", "image-hero",
								"comparison", "timeline", "metrics", "quote", "closing",
							},
						},
						"title":     map[string]any{"type": "string"},
						"subtitle":  map[string]any{"type": "string"},
						"body":      map[string]any{"type": "string"},
						"bullets":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"animation": map[string]any{"type": "string", "enum": []string{"auto", "none", "fade-up", "stagger", "scale-in", "draw-line", "count-up"}},
						"image": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"src": map[string]any{"type": "string"},
								"alt": map[string]any{"type": "string"},
							},
						},
						"items": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label": map[string]any{"type": "string"},
									"title": map[string]any{"type": "string"},
									"value": map[string]any{"type": "string"},
									"body":  map[string]any{"type": "string"},
								},
							},
						},
					},
					"required": []string{"layout"},
				},
			},
		},
		"required": []string{"title", "slides"},
	}
}

func (t *GeneratePresentationTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	output, err := t.generate(ctx, args)
	if err != nil {
		return ErrorResult(err.Error()).WithError(err)
	}

	userLines := []string{
		"Presentation generated.",
		"Open: " + output.IndexPath,
	}
	if output.ZipPath != "" {
		userLines = append(userLines, "ZIP: "+output.ZipPath)
	}

	llm := map[string]any{
		"title":      output.Deck.Title,
		"slides":     len(output.Deck.Slides),
		"folder":     output.DeckDir,
		"index_html": output.IndexPath,
		"zip":        output.ZipPath,
		"theme":      output.Deck.Theme,
		"output":     output.OutputMode,
	}
	llmJSON, _ := json.MarshalIndent(llm, "", "  ")

	result := &ToolResult{
		ForLLM:  string(llmJSON),
		ForUser: strings.Join(userLines, "\n"),
	}
	if output.ZipMedia != "" {
		result.Media = []string{output.ZipMedia}
	}
	return result
}

func (t *GeneratePresentationTool) generate(ctx context.Context, args map[string]any) (presentationOutput, error) {
	deck, err := parsePresentationDeck(args)
	if err != nil {
		return presentationOutput{}, err
	}

	root, err := validatePath(generatedPresentationsDirName, t.workspace, true)
	if err != nil {
		return presentationOutput{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return presentationOutput{}, fmt.Errorf("create presentation output root: %w", err)
	}

	timestamp := t.now().Format("20060102-150405")
	baseSlug := sanitizePresentationSlug(deck.Slug)
	if baseSlug == "" {
		baseSlug = sanitizePresentationSlug(deck.Title)
	}
	if baseSlug == "" {
		baseSlug = "presentation"
	}
	deck.Slug = baseSlug

	deckDir := filepath.Join(root, baseSlug+"-"+timestamp)
	if err := os.MkdirAll(filepath.Join(deckDir, "assets", "images"), 0o755); err != nil {
		return presentationOutput{}, fmt.Errorf("create presentation package: %w", err)
	}

	for i := range deck.Slides {
		deck.Slides[i].Number = i + 1
		deck.Slides[i].Accent = presentationAccent(i)
		if deck.Slides[i].Image == nil || strings.TrimSpace(deck.Slides[i].Image.Src) == "" {
			continue
		}
		resolved, err := t.copyPresentationImage(ctx, deck.Slides[i].Image.Src, deckDir, i+1)
		if err != nil {
			return presentationOutput{}, fmt.Errorf("slide %d image: %w", i+1, err)
		}
		deck.Slides[i].Image.ResolvedSrc = resolved
	}

	if err := writeEmbeddedPresentationAsset(deckDir, "anime.umd.min.js"); err != nil {
		return presentationOutput{}, err
	}
	if err := writeEmbeddedPresentationAsset(deckDir, "anime.LICENSE.md"); err != nil {
		return presentationOutput{}, err
	}

	indexHTML, err := renderPresentationHTML(deck)
	if err != nil {
		return presentationOutput{}, err
	}
	indexPath := filepath.Join(deckDir, "index.html")
	if err := fileutil.WriteFileAtomic(indexPath, []byte(indexHTML), 0o644); err != nil {
		return presentationOutput{}, fmt.Errorf("write index.html: %w", err)
	}

	deckJSON, err := json.MarshalIndent(deck, "", "  ")
	if err != nil {
		return presentationOutput{}, fmt.Errorf("marshal deck.json: %w", err)
	}
	if err := fileutil.WriteFileAtomic(filepath.Join(deckDir, "deck.json"), append(deckJSON, '\n'), 0o644); err != nil {
		return presentationOutput{}, fmt.Errorf("write deck.json: %w", err)
	}
	if err := fileutil.WriteFileAtomic(filepath.Join(deckDir, "README.txt"), []byte(presentationReadme(deck)), 0o644); err != nil {
		return presentationOutput{}, fmt.Errorf("write README.txt: %w", err)
	}

	var zipPath, zipMedia string
	if deck.Output == defaultPresentationOutput {
		zipPath = filepath.Join(deckDir, baseSlug+".zip")
		if err := zipPresentationPackage(deckDir, zipPath); err != nil {
			return presentationOutput{}, err
		}
		if t.mediaStore != nil {
			scope := fmt.Sprintf("tool:generate_presentation:%s:%s:%d", ToolChannel(ctx), ToolChatID(ctx), t.now().UnixNano())
			ref, err := t.mediaStore.Store(zipPath, media.MediaMeta{
				Filename:    filepath.Base(zipPath),
				ContentType: "application/zip",
				Source:      "tool:generate_presentation",
				Owned:       false,
			}, scope)
			if err != nil {
				return presentationOutput{}, fmt.Errorf("store presentation ZIP media: %w", err)
			}
			zipMedia = ref
		}
	}

	return presentationOutput{
		Deck:       deck,
		DeckDir:    deckDir,
		IndexPath:  indexPath,
		ZipPath:    zipPath,
		ZipMedia:   zipMedia,
		OutputMode: deck.Output,
	}, nil
}

func parsePresentationDeck(args map[string]any) (presentationDeck, error) {
	deck := presentationDeck{
		Title:       strings.TrimSpace(presentationStringArg(args, "title")),
		Slug:        strings.TrimSpace(presentationStringArg(args, "slug")),
		Theme:       strings.TrimSpace(presentationStringArg(args, "theme")),
		Output:      strings.TrimSpace(presentationStringArg(args, "output")),
		AspectRatio: strings.TrimSpace(presentationStringArg(args, "aspect_ratio")),
		Language:    strings.TrimSpace(presentationStringArg(args, "language")),
	}
	if deck.Title == "" {
		return presentationDeck{}, fmt.Errorf("title is required")
	}
	if deck.Theme == "" {
		deck.Theme = defaultPresentationTheme
	}
	if _, ok := presentationThemes[deck.Theme]; !ok {
		return presentationDeck{}, fmt.Errorf("unsupported theme %q", deck.Theme)
	}
	if deck.Output == "" {
		deck.Output = defaultPresentationOutput
	}
	if deck.Output != defaultPresentationOutput && deck.Output != "folder" {
		return presentationDeck{}, fmt.Errorf("unsupported output %q", deck.Output)
	}
	if deck.AspectRatio == "" {
		deck.AspectRatio = defaultPresentationAspect
	}
	if deck.AspectRatio != defaultPresentationAspect {
		return presentationDeck{}, fmt.Errorf("unsupported aspect_ratio %q; v1 supports 16:9", deck.AspectRatio)
	}

	rawSlides, ok := args["slides"].([]any)
	if !ok {
		return presentationDeck{}, fmt.Errorf("slides must be an array")
	}
	if len(rawSlides) == 0 {
		return presentationDeck{}, fmt.Errorf("slides must contain at least one slide")
	}
	if len(rawSlides) > maxPresentationSlides {
		return presentationDeck{}, fmt.Errorf("slides exceeds v1 limit of %d", maxPresentationSlides)
	}

	for i, raw := range rawSlides {
		rawMap, ok := raw.(map[string]any)
		if !ok {
			return presentationDeck{}, fmt.Errorf("slide %d must be an object", i+1)
		}
		slide, err := parsePresentationSlide(rawMap)
		if err != nil {
			return presentationDeck{}, fmt.Errorf("slide %d: %w", i+1, err)
		}
		deck.Slides = append(deck.Slides, slide)
	}

	return deck, nil
}

func parsePresentationSlide(args map[string]any) (presentationSlide, error) {
	slide := presentationSlide{
		Layout:    strings.TrimSpace(presentationStringArg(args, "layout")),
		Title:     strings.TrimSpace(presentationStringArg(args, "title")),
		Subtitle:  strings.TrimSpace(presentationStringArg(args, "subtitle")),
		Body:      strings.TrimSpace(presentationStringArg(args, "body")),
		Animation: strings.TrimSpace(presentationStringArg(args, "animation")),
	}
	if _, ok := presentationLayouts[slide.Layout]; !ok {
		return presentationSlide{}, fmt.Errorf("unsupported layout %q", slide.Layout)
	}
	if slide.Animation == "" {
		slide.Animation = "auto"
	}
	if _, ok := presentationAnimations[slide.Animation]; !ok {
		return presentationSlide{}, fmt.Errorf("unsupported animation %q", slide.Animation)
	}
	slide.Bullets = trimPresentationStringArray(args["bullets"])
	slide.Items = parsePresentationItems(args["items"])
	if rawImage, ok := args["image"].(map[string]any); ok {
		image := &presentationImage{
			Src: strings.TrimSpace(presentationStringArg(rawImage, "src")),
			Alt: strings.TrimSpace(presentationStringArg(rawImage, "alt")),
		}
		if image.Src != "" {
			if isRemotePresentationAsset(image.Src) {
				return presentationSlide{}, fmt.Errorf("remote image URLs are not supported in v1")
			}
			slide.Image = image
		}
	}
	if err := validatePresentationSlideContent(slide); err != nil {
		return presentationSlide{}, err
	}
	return slide, nil
}

func validatePresentationSlideContent(slide presentationSlide) error {
	switch slide.Layout {
	case "cover", "section", "closing":
		if slide.Title == "" {
			return fmt.Errorf("%s layout requires title", slide.Layout)
		}
	case "title-bullets":
		if slide.Title == "" || len(slide.Bullets) == 0 {
			return fmt.Errorf("title-bullets layout requires title and bullets")
		}
	case "two-column", "comparison", "timeline", "metrics":
		if slide.Title == "" || len(slide.Items) == 0 {
			return fmt.Errorf("%s layout requires title and items", slide.Layout)
		}
	case "image-hero":
		if slide.Title == "" || slide.Image == nil {
			return fmt.Errorf("image-hero layout requires title and image")
		}
	case "quote":
		if slide.Body == "" {
			return fmt.Errorf("quote layout requires body")
		}
	}
	return nil
}

func parsePresentationItems(raw any) []presentationItem {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	items := make([]presentationItem, 0, len(list))
	for _, item := range list {
		rawMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		parsed := presentationItem{
			Label: strings.TrimSpace(presentationStringArg(rawMap, "label")),
			Title: strings.TrimSpace(presentationStringArg(rawMap, "title")),
			Value: strings.TrimSpace(presentationStringArg(rawMap, "value")),
			Body:  strings.TrimSpace(presentationStringArg(rawMap, "body")),
		}
		if parsed.Label != "" || parsed.Title != "" || parsed.Value != "" || parsed.Body != "" {
			items = append(items, parsed)
		}
	}
	return items
}

func (t *GeneratePresentationTool) copyPresentationImage(
	ctx context.Context,
	src string,
	deckDir string,
	slideNumber int,
) (string, error) {
	localPath := ""
	filename := filepath.Base(src)
	if strings.HasPrefix(src, "media://") {
		if t.mediaStore == nil {
			return "", fmt.Errorf("media image %q requires a media store", src)
		}
		if src == "media://current" {
			mediaRefs := ToolMediaRefs(ctx)
			switch len(mediaRefs) {
			case 0:
				return "", fmt.Errorf("resolve media image %q: no current inbound image is available", src)
			case 1:
				src = mediaRefs[0]
			default:
				return "", fmt.Errorf("resolve media image %q: multiple current inbound images are available; pass one exact media:// ref", src)
			}
		}
		path, meta, err := t.mediaStore.ResolveWithMeta(src)
		if err != nil {
			return "", fmt.Errorf("resolve media image %q: %w", src, err)
		}
		localPath = path
		if strings.TrimSpace(meta.Filename) != "" {
			filename = meta.Filename
		}
	} else {
		path, err := validatePath(src, t.workspace, true)
		if err != nil {
			return "", err
		}
		localPath = path
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return "", fmt.Errorf("image not found: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("image path points to a directory")
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	if !presentationLooksLikeImage(filename, data) {
		return "", fmt.Errorf("only local image files are supported")
	}
	safeName := fmt.Sprintf("slide-%02d-%s", slideNumber, utils.SanitizeFilename(filename))
	if filepath.Ext(safeName) == "" {
		if ext := utils.PreferredExtensionForBytes(data); ext != "" {
			safeName += ext
		} else {
			safeName += ".png"
		}
	}
	target := filepath.Join(deckDir, "assets", "images", safeName)
	if err := fileutil.WriteFileAtomic(target, data, 0o644); err != nil {
		return "", fmt.Errorf("copy image: %w", err)
	}
	return filepath.ToSlash(filepath.Join("assets", "images", safeName)), nil
}

func presentationLooksLikeImage(filename string, data []byte) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if slices.Contains([]string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".svg", ".bmp"}, ext) {
		return true
	}
	contentType := mime.TypeByExtension(ext)
	if strings.HasPrefix(contentType, "image/") {
		return true
	}
	return strings.HasPrefix(mime.TypeByExtension(utils.PreferredExtensionForBytes(data)), "image/")
}

func writeEmbeddedPresentationAsset(deckDir, name string) error {
	data, err := presentationAssets.ReadFile(filepath.ToSlash(filepath.Join("presentation_assets", name)))
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", name, err)
	}
	target := filepath.Join(deckDir, "assets", name)
	if err := fileutil.WriteFileAtomic(target, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

func renderPresentationHTML(deck presentationDeck) (string, error) {
	var sb strings.Builder
	tmpl, err := template.New("presentation").Parse(presentationHTMLTemplate)
	if err != nil {
		return "", err
	}
	if err := tmpl.Execute(&sb, deck); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func zipPresentationPackage(deckDir, zipPath string) error {
	out, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("create ZIP: %w", err)
	}
	defer out.Close()

	zipWriter := zip.NewWriter(out)
	defer zipWriter.Close()

	base := filepath.Base(deckDir)
	return filepath.WalkDir(deckDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Clean(path) == filepath.Clean(zipPath) {
			return nil
		}
		rel, err := filepath.Rel(deckDir, path)
		if err != nil {
			return err
		}
		entryName := filepath.ToSlash(filepath.Join(base, rel))
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = entryName
		header.Method = zip.Deflate
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(writer, in)
		return err
	})
}

func presentationReadme(deck presentationDeck) string {
	return fmt.Sprintf(`%s

Open index.html in a modern browser on Mac or Windows.

Keyboard:
- Right arrow or Space: next step or slide
- Left arrow: previous step or slide
- Home: first slide
- End: last slide

This package is offline-ready. It includes a local Anime.js v4.4.1 bundle under assets/.
`, deck.Title)
}

func sanitizePresentationSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugUnsafePattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if len(value) > 64 {
		value = strings.Trim(value[:64], "-")
	}
	return value
}

func presentationAccent(index int) string {
	accents := []string{"teal", "coral", "gold", "indigo"}
	return accents[index%len(accents)]
}

func isRemotePresentationAsset(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:") ||
		strings.HasPrefix(lower, "javascript:")
}

func presentationStringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func trimPresentationStringArray(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]string); ok {
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					out = append(out, trimmed)
				}
			}
			return out
		}
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		text, ok := item.(string)
		if !ok {
			continue
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

const presentationHTMLTemplate = `<!doctype html>
<html lang="{{if .Language}}{{.Language}}{{else}}en{{end}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root {
      --paper: #f6f8f5;
      --ink: #111418;
      --muted: #53606c;
      --soft: #e5ebe7;
      --line: rgba(17, 20, 24, 0.14);
      --surface: rgba(255, 255, 255, 0.82);
      --surface-strong: #ffffff;
      --teal: #007c78;
      --coral: #d94f38;
      --gold: #a56f00;
      --indigo: #3544a3;
      --shadow: 0 26px 90px rgba(17, 20, 24, 0.24);
    }
    * { box-sizing: border-box; }
    html, body { margin: 0; min-height: 100%; background: #c8d0d7; color: var(--ink); }
    body {
      min-height: 100dvh;
      display: grid;
      place-items: center;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      letter-spacing: 0;
    }
    .deck-shell {
      position: relative;
      width: min(100vw, calc(100dvh * 16 / 9));
      aspect-ratio: 16 / 9;
      background: var(--paper);
      overflow: hidden;
      box-shadow: var(--shadow);
    }
    .deck-shell::before {
      content: "";
      position: absolute;
      inset: 0;
      background:
        linear-gradient(90deg, rgba(17, 20, 24, 0.05) 1px, transparent 1px) 0 0 / 4rem 4rem,
        linear-gradient(0deg, rgba(17, 20, 24, 0.04) 1px, transparent 1px) 0 0 / 4rem 4rem;
      pointer-events: none;
    }
    .theme-studio {
      --paper: #f7f5f0;
      --ink: #141210;
      --muted: #625c55;
      --soft: #ebe4dc;
      --teal: #0b7f68;
      --coral: #c94c3d;
      --gold: #9c7412;
      --indigo: #3d4a8c;
    }
    .theme-signal {
      --paper: #f4f7fb;
      --ink: #0f1720;
      --muted: #536273;
      --soft: #dfe8f2;
      --teal: #006f8f;
      --coral: #c94a31;
      --gold: #9a7500;
      --indigo: #273f9b;
    }
    .slide {
      position: absolute;
      inset: 0;
      display: none;
      grid-template-rows: auto minmax(0, 1fr) auto;
      gap: 1.45rem;
      padding: 3rem 4.25rem 2.35rem;
      isolation: isolate;
    }
    .slide.active { display: grid; }
    .slide::before {
      content: "";
      position: absolute;
      inset: 0 auto 0 0;
      width: 0.75rem;
      background: var(--accent);
      z-index: -1;
    }
    .slide::after {
      content: attr(data-page);
      position: absolute;
      right: 3.85rem;
      top: 1.7rem;
      color: rgba(17, 20, 24, 0.06);
      font-size: 9.5rem;
      line-height: 0.8;
      font-weight: 900;
      font-variant-numeric: tabular-nums;
      pointer-events: none;
      z-index: -1;
    }
    .slide-top,
    .footer {
      position: relative;
      z-index: 1;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 1rem;
      color: var(--muted);
      font-size: 0.78rem;
      font-weight: 700;
    }
    .slide-top {
      min-height: 1.5rem;
      padding-bottom: 0.7rem;
      border-bottom: 1px solid rgba(17, 20, 24, 0.12);
    }
    .footer {
      padding-top: 0.75rem;
      border-top: 1px solid rgba(17, 20, 24, 0.12);
    }
    .deck-label {
      max-width: 34rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .kicker {
      color: var(--accent);
      font-size: 0.78rem;
      text-transform: uppercase;
      font-weight: 850;
      letter-spacing: 0;
    }
    h1, h2, h3, p { margin: 0; }
    h1 {
      max-width: 9.6ch;
      font-size: 5.7rem;
      line-height: 0.92;
      font-weight: 900;
      letter-spacing: 0;
    }
    h2 {
      max-width: 12.5ch;
      font-size: 4.15rem;
      line-height: 0.98;
      font-weight: 900;
      letter-spacing: 0;
    }
    h3 {
      font-size: 1.16rem;
      line-height: 1.16;
      font-weight: 850;
      letter-spacing: 0;
    }
    p, li {
      font-size: 1.05rem;
      line-height: 1.46;
    }
    .subtitle {
      max-width: 43rem;
      color: var(--muted);
      font-size: 1.32rem;
      line-height: 1.38;
      font-weight: 520;
    }
    .body {
      max-width: 48rem;
      color: var(--muted);
    }
    .content {
      position: relative;
      z-index: 1;
      display: grid;
      align-content: center;
      gap: 1.25rem;
      min-height: 0;
    }
    .cover .content,
    .section .content,
    .closing .content {
      grid-template-columns: repeat(12, minmax(0, 1fr));
      align-content: end;
      column-gap: 1.4rem;
      padding-bottom: 2.25rem;
    }
    .cover h1,
    .section h2,
    .closing h2 {
      grid-column: 1 / 8;
    }
    .cover .subtitle,
    .section .subtitle,
    .closing .subtitle,
    .closing .body {
      grid-column: 1 / 7;
    }
    .cover::after {
      right: 3rem;
      bottom: 2rem;
      top: auto;
      font-size: 18rem;
      color: rgba(17, 20, 24, 0.055);
    }
    .section,
    .closing {
      --ink: #f8fafc;
      --muted: #c8d1dc;
      --surface: rgba(255, 255, 255, 0.08);
      color: var(--ink);
      background:
        linear-gradient(90deg, rgba(255, 255, 255, 0.08) 1px, transparent 1px) 0 0 / 4rem 4rem,
        linear-gradient(0deg, rgba(255, 255, 255, 0.06) 1px, transparent 1px) 0 0 / 4rem 4rem,
        #101820;
    }
    .section::after,
    .closing::after {
      color: rgba(255, 255, 255, 0.075);
    }
    .section .slide-top,
    .closing .slide-top,
    .section .footer,
    .closing .footer {
      color: rgba(248, 250, 252, 0.72);
      border-color: rgba(248, 250, 252, 0.16);
    }
    .section .subtitle,
    .closing .subtitle,
    .closing .body {
      color: rgba(248, 250, 252, 0.78);
    }
    .title-bullets .content {
      grid-template-columns: minmax(0, 0.88fr) minmax(0, 1.12fr);
      column-gap: 3rem;
      align-items: center;
    }
    .title-bullets h2,
    .title-bullets .body {
      grid-column: 1;
    }
    .title-bullets .bullets {
      grid-column: 2;
      grid-row: 1 / span 2;
    }
    .two-column .content,
    .comparison .content,
    .timeline .content,
    .metrics .content {
      align-content: center;
      gap: 1.55rem;
    }
    .layout-grid {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 1.2rem;
      align-items: stretch;
    }
    .panel {
      position: relative;
      min-height: 10.25rem;
      padding: 1.18rem 1.2rem 1.12rem;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--surface);
      display: grid;
      align-content: start;
      gap: 0.62rem;
      box-shadow: 0 14px 42px rgba(17, 20, 24, 0.08);
      overflow: hidden;
    }
    .panel::before {
      content: "";
      position: absolute;
      left: 0;
      right: 0;
      top: 0;
      height: 0.34rem;
      background: var(--accent);
    }
    .panel p {
      color: var(--muted);
      font-size: 0.98rem;
      line-height: 1.42;
    }
    .bullets {
      display: grid;
      gap: 0.72rem;
      margin: 0;
      padding: 0;
      list-style: none;
      max-width: 52rem;
    }
    .bullets li {
      display: grid;
      grid-template-columns: 2rem minmax(0, 1fr);
      gap: 0.8rem;
      align-items: start;
      padding: 0.9rem 1rem;
      border: 1px solid rgba(17, 20, 24, 0.11);
      border-radius: 8px;
      background: rgba(255, 255, 255, 0.66);
      color: var(--muted);
      box-shadow: 0 10px 32px rgba(17, 20, 24, 0.06);
    }
    .bullets li::before {
      content: "";
      width: 0.82rem;
      height: 0.82rem;
      margin-top: 0.34rem;
      border-radius: 50%;
      border: 2px solid var(--accent);
      background: transparent;
    }
    .bullets li.visible {
      color: var(--ink);
      border-color: rgba(17, 20, 24, 0.2);
      background: var(--surface-strong);
    }
    .bullets li.visible::before {
      background: var(--accent);
    }
    .metric-grid {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 1rem;
    }
    .metric {
      min-height: 13.2rem;
      align-content: space-between;
    }
    .metric .value {
      color: var(--accent);
      font-size: 4.65rem;
      line-height: 0.9;
      font-weight: 900;
      font-variant-numeric: tabular-nums;
    }
    .timeline-list {
      position: relative;
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(9.4rem, 1fr));
      gap: 0.9rem;
      padding-top: 1rem;
    }
    .timeline-list::before {
      content: "";
      position: absolute;
      left: 0.45rem;
      right: 0.45rem;
      top: 1.75rem;
      height: 2px;
      background: rgba(17, 20, 24, 0.18);
    }
    .timeline-list .panel {
      min-height: 12rem;
      padding-top: 2.25rem;
      background: rgba(255, 255, 255, 0.76);
    }
    .timeline-list .panel::after {
      content: "";
      position: absolute;
      left: 1.2rem;
      top: 1.43rem;
      width: 0.74rem;
      height: 0.74rem;
      border-radius: 50%;
      background: var(--accent);
      box-shadow: 0 0 0 0.35rem var(--paper);
    }
    .image-hero .layout-grid {
      grid-template-columns: minmax(0, 0.86fr) minmax(0, 1.14fr);
      align-items: center;
      gap: 2rem;
    }
    .image-hero .content .content {
      align-content: center;
    }
    .image-wrap {
      height: 26rem;
      margin: 0;
      border-radius: 8px;
      overflow: hidden;
      border: 1px solid var(--line);
      background: var(--soft);
      box-shadow: 0 20px 52px rgba(17, 20, 24, 0.16);
    }
    .image-wrap img {
      width: 100%;
      height: 100%;
      object-fit: cover;
      display: block;
    }
    .quote .content {
      align-content: center;
      max-width: 56rem;
    }
    .quote-mark {
      color: var(--accent);
      font-size: 7rem;
      line-height: 0.7;
      font-weight: 900;
    }
    .quote .body {
      color: var(--ink);
      font-size: 3.05rem;
      line-height: 1.08;
      max-width: 50rem;
      font-weight: 850;
    }
    .slide-count {
      font-variant-numeric: tabular-nums;
      white-space: nowrap;
    }
    .progress-track {
      position: absolute;
      left: 0;
      right: 0;
      bottom: 0;
      height: 5px;
      background: rgba(17, 20, 24, 0.12);
      z-index: 4;
    }
    .progress-bar {
      height: 100%;
      width: 0;
      background: var(--accent);
      transition: width 220ms ease-out;
    }
    .accent-teal { --accent: var(--teal); }
    .accent-coral { --accent: var(--coral); }
    .accent-gold { --accent: var(--gold); }
    .accent-indigo { --accent: var(--indigo); }
    [data-animate] {
      opacity: 0;
      transform: translateY(16px);
    }
    [data-animate].visible {
      opacity: 1;
      transform: translateY(0);
    }
    [data-step] {
      opacity: 1;
      transform: none;
      transition:
        opacity 220ms ease-out,
        transform 220ms ease-out,
        border-color 220ms ease-out,
        background 220ms ease-out,
        box-shadow 220ms ease-out;
    }
    [data-step].visible {
      opacity: 1;
      transform: translateY(-3px);
      box-shadow: 0 18px 48px rgba(17, 20, 24, 0.12);
    }
    @media (max-width: 900px), (max-height: 560px) {
      .slide { padding: 2rem 2.35rem 1.75rem; gap: 1rem; }
      .slide::after { right: 2.2rem; top: 1.4rem; font-size: 6.5rem; }
      h1 { font-size: 3.85rem; }
      h2 { font-size: 2.8rem; }
      h3 { font-size: 1rem; }
      p, li { font-size: 0.92rem; }
      .subtitle { font-size: 1.02rem; }
      .title-bullets .content,
      .image-hero .layout-grid {
        grid-template-columns: 1fr;
        gap: 1rem;
      }
      .title-bullets .bullets,
      .title-bullets h2,
      .title-bullets .body {
        grid-column: 1;
        grid-row: auto;
      }
      .layout-grid,
      .metric-grid,
      .timeline-list {
        grid-template-columns: 1fr;
      }
      .metric,
      .panel {
        min-height: auto;
      }
      .metric .value { font-size: 3.2rem; }
      .image-wrap { height: 15rem; }
      .quote .body { font-size: 2rem; }
    }
    @media (prefers-reduced-motion: reduce) {
      *, *::before, *::after {
        animation-duration: 1ms !important;
        transition-duration: 1ms !important;
      }
      [data-step],
      [data-animate] {
        transform: none !important;
      }
    }
    @media print {
      body { display: block; background: #fff; }
      .deck-shell { width: 100%; aspect-ratio: auto; box-shadow: none; }
      .deck-shell::before { display: none; }
      .slide {
        position: relative;
        display: grid !important;
        width: 100%;
        height: 100vh;
        page-break-after: always;
      }
      .progress-track { display: none; }
      [data-step],
      [data-animate] {
        opacity: 1 !important;
        transform: none !important;
      }
    }
  </style>
</head>
<body>
  <main class="deck-shell theme-{{.Theme}}" aria-label="{{.Title}}">
    {{range .Slides}}
    <section class="slide {{.Layout}} accent-{{.Accent}}" data-page="{{printf "%02d" .Number}}" data-animation="{{.Animation}}" aria-label="{{.Title}}">
      <div class="slide-top"><span class="kicker">{{printf "%02d" .Number}} / {{printf "%02d" (len $.Slides)}}</span><span class="deck-label">{{$.Title}}</span></div>
      <div class="content">
        {{if eq .Layout "cover"}}
          <h1 data-animate>{{.Title}}</h1>
          {{if .Subtitle}}<p class="subtitle" data-animate>{{.Subtitle}}</p>{{end}}
        {{else if eq .Layout "section"}}
          <h2 data-animate>{{.Title}}</h2>
          {{if .Subtitle}}<p class="subtitle" data-animate>{{.Subtitle}}</p>{{end}}
        {{else if eq .Layout "title-bullets"}}
          <h2 data-animate>{{.Title}}</h2>
          {{if .Body}}<p class="body" data-animate>{{.Body}}</p>{{end}}
          <ul class="bullets">
            {{range .Bullets}}<li data-step>{{.}}</li>{{end}}
          </ul>
        {{else if eq .Layout "two-column"}}
          <h2 data-animate>{{.Title}}</h2>
          <div class="layout-grid">
            {{range .Items}}<article class="panel" data-step>{{if .Label}}<div class="kicker">{{.Label}}</div>{{end}}<h3>{{.Title}}</h3>{{if .Body}}<p>{{.Body}}</p>{{end}}</article>{{end}}
          </div>
        {{else if eq .Layout "image-hero"}}
          <div class="layout-grid">
            <div class="content">
              <h2 data-animate>{{.Title}}</h2>
              {{if .Body}}<p class="body" data-animate>{{.Body}}</p>{{end}}
            </div>
            {{if .Image}}<figure class="image-wrap" data-animate><img src="{{.Image.ResolvedSrc}}" alt="{{.Image.Alt}}"></figure>{{end}}
          </div>
        {{else if eq .Layout "comparison"}}
          <h2 data-animate>{{.Title}}</h2>
          <div class="layout-grid">
            {{range .Items}}<article class="panel" data-step>{{if .Label}}<div class="kicker">{{.Label}}</div>{{end}}<h3>{{.Title}}</h3>{{if .Body}}<p>{{.Body}}</p>{{end}}</article>{{end}}
          </div>
        {{else if eq .Layout "timeline"}}
          <h2 data-animate>{{.Title}}</h2>
          <div class="timeline-list">
            {{range .Items}}<article class="panel" data-step>{{if .Label}}<div class="kicker">{{.Label}}</div>{{end}}<h3>{{.Title}}</h3>{{if .Body}}<p>{{.Body}}</p>{{end}}</article>{{end}}
          </div>
        {{else if eq .Layout "metrics"}}
          <h2 data-animate>{{.Title}}</h2>
          <div class="metric-grid">
            {{range .Items}}<article class="panel metric" data-step>{{if .Value}}<div class="value">{{.Value}}</div>{{end}}<h3>{{.Title}}</h3>{{if .Body}}<p>{{.Body}}</p>{{end}}</article>{{end}}
          </div>
        {{else if eq .Layout "quote"}}
          <div class="quote-mark" aria-hidden="true">"</div>
          <p class="body" data-animate>{{.Body}}</p>
          {{if .Title}}<p class="subtitle" data-animate>{{.Title}}</p>{{end}}
        {{else if eq .Layout "closing"}}
          <h2 data-animate>{{.Title}}</h2>
          {{if .Subtitle}}<p class="subtitle" data-animate>{{.Subtitle}}</p>{{end}}
          {{if .Body}}<p class="body" data-animate>{{.Body}}</p>{{end}}
        {{end}}
      </div>
      <div class="footer"><span class="deck-label">{{$.Title}}</span><span class="slide-count">{{printf "%02d" .Number}} / {{printf "%02d" (len $.Slides)}}</span></div>
    </section>
    {{end}}
    <div class="progress-track" aria-hidden="true"><div class="progress-bar"></div></div>
  </main>
  <script src="assets/anime.umd.min.js"></script>
  <script>
    (function () {
      const slides = Array.from(document.querySelectorAll('.slide'));
      const progress = document.querySelector('.progress-bar');
      const prefersReduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      let index = Math.max(0, Math.min(slides.length - 1, Number((location.hash || '#1').slice(1)) - 1 || 0));

      function activeSteps(slide) {
        return Array.from(slide.querySelectorAll('[data-step]'));
      }
      function revealNow(targets) {
        targets.forEach((el) => el.classList.add('visible'));
      }
      function countValues(targets) {
        targets.forEach((el) => {
          const value = el.querySelector('.value');
          if (!value) return;
          const finalText = value.dataset.finalText || value.textContent;
          value.dataset.finalText = finalText;
          const match = finalText.match(/[-+]?\d+(\.\d+)?/);
          if (!match) return;
          const end = Number(match[0]);
          const state = { n: 0 };
          window.anime.animate(state, {
            n: end,
            duration: 720,
            ease: 'out(3)',
            onUpdate: () => { value.textContent = finalText.replace(match[0], Math.round(state.n).toString()); },
            onComplete: () => { value.textContent = finalText; }
          });
        });
      }
      function animateIn(targets, preset) {
        if (!targets.length) return;
        if (preset === 'none' || prefersReduced || !window.anime || !window.anime.animate) {
          revealNow(targets);
          return;
        }
        const stepTargets = targets.filter((el) => el.hasAttribute('data-step'));
        const introTargets = targets.filter((el) => !el.hasAttribute('data-step'));
        if (preset === 'count-up') {
          countValues(stepTargets.length ? stepTargets : targets);
        }
        if (preset === 'draw-line') {
          revealNow(targets);
        }
        if (introTargets.length) {
          window.anime.animate(introTargets, {
            opacity: [0, 1],
            translateY: preset === 'scale-in' ? ['0px', '0px'] : ['18px', '0px'],
            scale: preset === 'scale-in' ? [0.96, 1] : [1, 1],
            delay: window.anime.stagger ? window.anime.stagger(55) : 0,
            duration: 500,
            ease: 'out(3)'
          });
          introTargets.forEach((el) => el.classList.add('visible'));
        }
        if (stepTargets.length) {
          stepTargets.forEach((el) => el.classList.add('visible'));
          window.anime.animate(stepTargets, {
            opacity: [0.82, 1],
            translateY: ['0px', '-3px'],
            scale: [0.99, 1],
            delay: window.anime.stagger ? window.anime.stagger(45) : 0,
            duration: 360,
            ease: 'out(3)'
          });
        }
      }
      function prepare(slide) {
        activeSteps(slide).forEach((step) => step.classList.remove('visible'));
      }
      function showSlide(nextIndex) {
        slides[index]?.classList.remove('active');
        index = Math.max(0, Math.min(slides.length - 1, nextIndex));
        const slide = slides[index];
        slide.classList.add('active');
        prepare(slide);
        const animated = Array.from(slide.querySelectorAll('[data-animate]'));
        animated.forEach((el) => el.classList.remove('visible'));
        animateIn(animated, slide.dataset.animation || 'auto');
        progress.style.width = (((index + 1) / slides.length) * 100) + '%';
        history.replaceState(null, '', '#' + (index + 1));
      }
      function next() {
        const steps = activeSteps(slides[index]);
        const pending = steps.find((step) => !step.classList.contains('visible'));
        if (pending) {
          animateIn([pending], slides[index].dataset.animation || 'stagger');
          return;
        }
        showSlide(index + 1);
      }
      function previous() {
        const steps = activeSteps(slides[index]).filter((step) => step.classList.contains('visible'));
        const last = steps[steps.length - 1];
        if (last) {
          last.classList.remove('visible');
          return;
        }
        showSlide(index - 1);
      }
      document.addEventListener('keydown', function (event) {
        if (event.key === 'ArrowRight' || event.key === 'PageDown' || event.key === ' ') { event.preventDefault(); next(); }
        if (event.key === 'ArrowLeft' || event.key === 'PageUp') { event.preventDefault(); previous(); }
        if (event.key === 'Home') { event.preventDefault(); showSlide(0); }
        if (event.key === 'End') { event.preventDefault(); showSlide(slides.length - 1); }
      });
      document.querySelector('.deck-shell').addEventListener('click', next);
      showSlide(index);
    }());
  </script>
</body>
</html>
`
