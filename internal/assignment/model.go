package assignment

import "time"

// ============ 实体 ============

type Assignment struct {
	ID          string    `gorm:"primaryKey;type:char(36)"`
	ClassID     string    `gorm:"type:char(36);not null;index:idx_assignment_class"`
	Title       string    `gorm:"type:varchar(200);not null"`
	Description string    `gorm:"type:varchar(2000);not null;default:''"`
	Status      string    `gorm:"type:varchar(16);not null;default:'open'"`
	AssignedAt  time.Time `gorm:"not null"`
	DueAt       time.Time `gorm:"not null"`
	CreatedBy   string    `gorm:"type:char(36);not null"`
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

func (Assignment) TableName() string { return "assignment_assignments" }

type Question struct {
	ID           string `gorm:"primaryKey;type:char(36)"`
	AssignmentID string `gorm:"type:char(36);not null;index:idx_question_assignment"`
	Position     int    `gorm:"not null;default:0"`
	Type         string `gorm:"type:varchar(16);not null;default:'choice'"`
	Prompt       string `gorm:"type:text"`
	Points       int    `gorm:"not null;default:0"`
	Options      string `gorm:"type:json"` // [{key,text}]
	Answer       string `gorm:"type:text"` // 参考答案 / 正确项("ABD")
	Media        string `gorm:"type:json"` // [{name,kind}]
}

func (Question) TableName() string { return "assignment_questions" }

type Submission struct {
	ID             string     `gorm:"primaryKey;type:char(36)"`
	AssignmentID   string     `gorm:"type:char(36);not null;uniqueIndex:idx_assignment_student,priority:1"`
	StudentID      string     `gorm:"type:char(36);not null;uniqueIndex:idx_assignment_student,priority:2"`
	Status         string     `gorm:"type:varchar(16);not null;default:'todo'"`
	Source         string     `gorm:"type:varchar(16);not null;default:'direct'"`
	SoloChatConvID string     `gorm:"type:char(36)"`
	SoloChatTitle  string     `gorm:"type:varchar(200)"`
	TotalScore     *float64   `gorm:""`
	SubmittedAt    *time.Time `gorm:""`
	GradedAt       *time.Time `gorm:""`
	CreatedAt      time.Time  `gorm:"not null"`
	UpdatedAt      time.Time  `gorm:"not null"`
}

func (Submission) TableName() string { return "assignment_submissions" }

type Answer struct {
	ID           string   `gorm:"primaryKey;type:char(36)"`
	SubmissionID string   `gorm:"type:char(36);not null;uniqueIndex:idx_submission_question,priority:1"`
	QuestionID   string   `gorm:"type:char(36);not null;uniqueIndex:idx_submission_question,priority:2"`
	Choice       string   `gorm:"type:varchar(64)"`
	Text         string   `gorm:"type:text"`
	ImageKeys    string   `gorm:"type:json"` // []ossKey
	Score        *float64 `gorm:""`
	Comment      string   `gorm:"type:text"`
	Annotations  string   `gorm:"type:json"` // [{x,y,w,h,note}] 图片批注框
	Highlights   string   `gorm:"type:json"` // [{start,end}] 主观题文本高亮
}

func (Answer) TableName() string { return "assignment_answers" }

// AssignmentFile 题目媒体(教师上传,scope=question)与图片作答(学生上传,scope=answer)。
// 按班级成员关系授权:question 全班可读,answer 仅 owner 学生或该班教师可读。
// 班级级而非作业级 —— 创建作业时作业尚不存在,题目媒体需先于作业上传。
type AssignmentFile struct {
	ID           string    `gorm:"primaryKey;type:char(36)"`
	ClassID      string    `gorm:"type:char(36);not null;index:idx_file_class"`
	Scope        string    `gorm:"type:varchar(16);not null;default:'answer'"`
	OwnerUserID  string    `gorm:"type:char(36);not null;index:idx_file_owner"`
	OSSKey       string    `gorm:"type:varchar(255)"`
	OriginalName string    `gorm:"type:varchar(255)"`
	MimeType     string    `gorm:"type:varchar(64)"`
	Kind         string    `gorm:"type:varchar(16)"`
	Size         int64     `gorm:""`
	CreatedAt    time.Time `gorm:"not null"`
}

func (AssignmentFile) TableName() string { return "assignment_files" }

const (
	StatusOpen   = "open"
	StatusClosed = "closed"

	SubTodo      = "todo"
	SubDoing     = "doing"
	SubSubmitted = "submitted"
	SubLate      = "late"
	SubGraded    = "graded"

	SourceDirect   = "direct"
	SourceSoloChat = "solochat"
	SourceUpload   = "upload"

	QTypeChoice     = "choice"
	QTypeMulti      = "multi"
	QTypeSubjective = "subjective"
	QTypeImage      = "image"

	ScopeQuestion = "question"
	ScopeAnswer   = "answer"
)

// ============ 嵌套值结构(JSON 解析后的形状)============

type Option struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// Media 题目媒体引用:id 指向 AssignmentFile,name/kind 为展示用快照。
type Media struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type MediaDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type Annotation struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	Note string  `json:"note"`
}

