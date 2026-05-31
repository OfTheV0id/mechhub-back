package course

import (
	"encoding/json"
	"strings"
)

// Assessment 是节点的判定规格,按 kind 用不同字段:
//   - theory / quiz:Questions(选择题)
//   - workshop:Steps(逐步讲解 + 每步小测)
//   - lab:FBD(力学题,Phase C)
//
// 含正确答案,只存后端;学生视角经 studentAssessment 剥除。
type Assessment struct {
	Questions []Question `json:"questions,omitempty"`
	Steps     []Step     `json:"steps,omitempty"`
	FBD       *FBDSpec   `json:"fbd,omitempty"`
}

// FBDSpec 自由体图题:刚体上的支座 + 已知载荷。标准答案(各支座反力)由静力学
// 求解器现算,不存库、不下发;spec 本身公开给学生(题面)。
type FBDSpec struct {
	Supports  []FBDSupport `json:"supports"`
	Loads     []FBDLoad    `json:"loads"`
	Tolerance float64      `json:"tolerance,omitempty"` // 相对容差,默认 0.05
}

type FBDSupport struct {
	ID    string  `json:"id"`
	Type  string  `json:"type"` // "pin" | "roller"
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Angle float64 `json:"angle"` // roller 法向角(度),默认 90(朝上)
}

type FBDLoad struct {
	ID string  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	Fx float64 `json:"fx"`
	Fy float64 `json:"fy"`
}

type FBDVec struct {
	Fx float64 `json:"fx"`
	Fy float64 `json:"fy"`
}

type Question struct {
	ID          string    `json:"id"`
	Prompt      string    `json:"prompt"`
	Type        string    `json:"type"` // "single" | "multi"
	Options     []QOption `json:"options"`
	Correct     []string  `json:"correct,omitempty"` // 仅后端
	Explanation string    `json:"explanation,omitempty"`
}

type QOption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Step workshop 的一步:讲解内容 + 一组小测(全对才算过这步;无题=纯讲解步,免检)。
type Step struct {
	ID        string          `json:"id"`
	Content   json.RawMessage `json:"content"`
	Questions []Question      `json:"questions,omitempty"`
}

func parseAssessment(raw string) *Assessment {
	t := strings.TrimSpace(raw)
	if t == "" || t == "null" {
		return nil
	}
	var a Assessment
	if err := json.Unmarshal([]byte(t), &a); err != nil {
		return nil
	}
	return &a
}

// assessmentGradable 节点是否计入进度 / 可判通过。
func assessmentGradable(kind, raw string) bool {
	a := parseAssessment(raw)
	if a == nil {
		return false
	}
	switch kind {
	case KindTheory, KindQuiz:
		return len(a.Questions) > 0
	case KindWorkshop:
		return len(a.Steps) > 0
	case KindLab:
		return a.FBD != nil && len(a.FBD.Supports) > 0
	default:
		return false
	}
}

// studentAssessment 给学生看的版本:剥掉所有题目的 correct 与 explanation(顶层 + 步内)。
func studentAssessment(raw string) json.RawMessage {
	a := parseAssessment(raw)
	if a == nil {
		return json.RawMessage("null")
	}
	stripQuestions(a.Questions)
	for i := range a.Steps {
		stripQuestions(a.Steps[i].Questions)
	}
	out, err := json.Marshal(a)
	if err != nil {
		return json.RawMessage("null")
	}
	return out
}

func stripQuestions(qs []Question) {
	for i := range qs {
		qs[i].Correct = nil
		qs[i].Explanation = ""
	}
}

// gradeQuestions 判一组选择题:每题答案集合与 correct 相等才对;全对且至少一题 = passed。
func gradeQuestions(questions []Question, answers map[string][]string) (map[string]bool, map[string]string, bool) {
	results := map[string]bool{}
	explanations := map[string]string{}
	if len(questions) == 0 {
		return results, explanations, false
	}
	passed := true
	for _, q := range questions {
		ok := sameSet(answers[q.ID], q.Correct)
		results[q.ID] = ok
		if q.Explanation != "" {
			explanations[q.ID] = q.Explanation
		}
		if !ok {
			passed = false
		}
	}
	return results, explanations, passed
}

func gradeMCQ(raw string, answers map[string][]string) (map[string]bool, map[string]string, bool) {
	a := parseAssessment(raw)
	if a == nil {
		return map[string]bool{}, map[string]string{}, false
	}
	return gradeQuestions(a.Questions, answers)
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(b))
	for _, x := range b {
		seen[x] = true
	}
	for _, x := range a {
		if !seen[x] {
			return false
		}
	}
	return true
}

// ---- workshop 逐步进度(存 NodeProgress.Detail)----

type progressDetail struct {
	Steps map[string]bool `json:"steps,omitempty"`
}

func parseProgressDetail(raw string) progressDetail {
	t := strings.TrimSpace(raw)
	if t != "" && t != "null" {
		var p progressDetail
		if json.Unmarshal([]byte(t), &p) == nil && p.Steps != nil {
			return p
		}
	}
	return progressDetail{Steps: map[string]bool{}}
}

func (d progressDetail) marshal() string {
	b, err := json.Marshal(d)
	if err != nil {
		return "null"
	}
	return string(b)
}

// allStepsPassed 所有「有题」的步骤都已通过,则整节通过(纯讲解步免检)。
func allStepsPassed(a *Assessment, d progressDetail) bool {
	if a == nil || len(a.Steps) == 0 {
		return false
	}
	for _, st := range a.Steps {
		if len(st.Questions) == 0 {
			continue
		}
		if !d.Steps[st.ID] {
			return false
		}
	}
	return true
}
