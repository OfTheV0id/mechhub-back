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

// Step workshop 右栏「一步步指导」的一步:纯文字讲解(TipTap JSON),不带判定。
// workshop 的判定走 FBD(中间实操区),与 lab 同一条路径。
type Step struct {
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
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
	case KindWorkshop, KindLab:
		return a.FBD != nil && len(a.FBD.Supports) > 0
	default:
		return false
	}
}

// studentAssessment 给学生看的版本:剥掉选择题的 correct 与 explanation。
// workshop 的 steps 是纯文字引导,原样下发。
func studentAssessment(raw string) json.RawMessage {
	a := parseAssessment(raw)
	if a == nil {
		return json.RawMessage("null")
	}
	stripQuestions(a.Questions)
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
