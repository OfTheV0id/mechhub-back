package course

import (
	"encoding/json"
	"time"
)

const (
	// 章节节点的「模式」标签。任何节点都能再嵌子节点,Kind 只决定徽标与是否可完成。
	// section 是纯分组容器(不可完成);其余是内容节点。
	KindSection  = "section"
	KindTheory   = "theory"
	KindWorkshop = "workshop"
	KindLab      = "lab"
	KindQuiz     = "quiz"

	// 批注可见性。
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"

	// 媒体文件 kind。
	FileKindImage = "image"
	FileKindAudio = "audio"
	FileKindVideo = "video"
)

// Course 课程,扁平列出(无科目)。作者(teacher)直接编辑、自行决定是否 Published。
type Course struct {
	ID           string    `gorm:"primaryKey;type:char(36)"`
	Title        string    `gorm:"type:varchar(200);not null"`
	Description  string    `gorm:"type:varchar(2000);default:''"`
	CoverKey     string    `gorm:"type:varchar(255);default:''"`
	AuthorUserID string    `gorm:"type:char(36);not null;index:idx_author,priority:1"`
	Published    bool      `gorm:"not null;default:false;index"`
	Position     int       `gorm:"not null;default:0"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null;index:idx_author,priority:2,sort:desc"`
}

func (Course) TableName() string { return "courses" }

