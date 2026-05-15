package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	documentai "cloud.google.com/go/documentai/apiv1"
	"cloud.google.com/go/documentai/apiv1/documentaipb"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	fieldmaskpb "google.golang.org/genproto/protobuf/field_mask"
)

// OCRArgs is the schema the LLM sees for ocr_images_cached.
type OCRArgs struct {
	ImagePaths []string `json:"image_paths" description:"图片本地路径列表,按页码顺序传入"`
}

// OCRResult 返给 LLM 的精简摘要;完整 OCR JSON 存到 session.state["ocr_cache"]。
type OCRResult struct {
	OK            bool   `json:"ok"`
	ImageCount    int    `json:"image_count"`
	PageCount     int    `json:"page_count"`
	TextChars     int    `json:"text_chars"`
	HasFormulas   bool   `json:"has_formulas"`
	Preview       string `json:"preview"`
	CachedToState bool   `json:"cached_to_state"`
}

// OCRDocument 是完整 OCR JSON 的 Go 表示(供 grader 工具复用)。
type OCRDocument struct {
	Images []string  `json:"images"`
	Text   string    `json:"text"`
	Pages  []OCRPage `json:"pages"`
}

type OCRPage struct {
	PageNumber         int              `json:"pageNumber"`
	Paragraphs         []OCRParagraph   `json:"paragraphs"`
	VisualElements     []OCRVisualElem  `json:"visualElements"`
	ImageQualityScores *OCRImageQuality `json:"imageQualityScores,omitempty"`
}

type OCRParagraph struct {
	Text         string        `json:"text"`
	Confidence   float64       `json:"confidence,omitempty"`
	BoundingPoly *OCRBoundPoly `json:"boundingPoly,omitempty"`
	Orientation  string        `json:"orientation,omitempty"`
}

type OCRVisualElem struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	Confidence   float64       `json:"confidence,omitempty"`
	BoundingPoly *OCRBoundPoly `json:"boundingPoly,omitempty"`
	Orientation  string        `json:"orientation,omitempty"`
}

type OCRBoundPoly struct {
	NormalizedVertices []OCRVertex `json:"normalizedVertices"`
}

type OCRVertex struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

type OCRImageQuality struct {
	QualityScore    float32     `json:"qualityScore"`
	DetectedDefects []OCRDefect `json:"detectedDefects,omitempty"`
}

type OCRDefect struct {
	Type       string  `json:"type"`
	Confidence float32 `json:"confidence,omitempty"`
}

// NewOCRTool 注册 ocr_images_cached 工具。
func NewOCRTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "ocr_images_cached",
		Description: "对图片做 OCR 文字识别。完整 OCR JSON 缓存到 session state['ocr_cache'][cache_key];返回值仅是精简摘要(image_count / page_count / text_chars / has_formulas / preview / cached_to_state)。grade_submission 会按需要复用同一份缓存。",
	}, runOCR)
}

func runOCR(tctx tool.Context, args OCRArgs) (OCRResult, error) {
	if len(args.ImagePaths) == 0 {
		return OCRResult{}, fmt.Errorf("image_paths is empty")
	}

	state := tctx.State()
	cacheKey := CacheKey(args.ImagePaths)

	if hit, ok := ReadCachedOCR(state, cacheKey); ok {
		return summarize(args.ImagePaths, hit), nil
	}

	doc, err := ProcessImages(tctx, args.ImagePaths)
	if err != nil {
		return OCRResult{}, err
	}
	if err := WriteCachedOCR(state, cacheKey, doc); err != nil {
		return OCRResult{}, err
	}
	return summarize(args.ImagePaths, doc), nil
}