// HighlightRange 主观题作答文本里的高亮区间(字符偏移)。
type HighlightRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ============ 请求 DTO ============

type QuestionInput struct {
	Type    string   `json:"type"    binding:"required,oneof=choice multi subjective image"`
	Prompt  string   `json:"prompt"`
	Points  int      `json:"points"`
	Options []Option `json:"options"`
	Answer  string   `json:"answer"`
	Media   []Media  `json:"media"`
}

type CreateAssignmentReq struct {
	Title       string          `json:"title"       binding:"required,min=1,max=200"`
	Description string          `json:"description" binding:"max=2000"`
	DueAt       string          `json:"due_at"      binding:"required"`
	Questions   []QuestionInput `json:"questions"   binding:"required,min=1,dive"`
}

type UpdateAssignmentReq struct {
	Title       *string         `json:"title,omitempty"       binding:"omitempty,min=1,max=200"`
	Description *string         `json:"description,omitempty" binding:"omitempty,max=2000"`
	DueAt       *string         `json:"due_at,omitempty"`
	Status      *string         `json:"status,omitempty"      binding:"omitempty,oneof=open closed"`
	Questions   []QuestionInput `json:"questions,omitempty"   binding:"omitempty,dive"`
}

type AnswerInput struct {
	QuestionID string   `json:"question_id" binding:"required"`
	Choice     string   `json:"choice"`
	Text       string   `json:"text"`
	ImageKeys  []string `json:"image_keys"`
}

// SaveSubmissionReq 学生保存/提交作答。submit=true 表示正式提交(锁定),否则存草稿。
type SaveSubmissionReq struct {
	Submit         bool          `json:"submit"`
	Source         string        `json:"source"` // direct / solochat / upload
	SoloChatConvID string        `json:"solochat_conv_id"`
	SoloChatTitle  string        `json:"solochat_title"`
	Answers        []AnswerInput `json:"answers"`
}

type GradeAnswerInput struct {
	QuestionID  string           `json:"question_id" binding:"required"`
	Score       *float64         `json:"score"`
	Comment     string           `json:"comment"`
	Annotations []Annotation     `json:"annotations"`
	Highlights  []HighlightRange `json:"highlights"`
}

// GradeReq 教师批改。finalize=true 把提交标记为已批改。
type GradeReq struct {
	Finalize bool               `json:"finalize"`
	Answers  []GradeAnswerInput `json:"answers" binding:"required,dive"`
}

// ============ 响应 DTO ============

type QuestionDTO struct {
	ID       string     `json:"id"`
	Type     string     `json:"type"`
	Prompt   string     `json:"prompt"`
	Points   int        `json:"points"`
	Options  []Option   `json:"options"`
	Answer   string     `json:"answer"`
	Media    []MediaDTO `json:"media"`
	Position int        `json:"position"`
}

// MySubmissionLite 列表/侧栏里学生自己的提交摘要。
type MySubmissionLite struct {
	Status string   `json:"status"`
	Done   int      `json:"done"`
	Total  int      `json:"total"`
	Score  *float64 `json:"score,omitempty"`
}

