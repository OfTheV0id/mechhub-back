package realtime

// 帧类型常量。统一通过 Frame 信封发出,客户端按 Type 字段分发。
const (
	FrameReady = "ready"
	FramePing  = "ping"

	// 班级失效(继承自第 1 阶段 SSE 语义)
	FrameClassInvalidate = "class.invalidate"

	// 频道消息事件(由 channel 模块发出)
	FrameChannelMessageCreated = "channel.message.created"
	FrameChannelMessageUpdated = "channel.message.updated"
	FrameChannelMessageDeleted = "channel.message.deleted"
)

// 班级失效事件:targets / reason 与第 1 阶段 SSE 完全一致(语义不变,只是搬载体)
const (
	TargetClasses     = "classes"
	TargetClassDetail = "class_detail"
	TargetMembers     = "members"
	TargetChannels    = "channels"

	ReasonMemberJoined   = "member_joined"
	ReasonMemberLeft     = "member_left"
	ReasonMemberRemoved  = "member_removed"
	ReasonClassUpdated   = "class_updated"
	ReasonAvatarUpdated  = "avatar_updated"
	ReasonAvatarRemoved  = "avatar_removed"
	ReasonClassDeleted   = "class_deleted"
	ReasonChannelCreated = "channel_created"
	ReasonChannelUpdated = "channel_updated"
	ReasonChannelDeleted = "channel_deleted"
)

// ReadyFrame 连接建立后第一帧
type ReadyFrame struct {
	Type     string   `json:"type"`
	UserID   string   `json:"user_id"`
	ClassIDs []string `json:"class_ids"`
}

// ClassInvalidate 表示班级"该重拉了"提示(不带具体 payload)
type ClassInvalidate struct {
	Type    string   `json:"type"`
	ClassID string   `json:"class_id"`
	Targets []string `json:"targets"`
	Reason  string   `json:"reason"`
}