// ProcessImages 是 OCR 核心实现,直接对外暴露(grader 工具内部会调用,
// 不走 LLM)。结果不写 session.state —— 调用方决定是否落缓存。
func ProcessImages(ctx context.Context, imagePaths []string) (*OCRDocument, error) {
	client, err := ocrClient(ctx)
	if err != nil {
		return nil, err
	}
	project := os.Getenv("GOOGLE_CLOUD_PROJECT_ID")
	location := os.Getenv("DOCUMENTAI_LOCATION")
	processor := os.Getenv("DOCUMENTAI_PROCESSOR_ID")
	if project == "" || location == "" || processor == "" {
		return nil, fmt.Errorf("missing DOCUMENTAI_* env (GOOGLE_CLOUD_PROJECT_ID / DOCUMENTAI_LOCATION / DOCUMENTAI_PROCESSOR_ID)")
	}
	name := fmt.Sprintf("projects/%s/locations/%s/processors/%s", project, location, processor)

	merged := &OCRDocument{Images: append([]string(nil), imagePaths...)}
	var allText []string
	pageCounter := 0

	for _, p := range imagePaths {
		content, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		mime, err := mimeForPath(p)
		if err != nil {
			return nil, err
		}
		req := &documentaipb.ProcessRequest{
			Name: name,
			Source: &documentaipb.ProcessRequest_RawDocument{
				RawDocument: &documentaipb.RawDocument{Content: content, MimeType: mime},
			},
			ImagelessMode: envBool("OCR_IMAGELESS_MODE"),
			ProcessOptions: &documentaipb.ProcessOptions{
				OcrConfig: &documentaipb.OcrConfig{
					EnableImageQualityScores: envBool("OCR_ENABLE_IMAGE_QUALITY"),
					PremiumFeatures: &documentaipb.OcrConfig_PremiumFeatures{
						EnableMathOcr: envBool("OCR_ENABLE_MATH_OCR"),
					},
				},
			},
			FieldMask: &fieldmaskpb.FieldMask{Paths: []string{
				"text", "mime_type",
				"pages.page_number", "pages.paragraphs",
				"pages.visual_elements", "pages.image_quality_scores",
			}},
		}
		resp, err := client.ProcessDocument(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("document ai process %s: %w", p, err)
		}
		compact := compactOCR(resp.GetDocument())
		allText = append(allText, compact.Text)
		for _, pg := range compact.Pages {
			pageCounter++
			pg.PageNumber = pageCounter
			merged.Pages = append(merged.Pages, pg)
		}
	}
	merged.Text = strings.Join(allText, "\n")
	return merged, nil
}

func compactOCR(doc *documentaipb.Document) struct {
	Text  string
	Pages []OCRPage
} {
	text := doc.GetText()
	out := struct {
		Text  string
		Pages []OCRPage
	}{Text: text}

	for i, page := range doc.GetPages() {
		p := OCRPage{PageNumber: int(page.GetPageNumber())}
		if p.PageNumber == 0 {
			p.PageNumber = i + 1
		}
		for _, para := range page.GetParagraphs() {
			layout := para.GetLayout()
			if layout == nil {
				continue
			}
			p.Paragraphs = append(p.Paragraphs, OCRParagraph{
				Text:         extractText(text, layout.GetTextAnchor()),
				Confidence:   float64(layout.GetConfidence()),
				BoundingPoly: simplifyPoly(layout.GetBoundingPoly()),
				Orientation:  layout.GetOrientation().String(),
			})
		}
		for _, ve := range page.GetVisualElements() {
			layout := ve.GetLayout()
			if layout == nil {
				continue
			}
			p.VisualElements = append(p.VisualElements, OCRVisualElem{
				Type:         ve.GetType(),
				Text:         extractText(text, layout.GetTextAnchor()),
				Confidence:   float64(layout.GetConfidence()),
				BoundingPoly: simplifyPoly(layout.GetBoundingPoly()),
				Orientation:  layout.GetOrientation().String(),
			})
		}
		if iqs := page.GetImageQualityScores(); iqs != nil {
			score := &OCRImageQuality{QualityScore: iqs.GetQualityScore()}
			for _, d := range iqs.GetDetectedDefects() {
				score.DetectedDefects = append(score.DetectedDefects, OCRDefect{
					Type: d.GetType(), Confidence: d.GetConfidence(),
				})
			}
			p.ImageQualityScores = score
		}
		out.Pages = append(out.Pages, p)
	}
	return out
}

