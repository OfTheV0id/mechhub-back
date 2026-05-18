// Package schemas defines GradingOutput — the structured schema the
// grade_submission tool asks Gemini to produce. Mirrors the Pydantic
// model in mechhub-agent/mechhub_agent/schemas/grading.py 1:1.
package schemas

import "google.golang.org/genai"

type NormalizedVertex struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type BoundingPoly struct {
	NormalizedVertices []NormalizedVertex `json:"normalizedVertices"`
}

type ContentBlock struct {
	ID          string       `json:"id"`
	Type        string       `json:"type"` // question | answer | explanation
	Text        string       `json:"text"`
	SourceText  string       `json:"sourceText"`
	BBox        BoundingPoly `json:"bbox"`
	Orientation string       `json:"orientation"`
	Confidence  float64      `json:"confidence"`
}

type Formula struct {
	ID          string       `json:"id"`
	LaTeX       string       `json:"latex"`
	SourceText  string       `json:"sourceText"`
	BBox        BoundingPoly `json:"bbox"`
	Orientation string       `json:"orientation"`
	Confidence  float64      `json:"confidence"`
}

type Correction struct {
	Original  string `json:"original"`
	Corrected string `json:"corrected"`
	Reason    string `json:"reason"`
}

type StepEvaluation struct {
	StepLabel   string       `json:"stepLabel"`
	Description string       `json:"description"`
	Verdict     string       `json:"verdict"` // correct | partially_correct | incorrect
	Score       float64      `json:"score"`
	Comment     string       `json:"comment"`
	RelatedIDs  []string     `json:"relatedIds"`
	Corrections []Correction `json:"corrections,omitempty"`
}

type PageGrading struct {
	Applicable      bool             `json:"applicable"`
	StepEvaluations []StepEvaluation `json:"stepEvaluations"`
}

type PageResult struct {
	PageNumber          int            `json:"pageNumber"`
	DocumentType        string         `json:"documentType"` // question_page | student_answer | mixed_question_answer | unknown
	Summary             string         `json:"summary"`
	ContentBlocks       []ContentBlock `json:"contentBlocks"`
	Formulas            []Formula      `json:"formulas"`
	SuspectedOcrIssues  []string       `json:"suspectedOcrIssues"`
	Grading             PageGrading    `json:"grading"`
	NeedsHumanReview    bool           `json:"needsHumanReview"`
}

type GradingOutput struct {
	OverallScore   float64      `json:"overallScore"`
	OverallComment string       `json:"overallComment"`
	Pages          []PageResult `json:"pages"`
}

// Schema 返回 Gemini structured-output 需要的 *genai.Schema,字段命名与
// 上面 struct 的 json tag 一一对齐。
func Schema() *genai.Schema {
	vertex := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"x": {Type: genai.TypeNumber},
			"y": {Type: genai.TypeNumber},
		},
		Required: []string{"x", "y"},
	}
	bbox := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"normalizedVertices": {Type: genai.TypeArray, Items: vertex},
		},
		Required: []string{"normalizedVertices"},
	}
	contentBlock := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"id":          {Type: genai.TypeString},
			"type":        {Type: genai.TypeString, Enum: []string{"question", "answer", "explanation"}},
			"text":        {Type: genai.TypeString},
			"sourceText":  {Type: genai.TypeString},
			"bbox":        bbox,
			"orientation": {Type: genai.TypeString},
			"confidence":  {Type: genai.TypeNumber},
		},
		Required: []string{"id", "type", "text", "sourceText", "bbox", "orientation", "confidence"},
	}
	formula := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"id":          {Type: genai.TypeString},
			"latex":       {Type: genai.TypeString},
			"sourceText":  {Type: genai.TypeString},
			"bbox":        bbox,
			"orientation": {Type: genai.TypeString},
			"confidence":  {Type: genai.TypeNumber},
		},
		Required: []string{"id", "latex", "sourceText", "bbox", "orientation", "confidence"},
	}
	correction := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"original":  {Type: genai.TypeString},
			"corrected": {Type: genai.TypeString},
			"reason":    {Type: genai.TypeString},
		},
		Required: []string{"original", "corrected", "reason"},
	}
	stepEval := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"stepLabel":   {Type: genai.TypeString},
			"description": {Type: genai.TypeString},
			"verdict":     {Type: genai.TypeString, Enum: []string{"correct", "partially_correct", "incorrect"}},
			"score":       {Type: genai.TypeNumber},
			"comment":     {Type: genai.TypeString},
			"relatedIds":  {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
			"corrections": {Type: genai.TypeArray, Items: correction},
		},
		Required: []string{"stepLabel", "description", "verdict", "score", "comment", "relatedIds"},
	}
	pageGrading := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"applicable":      {Type: genai.TypeBoolean},
			"stepEvaluations": {Type: genai.TypeArray, Items: stepEval},
		},
		Required: []string{"applicable", "stepEvaluations"},
	}
	pageResult := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"pageNumber":          {Type: genai.TypeInteger},
			"documentType":        {Type: genai.TypeString, Enum: []string{"question_page", "student_answer", "mixed_question_answer", "unknown"}},
			"summary":             {Type: genai.TypeString},
			"contentBlocks":       {Type: genai.TypeArray, Items: contentBlock},
			"formulas":            {Type: genai.TypeArray, Items: formula},
			"suspectedOcrIssues":  {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
			"grading":             pageGrading,
			"needsHumanReview":    {Type: genai.TypeBoolean},
		},
		Required: []string{"pageNumber", "documentType", "summary", "contentBlocks", "formulas", "suspectedOcrIssues", "grading", "needsHumanReview"},
	}
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"overallScore":   {Type: genai.TypeNumber},
			"overallComment": {Type: genai.TypeString},
			"pages":          {Type: genai.TypeArray, Items: pageResult},
		},
		Required: []string{"overallScore", "overallComment", "pages"},
	}
}
