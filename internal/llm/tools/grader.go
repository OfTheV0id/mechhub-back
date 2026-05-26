package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
	"google.golang.org/genai"

	"mechhub-back/internal/llm/prompts"
	"mechhub-back/internal/llm/schemas"
)

// GraderArgs is the schema the LLM sees for grade_with_ocr.
type GraderArgs struct {
	ImageIndices []int `json:"image_indices" description:"图片索引列表,必须与之前 ocr_images_cached 调用的 image_indices 完全一致(例如 [0, 1, 2])"`
}

// NewGraderTool 注册 grade_with_ocr 工具。**前置条件**:必须先对同一批
// image_indices 调用过 ocr_images_cached,否则直接返回 error 让 LLM 自己补一步。
// 与 mechhub-agent Python 版的两步 SOP 对齐。
func NewGraderTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "grade_with_ocr",
		Description: "对一批作业图片做完整批改,返回结构化 GradingOutput(整体分 + 每页步骤分析 + 错误更正)。**前置条件**:必须先对同一批 image_indices 调用过 ocr_images_cached(本工具只读 session 中的 OCR 缓存,不会自己 OCR);未命中缓存会返回 error,届时请补一次 ocr_images_cached 再重试。",
	}, runGrade)
}

func runGrade(tctx tool.Context, args GraderArgs) (schemas.GradingOutput, error) {
	if len(args.ImageIndices) == 0 {
		return schemas.GradingOutput{}, fmt.Errorf("image_indices is empty")
	}

	state := tctx.State()

	sid, err := state.Get("_solochat_session")
	if err != nil {
		return schemas.GradingOutput{}, fmt.Errorf("session not initialized: %w", err)
	}
	sessionID, ok := sid.(string)
	if !ok {
		return schemas.GradingOutput{}, fmt.Errorf("invalid session id type")
	}

	imgs, err := GetSessionImages(sessionID, args.ImageIndices)
	if err != nil {
		return schemas.GradingOutput{}, err
	}

	cacheKey := cacheKeyFromIndices(args.ImageIndices)
	hit, ok := ReadCachedOCR(state, cacheKey)
	if !ok {
		return schemas.GradingOutput{}, fmt.Errorf("OCR 缓存未命中:请先用相同的 image_indices 调用 ocr_images_cached,再调本工具")
	}
	out, err := callGeminiGrader(tctx, imgs, hit)
	if err != nil {
		return out, err
	}
	// 回填图片引用,前端据此直接渲染 OCR 详情页,无需根据"上一条消息"推导。
	refs := make([]schemas.ImageRef, len(imgs))
	for i, img := range imgs {
		refs[i] = schemas.ImageRef{
			Index:        args.ImageIndices[i],
			AttachmentID: img.AttachmentID,
			OriginalName: img.OrigName,
			MimeType:     img.MimeType,
			URL:          img.URL,
		}
	}
	out.ImageRefs = refs
	return out, nil
}

func callGeminiGrader(ctx context.Context, images []CachedImage, ocr *OCRDocument) (schemas.GradingOutput, error) {
	client, err := geminiClient(ctx)
	if err != nil {
		return schemas.GradingOutput{}, err
	}
	model := os.Getenv("GEMINI_GRADER_MODEL")
	if model == "" {
		model = os.Getenv("GEMINI_MODEL")
	}
	if model == "" {
		model = "gemini-2.5-flash"
	}

	parts := make([]*genai.Part, 0, len(images)+1)
	for _, img := range images {
		parts = append(parts, &genai.Part{InlineData: &genai.Blob{Data: img.Data, MIMEType: img.MimeType}})
	}
	parts = append(parts, &genai.Part{Text: prompts.BuildGradingPrompt(ocr, len(images))})

	resp, err := client.Models.GenerateContent(ctx, model, []*genai.Content{
		{Role: "user", Parts: parts},
	}, &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   schemas.Schema(),
	})
	if err != nil {
		return schemas.GradingOutput{}, fmt.Errorf("gemini generate: %w", err)
	}

	jsonText := resp.Text()
	if jsonText == "" {
		return schemas.GradingOutput{}, fmt.Errorf("gemini returned empty response")
	}
	var out schemas.GradingOutput
	if err := json.Unmarshal([]byte(jsonText), &out); err != nil {
		return schemas.GradingOutput{}, fmt.Errorf("parse grading json: %w", err)
	}
	return out, nil
}

var (
	geminiMu     sync.Mutex
	cachedGemini *genai.Client
)

func geminiClient(ctx context.Context) (*genai.Client, error) {
	geminiMu.Lock()
	defer geminiMu.Unlock()
	if cachedGemini != nil {
		return cachedGemini, nil
	}
	clientCfg := &genai.ClientConfig{
		APIKey:  os.Getenv("GEMINI_API_KEY"),
		Backend: genai.BackendGeminiAPI,
	}
	if base := os.Getenv("GEMINI_BASE_URL"); base != "" {
		clientCfg.HTTPOptions.BaseURL = base
	}
	c, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return nil, err
	}
	cachedGemini = c
	return c, nil
}
