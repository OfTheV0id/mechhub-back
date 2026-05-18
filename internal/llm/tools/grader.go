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

// GraderArgs is the schema the LLM sees for grade_submission.
type GraderArgs struct {
	ImagePaths []string `json:"image_paths" description:"作业图片本地路径列表,按页码顺序"`
}

// NewGraderTool 注册 grade_submission 工具。工具内部先取/算 OCR 结果,
// 然后直接调 Gemini structured output 生成 GradingOutput。
func NewGraderTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "grade_submission",
		Description: "对一批作业图片做完整批改,返回结构化 GradingOutput(整体分 + 每页步骤分析 + 错误更正)。工具内部按需要调用 OCR(复用 session 缓存),你不必先手动调 ocr_images_cached。",
	}, runGrade)
}

func runGrade(tctx tool.Context, args GraderArgs) (schemas.GradingOutput, error) {
	if len(args.ImagePaths) == 0 {
		return schemas.GradingOutput{}, fmt.Errorf("image_paths is empty")
	}

	state := tctx.State()
	cacheKey := CacheKey(args.ImagePaths)
	var ocr *OCRDocument
	if hit, ok := ReadCachedOCR(state, cacheKey); ok {
		ocr = hit
	} else {
		doc, err := ProcessImages(tctx, args.ImagePaths)
		if err != nil {
			return schemas.GradingOutput{}, fmt.Errorf("ocr: %w", err)
		}
		if err := WriteCachedOCR(state, cacheKey, doc); err != nil {
			return schemas.GradingOutput{}, fmt.Errorf("ocr cache write: %w", err)
		}
		ocr = doc
	}

	return callGeminiGrader(tctx, args.ImagePaths, ocr)
}

func callGeminiGrader(ctx context.Context, imagePaths []string, ocr *OCRDocument) (schemas.GradingOutput, error) {
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

	parts := make([]*genai.Part, 0, len(imagePaths)+1)
	for _, p := range imagePaths {
		data, err := os.ReadFile(p)
		if err != nil {
			return schemas.GradingOutput{}, fmt.Errorf("read %s: %w", p, err)
		}
		mime, err := mimeForPath(p)
		if err != nil {
			return schemas.GradingOutput{}, err
		}
		parts = append(parts, &genai.Part{InlineData: &genai.Blob{Data: data, MIMEType: mime}})
	}
	parts = append(parts, &genai.Part{Text: prompts.BuildGradingPrompt(ocr, len(imagePaths))})

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
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  os.Getenv("GEMINI_API_KEY"),
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, err
	}
	cachedGemini = c
	return c, nil
}