type AssignmentDTO struct {
	ID            string `json:"id"`
	ClassID       string `json:"class_id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Status        string `json:"status"`
	AssignedAt    string `json:"assigned_at"`
	DueAt         string `json:"due_at"`
	CreatedBy     string `json:"created_by"`
	QuestionCount int    `json:"question_count"`
	Points        int    `json:"points"`
	CreatedAt     string `json:"created_at"`

	// 教师视角统计
	Submitted int      `json:"submitted"`
	Total     int      `json:"total"`
	Graded    int      `json:"graded"`
	Avg       *float64 `json:"avg"`

	// 学生视角
	My *MySubmissionLite `json:"my,omitempty"`
}

type AnswerDTO struct {
	QuestionID  string           `json:"question_id"`
	Choice      string           `json:"choice"`
	Text        string           `json:"text"`
	ImageKeys   []string         `json:"image_keys"` // 文件 id,供学生再编辑时保留
	ImageURLs   []string         `json:"image_urls"`
	Score       *float64         `json:"score"`
	Comment     string           `json:"comment"`
	Annotations []Annotation     `json:"annotations"`
	Highlights  []HighlightRange `json:"highlights"`
}

type SubmissionDTO struct {
	ID             string      `json:"id"`
	AssignmentID   string      `json:"assignment_id"`
	StudentID      string      `json:"student_id"`
	Status         string      `json:"status"`
	Source         string      `json:"source"`
	SoloChatConvID string      `json:"solochat_conv_id,omitempty"`
	SoloChatTitle  string      `json:"solochat_title,omitempty"`
	TotalScore     *float64    `json:"total_score"`
	SubmittedAt    string      `json:"submitted_at,omitempty"`
	GradedAt       string      `json:"graded_at,omitempty"`
	Answers        []AnswerDTO `json:"answers"`
}

// AssignmentDetailDTO 作业详情:题目 + (学生)自己的提交。
type AssignmentDetailDTO struct {
	Assignment AssignmentDTO   `json:"assignment"`
	Questions  []QuestionDTO   `json:"questions"`
	My         *SubmissionDTO  `json:"my,omitempty"`
	MyImported *ImportedRecord `json:"my_imported,omitempty"`
}

// ImportedRecord 学生从 SoloChat 导入的记录正文(供师/生在批阅/查看时阅读)。
type ImportedRecord struct {
	Title    string        `json:"title"`
	Messages []ImportedMsg `json:"messages"`
}

type ImportedMsg struct {
	Role string `json:"role"` // user / assistant
	Text string `json:"text"`
}

type AssignmentFileDTO struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Size int64  `json:"size"`
}

type StudentLite struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// RosterEntryDTO 看板里一名学生 + 其提交状态。
type RosterEntryDTO struct {
	Student      StudentLite `json:"student"`
	SubmissionID string      `json:"submission_id,omitempty"`
	State        string      `json:"state"` // graded / submitted / late / missing
	SubmittedAt  string      `json:"submitted_at,omitempty"`
	Score        *float64    `json:"score,omitempty"`
}

// GradeRosterLite 批阅页可翻页的已提交学生(带 submission id)。
type GradeRosterLite struct {
	SubmissionID string `json:"submission_id"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatar_url,omitempty"`
}

// 教师批阅:单份提交 + 题目 + 学生信息
type GradeViewDTO struct {
	Assignment AssignmentDTO     `json:"assignment"`
	Questions  []QuestionDTO     `json:"questions"`
	Student    StudentLite       `json:"student"`
	Submission SubmissionDTO     `json:"submission"`
	Roster     []GradeRosterLite `json:"roster"`
	Imported   *ImportedRecord   `json:"imported,omitempty"`
}

// ============ Hub 总览 ============

type ClassSummaryDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Students  int    `json:"students"`
	Count     int    `json:"count"`
	Open      int    `json:"open"`
	ToGrade   int    `json:"to_grade"`
	MyTodo    int    `json:"my_todo"`
	NextTitle string `json:"next_title,omitempty"`
}

// HubAssignmentDTO 学生总览里跨班的扁平作业项。
type HubAssignmentDTO struct {
	ID             string   `json:"id"`
	ClassID        string   `json:"class_id"`
	ClassName      string   `json:"class_name"`
	ClassAvatarURL string   `json:"class_avatar_url,omitempty"`
	Title          string   `json:"title"`
	DueAt          string   `json:"due_at"`
	Status         string   `json:"status"`
	MyStatus       string   `json:"my_status"`
	MyDone         int      `json:"my_done"`
	MyTotal        int      `json:"my_total"`
	MyScore        *float64 `json:"my_score,omitempty"`
}

type HubDTO struct {
	IsTeacher bool              `json:"is_teacher"`
	Classes   []ClassSummaryDTO `json:"classes"`

	// 聚合
	ToGrade    int `json:"to_grade"`
	OpenCount  int `json:"open_count"`
	MyTodo     int `json:"my_todo"`
	ClassCount int `json:"class_count"`

	// 学生扩展
	MyAssignments []HubAssignmentDTO `json:"my_assignments,omitempty"`
	NextDue       string             `json:"next_due,omitempty"`
	MonthAvg      *float64           `json:"month_avg,omitempty"`

	// 截止/活动热力(近 ~18 周每日一格)
	Heat []HeatCell `json:"heat"`
}

type HeatCell struct {
	Date  string `json:"date"` // 2006-01-02
	Level int    `json:"level"`
}