func extractText(full string, anchor *documentaipb.Document_TextAnchor) string {
	if anchor == nil {
		return ""
	}
	var sb strings.Builder
	for _, seg := range anchor.GetTextSegments() {
		start := int(seg.GetStartIndex())
		end := int(seg.GetEndIndex())
		if start < 0 || end > len(full) || start > end {
			continue
		}
		sb.WriteString(full[start:end])
	}
	return sb.String()
}

func simplifyPoly(poly *documentaipb.BoundingPoly) *OCRBoundPoly {
	if poly == nil {
		return nil
	}
	out := &OCRBoundPoly{}
	for _, v := range poly.GetNormalizedVertices() {
		out.NormalizedVertices = append(out.NormalizedVertices, OCRVertex{X: v.GetX(), Y: v.GetY()})
	}
	return out
}

var fileIDPrefixRE = regexp.MustCompile(`^([0-9a-f-]{36}|[0-9a-f]{24})-`)

// CacheKey 用 file_id 反查路径前缀作 cache key。Round 6 引入的稳定 key
// 模式 —— 文件名前缀 = uploaded_files.id(UUID 或旧 ObjectID)。解出来才
// 能跨重启 / 跨 conversation 命中。解不出(curl 测试场景)才退化用绝对路径。
func CacheKey(paths []string) string {
	keys := make([]string, 0, len(paths))
	for _, p := range paths {
		name := filepath.Base(p)
		if m := fileIDPrefixRE.FindStringSubmatch(name); len(m) > 1 {
			keys = append(keys, m[1])
		} else {
			keys = append(keys, p)
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, ":")
}

// ReadCachedOCR / WriteCachedOCR 通过 JSON 序列化 roundtrip,绕开 GORM
// 反序列化后 entry 变成 map[string]any 的麻烦 —— 一次性 marshal+unmarshal
// 就还原回我们的 struct。
func ReadCachedOCR(state session.State, key string) (*OCRDocument, bool) {
	raw, err := state.Get("ocr_cache")
	if err != nil {
		return nil, false
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	entry, ok := m[key]
	if !ok {
		return nil, false
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return nil, false
	}
	doc := &OCRDocument{}
	if err := json.Unmarshal(b, doc); err != nil {
		return nil, false
	}
	return doc, true
}

func WriteCachedOCR(state session.State, key string, doc *OCRDocument) error {
	raw, _ := state.Get("ocr_cache")
	m, _ := raw.(map[string]any)
	if m == nil {
		m = make(map[string]any)
	}
	m[key] = doc
	return state.Set("ocr_cache", m)
}

func summarize(imagePaths []string, doc *OCRDocument) OCRResult {
	hasFormulas := false
	for _, p := range doc.Pages {
		if len(p.VisualElements) > 0 {
			hasFormulas = true
			break
		}
	}
	preview := doc.Text
	const previewLen = 200
	if len(preview) > previewLen {
		preview = preview[:previewLen]
	}
	return OCRResult{
		OK: true, ImageCount: len(imagePaths), PageCount: len(doc.Pages),
		TextChars: len(doc.Text), HasFormulas: hasFormulas,
		Preview: preview, CachedToState: true,
	}
}

func mimeForPath(p string) (string, error) {
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg", nil
	case ".png":
		return "image/png", nil
	case ".webp":
		return "image/webp", nil
	case ".pdf":
		return "application/pdf", nil
	}
	return "", fmt.Errorf("unsupported image type for OCR: %s", ext)
}

func envBool(k string) bool {
	return strings.EqualFold(os.Getenv(k), "true")
}

var (
	clientMu sync.Mutex
	cachedDP *documentai.DocumentProcessorClient
)

func ocrClient(ctx context.Context) (*documentai.DocumentProcessorClient, error) {
	clientMu.Lock()
	defer clientMu.Unlock()
	if cachedDP != nil {
		return cachedDP, nil
	}
	c, err := documentai.NewDocumentProcessorClient(ctx)
	if err != nil {
		return nil, err
	}
	cachedDP = c
	return c, nil
}