// CourseNode 章节节点,自引用邻接表,支持无限嵌套。ParentID 为空 = 课程顶层。
// Content 是 TipTap(ProseMirror)JSON 文档,纯容器节点为空。
type CourseNode struct {
	ID        string    `gorm:"primaryKey;type:char(36)"`
	CourseID  string    `gorm:"type:char(36);not null;index:idx_course_parent,priority:1"`
	ParentID  *string   `gorm:"type:char(36);index:idx_course_parent,priority:2"`
	Title     string    `gorm:"type:varchar(200);not null"`
	Kind      string    `gorm:"type:varchar(16);not null;default:'theory'"`
	Position  int       `gorm:"not null;default:0;index:idx_course_parent,priority:3"`
	Content   string    `gorm:"type:json"`
	// Assessment 判定规格(含答案,kind 专属):theory/quiz 的 questions、workshop 的 steps、lab 的 fbd。
	// 学生 DTO 一律剥除答案,判分只在后端。
	Assessment string    `gorm:"type:json"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

func (CourseNode) TableName() string { return "course_nodes" }

// CourseFile 课程媒体(图/音/视频),私有 OSS,走后端 stream-through。
type CourseFile struct {
	ID           string `gorm:"primaryKey;type:char(36)"`
	OwnerUserID  string `gorm:"type:char(36);not null;index"`
	OSSKey       string `gorm:"type:varchar(255)"`
	OriginalName string `gorm:"type:varchar(255)"`
	MimeType     string `gorm:"type:varchar(64)"`
	Kind         string `gorm:"type:varchar(16)"`
	Size         int64
	CreatedAt    time.Time `gorm:"not null"`
}

func (CourseFile) TableName() string { return "course_files" }

// NodeProgress 学习记录,每用户每节点一行。Completed 由学生「标记完成」置位。
type NodeProgress struct {
	ID          string `gorm:"primaryKey;type:char(36)"`
	UserID      string `gorm:"type:char(36);not null;uniqueIndex:idx_user_node,priority:1;index:idx_user_course,priority:1"`
	NodeID      string `gorm:"type:char(36);not null;uniqueIndex:idx_user_node,priority:2"`
	CourseID    string `gorm:"type:char(36);not null;index:idx_user_course,priority:2"`
	Completed   bool   `gorm:"not null;default:false"` // 语义 = 该节点「已通过」(由判定驱动,非手动)
	CompletedAt *time.Time
	// Detail workshop 逐步通过状态 {"steps":{stepId:true}};其余 kind 为空。
	Detail    string    `gorm:"type:json"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (NodeProgress) TableName() string { return "course_node_progress" }

// Annotation 划词批注,锚在某内容节点的 ProseMirror 选区 [AnchorFrom,AnchorTo)。
// Quote 存选中文本,内容编辑后可据此重锚 / 判 orphan。Visibility 控制公开或私有。
type Annotation struct {
	ID         string    `gorm:"primaryKey;type:char(36)"`
	NodeID     string    `gorm:"type:char(36);not null;index"`
	CourseID   string    `gorm:"type:char(36);not null;index"`
	UserID     string    `gorm:"type:char(36);not null;index"`
	Body       string    `gorm:"type:varchar(4000);not null"`
	Visibility string    `gorm:"type:varchar(16);not null;default:'private'"`
	AnchorFrom int       `gorm:"not null"`
	AnchorTo   int       `gorm:"not null"`
	Quote      string    `gorm:"type:varchar(2000);default:''"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

func (Annotation) TableName() string { return "course_annotations" }

// ---- DTO ----

type CourseDTO struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	CoverURL    string `json:"cover_url,omitempty"`
	AuthorID    string `json:"author_id"`
	Published   bool   `json:"published"`
	IsAuthor    bool   `json:"is_author"`
	NodeCount   int    `json:"node_count"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// CourseNodeDTO 课程详情里的章节树项(递归,不含正文 content)。
// Completable 为 true 时学生侧显示完成勾。
type CourseNodeDTO struct {
	ID          string          `json:"id"`
	ParentID    *string         `json:"parent_id"`
	Title       string          `json:"title"`
	Kind        string          `json:"kind"`
	Position    int             `json:"position"`
	Completable bool            `json:"completable"`
	Children    []CourseNodeDTO `json:"children"`
}

// CourseDetailDTO 课程详情 = 课程元信息 + 章节树。
type CourseDetailDTO struct {
	Course CourseDTO       `json:"course"`
	Nodes  []CourseNodeDTO `json:"nodes"`
}

// NodeDetailDTO 单个节点详情,content 是 TipTap JSON 文档原样透传。
type NodeDetailDTO struct {
	ID          string          `json:"id"`
	CourseID    string          `json:"course_id"`
	ParentID    *string         `json:"parent_id"`
	Title       string          `json:"title"`
	Kind        string          `json:"kind"`
	Completable bool            `json:"completable"` // = gradable(有判定才计入进度)
	Passed      bool            `json:"passed"`      // 当前用户是否已通过
	IsAuthor    bool            `json:"is_author"`
	Content     json.RawMessage `json:"content"`
	// Assessment:作者视角原样(含答案);学生视角已剥除 correct/explanation。
	Assessment json.RawMessage `json:"assessment"`
	// StepState workshop 逐步通过状态 {stepId:true}(当前用户);其余 kind 为空。
	StepState map[string]bool `json:"step_state,omitempty"`
}

type AnnotationDTO struct {
	ID         string `json:"id"`
	NodeID     string `json:"node_id"`
	UserID     string `json:"user_id"`
	AuthorName string `json:"author_name"`
	Body       string `json:"body"`
	Visibility string `json:"visibility"`
	AnchorFrom int    `json:"anchor_from"`
	AnchorTo   int    `json:"anchor_to"`
	Quote      string `json:"quote"`
	IsMine     bool   `json:"is_mine"`
	CreatedAt  string `json:"created_at"`
}

// MediaDTO 媒体附件,前端按 url+kind 渲染。
type MediaDTO struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	MimeType     string `json:"mime_type"`
	OriginalName string `json:"original_name"`
	Size         int64  `json:"size"`
	URL          string `json:"url"`
}

type NodeProgressDTO struct {
	NodeID    string `json:"node_id"`
	Completed bool   `json:"completed"`
}

// CourseProgressDTO 聚合进度。Total = 可完成节点数。
type CourseProgressDTO struct {
	CourseID  string            `json:"course_id"`
	Total     int               `json:"total"`
	Completed int               `json:"completed"`
	Nodes     []NodeProgressDTO `json:"nodes"`
}

// ---- 请求体 ----

type CreateCourseReq struct {
	Title       string `json:"title" binding:"required,min=1,max=200"`
	Description string `json:"description" binding:"max=2000"`
}

type UpdateCourseReq struct {
	Title       string `json:"title" binding:"required,min=1,max=200"`
	Description string `json:"description" binding:"max=2000"`
	CoverFileID string `json:"cover_file_id"`
	Published   *bool  `json:"published"`
}

type CreateNodeReq struct {
	ParentID *string `json:"parent_id"`
	Title    string  `json:"title" binding:"required,min=1,max=200"`
	Kind     string  `json:"kind" binding:"omitempty,oneof=section theory workshop lab quiz"`
}

type UpdateNodeReq struct {
	Title      *string         `json:"title" binding:"omitempty,min=1,max=200"`
	Kind       *string         `json:"kind" binding:"omitempty,oneof=section theory workshop lab quiz"`
	Content    json.RawMessage `json:"content"`
	Assessment json.RawMessage `json:"assessment"`
}

// AssessNodeReq 提交作答:theory/quiz 用 Answers(questionId→选中 optionId);lab 用 Reactions(supportId→反力矢量)。
type AssessNodeReq struct {
	Answers   map[string][]string `json:"answers"`
	Reactions map[string]FBDVec   `json:"reactions"`
}

// FBDSolutionDTO 作者预览:各支座的标准反力。
type FBDSolutionDTO struct {
	Reactions map[string]FBDVec `json:"reactions"`
}

// AssessResultDTO 判分结果:逐题对错 + 解析 + 是否全对通过。
type AssessResultDTO struct {
	Results      map[string]bool   `json:"results"`
	Explanations map[string]string `json:"explanations,omitempty"`
	Passed       bool              `json:"passed"`
}

// StepAssessResultDTO workshop 单步判分:该步是否通过 + 整节是否通过。
type StepAssessResultDTO struct {
	Results      map[string]bool   `json:"results"`
	Explanations map[string]string `json:"explanations,omitempty"`
	Passed       bool              `json:"passed"`
	NodePassed   bool              `json:"node_passed"`
}

type MoveNodeReq struct {
	NodeID   string  `json:"node_id" binding:"required"`
	ParentID *string `json:"parent_id"`
	Position int     `json:"position"`
}

type CreateAnnotationReq struct {
	AnchorFrom int    `json:"anchor_from" binding:"required"`
	AnchorTo   int    `json:"anchor_to" binding:"required"`
	Quote      string `json:"quote" binding:"max=2000"`
	Body       string `json:"body" binding:"required,min=1,max=4000"`
	Visibility string `json:"visibility" binding:"required,oneof=public private"`
}

type UpdateAnnotationReq struct {
	Body       *string `json:"body" binding:"omitempty,min=1,max=4000"`
	Visibility *string `json:"visibility" binding:"omitempty,oneof=public private"`
}
