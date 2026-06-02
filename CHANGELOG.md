# 更改日志

记录每一轮 AI 协作 / PR 的实质性改动。新条目放在最上面。

格式约定:
- 标题用 `## <轮次> — <日期> — <主要执行者>`
- 改动按"破坏性 / 功能 / 修复 / 杂项"分组
- 破坏性变更必须标 ⚠️,并说明前端 / 调用方需要怎么改

---

## Claude 轮 21 — 2026-06-02 — 作业模块第二轮:导入内容/媒体/实时/高亮

把作业模块里几处占位做实。**不接 AI、不自动判分(全人工批改)。**

### ⚠️ 破坏性

- **文件上传端点改为班级级**:`POST /api/assignments/:assignmentId/files` → `POST /api/classes/:classId/assignment-files`(创建作业时题目媒体需先于作业上传)。`assignment_files` 表 `assignment_id` 列 → `class_id` + 新增 `scope`(`question`/`answer`)。前端 `uploadFiles` 需改打新路径。AutoMigrate 自动加新列,旧 `assignment_id` 列残留无害(功能新无真数据)。

### 功能

- **SoloChat 导入内容内嵌**:`assignment.Service` 注入 `*llm.Service`;`GetGradeView`(教师)与 `GetDetail`(学生本人,source=solochat)经 `llm.ListMessages` 取导入会话正文,压成 `ImportedRecord{ title, messages[] }` 随 DTO 返回(`imported` / `my_imported`)。
- **题目真实媒体**:`Question.Media` 现为 `[{id,name,kind}]` 指向 `assignment_files`;`QuestionDTO.Media` 带解析好的 `url`。教师在创建/编辑作业时上传图片作为题目媒体。
- **主观题文本高亮**:`assignment_answers` 增 `highlights`(JSON `[{start,end}]`);批改请求 `GradeAnswerInput.highlights` 写入,`AnswerDTO.highlights` 回带。
- **总览热力图真数据**:`HubDTO.heat` = 近 18 周每日活跃序列(按作业截止 + 提交时间聚合,`[{date,level}]`)。
- **实时通知**:`assignment.Service` 注入 `*realtime.Hub`,新增帧 `assignment.invalidate`(reason:`assignment_created/updated/deleted/submission_created/graded`)。建/改/删作业→广播全班;学生提交→推教师;教师批改完成→推学生。前端据此实时刷新看板/侧栏/总览。

### 修复

- **已提交即锁定**:`SaveSubmission` 对已 submitted/late/graded 的提交返回 409「作业已提交,不能再修改」,避免学生重存覆盖掉教师批改痕迹。

### 杂项

- `router.go`:`assignment.NewService(..., hub, llmSvc, cfg)` 注入两个新依赖。Postman 文件上传请求路径同步更新。

---

## Claude 轮 20 — 2026-06-01 — 新增作业(Assignments)模块

新增完整的「作业」板块后端,挂在班级之下,覆盖创建 → 学生作答/提交 → 教师批阅打分全流程。复用 `class` 模块的成员/角色校验(`user.role == "teacher"` 且为班级 owner 才能管理作业/批改),其余成员为学生视角。

### 功能

- **新模块 `internal/assignment/`(五件套)**,5 张新表(均加 `assignment_` 前缀,已进 `main.go` AutoMigrate):
  - `assignment_assignments`(作业:title/description/status(open/closed)/assigned_at/due_at/created_by)
  - `assignment_questions`(子题目:type ∈ choice/multi/subjective/image,options/answer/media 存 JSON,points,position)
  - `assignment_submissions`(一生一作业一份,唯一索引 (assignment_id, student_id);status ∈ todo/doing/submitted/late/graded;source ∈ direct/solochat/upload;solochat 导入存 conv id + 标题快照)
  - `assignment_answers`(每题作答:choice/text/image_keys + score/comment/annotations)
  - `assignment_files`(图片作答/媒体,按班级成员关系授权读取,独立于 solochat 附件)
- **新端点(均需登录)**:
  - `GET /api/assignments/hub` 总览聚合(师/生按角色)
  - `GET|POST /api/classes/:classId/assignments` 班级作业列表 / 创建
  - `GET|PATCH|DELETE /api/assignments/:assignmentId` 详情 / 编辑 / 删除
  - `GET /api/assignments/:assignmentId/roster` 学生提交总览(看板,教师)
  - `GET|PUT /api/assignments/:assignmentId/submission` 学生取 / 保存提交
  - `POST /api/assignments/:assignmentId/files` 图片作答 / 媒体上传(multipart)
  - `GET /api/assignment/files/:fileId` 读取作答文件(owner 或班级教师)
  - `GET /api/submissions/:submissionId` 批阅工作台数据(教师)
  - `PATCH /api/submissions/:submissionId/grade` 逐题打分 + 评语 + 图片批注(教师)
- 学生提交支持直接作答(选择/文本/图片)与从 SoloChat 导入(引用快照,不深拷对话正文)。
- Postman 同步:新增 `assignment/` 组及全部请求文件,集合根 + local 环境补 `assignmentId`/`submissionId`/`questionId`/`questionId2`/`fileId` 变量。

### 杂项

- 路由装配在 class 之后(复用 `classRepo` / `userRepo`),`internal/router/router.go` 加一行 mount,无新增配置项。
- 「AI 批改建议」当前为前端占位,后端未接 LLM。

---

## Claude 轮 19 — 2026-05-29 — 分享消息 fork 回 solochat + 浓缩卡片

给频道里的分享消息(批改 / 对话片段)加 **fork** 能力:任何班级成员都能把它**忠实复制**成自己的 solochat 新对话,不触发 AI 生成。基于消息里自包含的快照 + 频道附件重建,不读原 solochat 对话(属分享者),因此非原作者也能 fork。前端配套:对话片段卡片改为浓缩态 + 可展开全屏预览(纯前端)。

### 功能

- **新端点 `POST /api/channels/:channelId/messages/:messageId/fork`**:成员校验后,把该消息的 `reference` 快照 + 绑定的频道附件重建成 fork 用户的 solochat 新对话,返回 `solochat.ConversationDTO`(`{ id, title, created_at, updated_at }`)。
- `channel.Service.ForkMessageToSolochat`(`internal/channel/fork.go`):把 `MessageReference` + 频道附件映射成 `solochat.SeedSpec`,委托 solochat 重建。
- `solochat.Service.SeedConversation` + `SeedSpec`(`internal/solochat/seed.go`):建对话(`title_generated=true` 防自动改名)→ OSS 服务端把频道附件**反向拷贝**进本人 `uploaded_files` → 重建 ADK 历史事件;grading 的 `imageRefs` 按 RefKey 回写成新 solochat 附件 id/url,fork 后 OCR 可视化可直接加载。失败补偿:删已建附件行 + OSS 对象 + 对话行。
- `llm.Service.SeedSession` + `SeedTurn`/`SeedPart`(`internal/llm/seed.go`):经 `sessionSvc.AppendEvent` 直接注入 user/assistant 历史事件(text / tool_use / tool_result),**不经 LLM**;user turn 的附件用 `_solochat_attachments_<inv>` state 绑定,与正常 stream 落盘格式一致,`ListMessages` 能原样还原。

### 功能(对话片段保真)

- **`MessageReference.segments[]` 新增 `parts`**(`SegmentPart{ type, text, name, input, output }`,additive):分享对话片段时忠实保留每条消息的 `text` / `tool_use` / `tool_result`(丢 `thinking`),而非只存纯文本。这样:① 频道里分享的对话若含批改,展开预览能直接「查看可视化分析」;② fork 出来的对话能重建完整工具链。
- 分享 / fork 时,对话片段里 grading `tool_result` 引用的图片也一并复制(分享→频道附件、fork→solochat 附件)并按 RefKey 回写 `imageRefs`,与单独分享批改一致。`segments[].text` 保留作纯文本兼容 + 浓缩卡片预览;无 `parts` 的老数据回落到 `text`。

### 杂项

- fork 复用现有依赖装配(channel→solochat→llm),`main.go` 无需改动。
- 已知取舍:assistant 事件 author 用 agent 名(`mechhub_tutor`),grading turn 把 `grade_with_ocr` 的 tool_use + tool_result 放在同一 assistant invocation(与正常批改分组一致);fork 后继续追问时该历史会原样进上下文。

---

## Claude 轮 18 — 2026-05-29 — solochat 批改 / 对话片段分享到频道

新增"把 solochat 的批改结果 / 多条对话片段分享到 class 频道"的能力。复用 `POST /api/channels/:channelId/messages` 端点,请求体新增可选 `share` 字段;后端按 `source_chat_id + source_message_id(s)` **反查 ADK session 自己生成快照**(不信前端传内容,防伪造),并把被引用的 solochat 图片附件**服务端拷贝**成本频道附件,使分享消息自包含、不依赖原 solochat 权限。

### ⚠️ 破坏性变更

1. **`MessageDTO` 新增可选字段 `reference`**(`channel.MessageReference`)。普通消息为 `null`,分享消息为结构化快照:
   - `reference.type`:`"grading"`(批改)或 `"thread"`(对话片段)
   - `grading`:完整 `GradingOutput`,其 `imageRefs[].url` 已指向**频道附件** URL(前端可直接喂给 OCR 可视化)
   - `segments[]`:`{ role, text, attachments[] }`,`attachments[].url` 同为频道附件 URL
   - 前端 `messageitem` 需在 `message.reference` 非空时渲染富卡片。
2. **`SendMessageReq` 新增可选字段 `share`**(`ShareRefInput`):`{ type(grading|thread), source_chat_id, source_message_id?, source_message_ids[]? }`。普通发消息不传即可,契约向后兼容(`content` 仍 `required`,分享时前端传附言或默认文案)。

### 功能

- `channel.Service` 注入 `*solochat.Service`,新增 `SendShareMessage` / `copySolochatFilesToChannel`(见 `internal/channel/share.go`)。
- `storage.OSS.Copy`:同 bucket 服务端拷贝(`CopyObject`),不下载再上传。
- `solochat.Service.FindFiles`:按 id 拉本人文件元数据(含 OSSKey),供跨模块拷贝。
- 失败补偿:图片拷贝非事务,任一失败回滚已拷贝的附件行 + OSS 对象;某图已删致数量不符 → 整体拒绝(`400 分享来源无效或图片已不存在`),不静默丢图。

### 杂项

- `channel_messages` 加 `reference` 列(`type:json`,nullable),AutoMigrate 自动加。
- `Repo.DeleteAttachmentsByIDs`:回滚用。

---

## Claude 轮 17 — 2026-05-26 — 取消「班内角色」概念,成员 DTO 扁平化

「班内角色」从概念里完全删除 —— 用户的账号 `role` 就是唯一身份。所以 `class_members.role`、`UpdateMemberRole` 端点、`MembershipRole` 字段全部下线;成员路由用 `user_id`,班级模块只有一种成员标识。

### ⚠️ 破坏性变更

1. **删除端点 `PATCH /api/classes/:classId/members/:memberId`**(改成员角色)。班级里没有「成员的班内角色」,要改用户身份去 user 域做(没有这个 API)。
2. **`DELETE /api/classes/:classId/members/:memberId` → `DELETE /api/classes/:classId/members/:userId`** —— URL 参数从 class_members 行 PK 改成 user_id。前端把 `member.id` 替换成 `member.user_id`。
3. **`GET /api/classes/:id/members` 响应扁平化**:
   - 旧:`{ id, class_id, role, is_owner, joined_at, user: { id, email, name, role, avatar_url } }`
   - 新:`{ user_id, name, email, role, avatar_url, joined_at }`
   - 删字段:`id` / `class_id` / `is_owner`(`user_id === class.owner_user_id` 推);嵌套 `user.*` 全部拍平
   - `role` 现在就是用户账号角色,没有「班内角色」
4. **`ClassDTO.membership_role` 删除**。`/api/classes` 和 `/api/classes/:id` 响应不再含该字段。
5. **`class_members.role` 列已 drop**(本轮 ALTER 执行,DB 兼容)。

### 杂项

- `realtime.ReasonMemberRoleUpdated` 常量删(再没人发)。
- `class.ErrOwnerRoleImmutable` 错误码删。
- `channel/service.go::requireChannelAdmin` 改为查 user.role 而不是已废弃的 `Member.Role`。

---

## Claude 轮 16 — 2026-05-26 — 班级响应 DTO 收敛 + drop invite_code 列

### ⚠️ 破坏性变更

1. **班级响应统一为 `ClassDTO`**(`ClassListItem` / `ClassDetail` 合并)。
   - `GET /api/classes`(列表)和 `GET /api/classes/:id`(详情)返回**完全相同**的形状,列表不再缺字段,前端不需要再调详情。
   - **去掉字段:`status` / `is_owner`**。`status` 当前没有 archived 流程,删字段不丢任何信息;`is_owner` 用 `owner_user_id === currentUserId` 自己算。
   - `POST /api/classes` / `PATCH /api/classes/:id` / `POST /api/classes/:id/avatar` / `POST /api/classes/invite/:token/accept` 的成功响应也是 `ClassDTO`。
   - `GET /api/classes/invite/:token` 预览里的 `class` 字段同步变成 `ClassDTO`。
2. **`PATCH /api/classes/:id` 不再接受 `status` 字段**(前端如有传请删掉)。
3. **`classes.invite_code` 列已 drop**(本轮通过一次性 ALTER 执行,DB 兼容)。

### 杂项

- `internal/class/model.go::Class.Status` 字段保留(DB 兼容 + 后续 archived 流程预留),只是不再外露。

---

## Claude 轮 15 — 2026-05-25 — Discord 频道化 + 邀请链接 + WebSocket 实时(班级第 2 阶段)

把班级从「组织容器」拓展成「Discord 服务器」式的多频道讨论空间:新模块 `internal/channel/`(频道 + 消息 + 附件),新模块 `internal/realtime/`(WebSocket 单连接多路复用),`internal/class/` 改造邀请链接 + 装配 channel hook。本轮**吃下 3 处破坏性变更**,见 ⚠️。

### ⚠️ 破坏性变更

1. **`GET /api/classes/events` (SSE) 删除** —— 第 14 轮刚发的 SSE 失效事件流退役。
   - 替代:`GET /api/ws`(WebSocket),`class.invalidate` 帧的 `type` / `class_id` / `targets` / `reason` 字段保持不变,只是搬到 WS 信封里
   - 前端:删 `EventSource`,改 `new WebSocket(...)`,按 frame.type 分发
2. **`POST /api/classes/join { invite_code }` 删除** —— 邀请机制从 6 位短码升级为 token + 分享链接。
   - 替代:`POST /api/classes/invite/:token/accept`,新增 `GET /api/classes/invite/:token` 预览
   - 前端:把"输入邀请码"控件换成"贴邀请链接 / 直接打开 `<APP_BASE_URL>/invite/<token>`"
3. **`classes.invite_code` 列废弃**(代码不再读写)+ 新增 3 列 `invite_token` / `invite_expires_at` / `invite_disabled`。
   - `AutoMigrate` 自动加新列,但**不会**自动 drop 旧列
   - 一次性 SQL(可选):`ALTER TABLE classes DROP COLUMN invite_code;`
   - `ClassDetail` 响应里**不再包含** `invite_code` —— 拿 invite 走专门的 `GET /api/classes/:id/invite`

### 功能

4. **新模块 `internal/realtime/`**(WebSocket 基础设施,项目首个 WS 依赖)
   - [hub.go](internal/realtime/hub.go):内存 Hub,索引 `byUser` / `byClass` / `classesByConn`;暴露 `Register / Unregister / AddUserToClass / RemoveUserFromClass / BroadcastToClass / SendToUsers`,buffer 满直接丢帧不阻塞
   - [conn.go](internal/realtime/conn.go):per-connection read/write pump + 心跳;60s pongWait / 25s pingPeriod / 10s writeWait(对齐 gorilla 官方 chat example);`send` chan buffer=64
   - [handler.go](internal/realtime/handler.go):`GET /api/ws`,session cookie 鉴权,upgrade 后 `Register` + 立即发 `ready` 帧带 `class_ids`。用 `MembershipResolver` 接口反向依赖 class.Repo,避免 realtime → class import 环
   - 新依赖:`github.com/gorilla/websocket v1.5.x`(`go.mod` / `go.sum`)

5. **新模块 `internal/channel/`**(Discord 频道 + 消息 + 附件)
   - [model.go](internal/channel/model.go):3 张表 `channels` / `channel_messages` / `channel_attachments`;主键全 UUID;`channels(class_id, name)` 复合 unique;`channel_messages(channel_id, created_at DESC)` 主查询索引;`channel_attachments.message_id` 可空(上传后 SendMessage 时绑定)
   - [repo.go](internal/channel/repo.go):CRUD + 游标分页(`before=<msgId>` → 查该消息 created_at 再 `< created_at`)+ 事务级联删除 + `BindAttachmentsToMessage` 原子绑定
   - [service.go](internal/channel/service.go):
     - 实现 `class.ChannelHook`:`OnClassCreated` 自动建 `#general`(`IsDefault=true`,name / is_default 锁,删 / 改名 → 400);`OnClassDeleted` 联带删该班所有频道 + 消息 + 附件 + OSS 文件
     - 频道写操作末尾 `hub.BroadcastToClass(classID, class.invalidate{targets:[channels], reason:channel_*})`
     - 消息写操作末尾 `hub.BroadcastToClass(classID, channel.message.created/updated/deleted)` —— 完整 MessageDTO 直接进 WS 帧,前端无需重拉
     - 附件 OSS key 格式 `channels/<channelID>/<random><ext>`;消息删 / 频道删 / 班删时联带删 OSS
   - [handler.go](internal/channel/handler.go) + [route.go](internal/channel/route.go):10 个端点
     - `GET / POST /api/classes/:classId/channels`
     - `GET / PATCH / DELETE /api/classes/:classId/channels/:channelId`
     - `GET / POST /api/channels/:channelId/messages`(GET 支持 `?before=<msgId>&limit=50`)
     - `PATCH / DELETE /api/channels/:channelId/messages/:messageId`
     - `POST /api/channels/:channelId/attachments`(multipart `files`)
     - `GET /api/channels/:channelId/attachments/:fileId`
   - 权限:列频道 / 列消息 / 发消息 / 上传附件 / 拉附件 = 班级成员;编辑消息 = 作者本人;删消息 = 作者本人或班 owner;建 / 改 / 删频道 = owner 或班里 `role=teacher` 的成员;`#general` 改名 / 删 → 400

6. **`internal/class/` 改造邀请链接**(替换原 invite_code)
   - [model.go](internal/class/model.go):删 `InviteCode`;加 `InviteToken` / `InviteExpiresAt *time.Time` / `InviteDisabled bool`;`DefaultInviteTTL = 30d`;新增 `ChannelHook` 接口(Go 隐式实现,channel.Service 自动满足)
   - [service.go](internal/class/service.go):
     - `JoinByInviteCode` → `JoinByInviteToken`,加过期 + 禁用闸
     - 新增 `GetInvite` / `RegenerateInvite` / `DisableInvite` / `PreviewInvite`
     - `Create` 末尾调 `channelHook.OnClassCreated`(建 #general)+ `hub.AddUserToClass(owner, classID)`
     - `Delete` 调 `channelHook.OnClassDeleted` + 给所有成员 `SendToUsers(class.invalidate{reason:class_deleted})` + 批量 `RemoveUserFromClass` 解绑 WS
     - `Leave` / `RemoveMember` 成功后 `hub.RemoveUserFromClass`
     - 第 14 轮的 `class.EventHub` 替换为 `realtime.Hub`,`emit(...)` 改成 `hub.SendToUsers(...)`
   - 邀请 token:`crypto/rand` 24 字节 → base64url 32 字符;`ShareURL = APP_BASE_URL + "/invite/" + token`(前端落地页)
   - 5 个新 invite handler + 路由;`failClassErr` 加 `ErrInviteExpired` / `ErrInviteDisabled` → 410

7. **WebSocket 帧约定**([internal/realtime/model.go](internal/realtime/model.go))
   - 服务端→客户端:`ready` / `ping` / `class.invalidate` / `channel.message.created` / `channel.message.updated` / `channel.message.deleted`
   - 客户端→服务端:无业务帧,只走 WS 协议级 ping/pong(浏览器自动响应)
   - 帧信封统一 JSON,`type` 字段分发;载荷视类型而定
   - 多 tab 同账号:`byUser` 持多连接,所有 tab 都收到广播

### 修复 / 杂项

8. **删除文件**:`internal/class/eventhub.go`(替代:`internal/realtime/hub.go`)
9. **Postman 同步**:
   - 新增 `postman/collections/MechHub Backend/channel/` 目录 —— 10 个 yaml(channel CRUD / messages / attachments)
   - 改 `postman/collections/MechHub Backend/class/` —— invite 系列 yaml(GetInvite / RegenerateInvite / DisableInvite / PreviewInvite / AcceptInvite)替换原 join,删 events SSE yaml
   - `.resources/definition.yaml` 加 `channelId` / `messageId` / `inviteToken` / `attachmentId` 占位
   - `environments/local.environment.yaml` 同步
10. **隐含规约修订**(未改 CLAUDE.md 文本,下一轮补):
    - 原"所有流式接口走 SSE"→ 实际是「**AI 流式输出 / 服务端单向 push** 走 SSE,**双向实时推送 / 多端通知**走 WS」

### 验证

`go build ./...` 通过。端到端手测路径见 [plan 文件](~/.claude/plans/miniback-back-silly-sun.md) 「验证(端到端)」段,14 步覆盖:建班自动建 #general → WS upgrade → invite regenerate → preview / accept → 频道 CRUD 广播 → 消息 CRUD 广播 → 附件上传 → #general 删除返 400 → invite 过期 / 禁用 / 重生。

---

## Claude 轮 14 — 2026-05-25 — 班级模块迁移(miniback → back)

### ⚠️ 破坏性变更

1. **班级 API 字段从 camelCase 改成 snake_case,主键改成 UUID 字符串**
   - 旧 miniback:`{ ownerUserId, membershipRole, isOwner, inviteCode, createdAt }`,整型 ID
   - 新 back:`{ owner_user_id, membership_role, is_owner, invite_code, created_at }`,UUID(char(36))
   - 前端切到新后端时要按 snake_case 取字段、把 `classId` / `memberId` 当字符串处理

2. **班级头像不再走 `uploaded_files` 元数据表,改走 OSS key 字段**
   - 旧 miniback:`POST /classes/:id/avatar` 返回 `{ avatar: { id, fileName, mimeType, sizeBytes, width, height } }`
   - 新 back:返回的 ClassDetail 里只有 `avatar_url`(stream-through `/api/classes/:id/avatar?v=<hash>`),不再有 width/height/sizeBytes 等元数据
   - 前端:不要再读 `avatar.fileName` 等字段;直接 `<img src={class.avatar_url}>`

3. **响应统一外壳 `{ code, msg, data }`**
   - 旧 miniback:`GET /classes` 直接返回数组,`POST /classes` 返回 201,204 表示删除成功
   - 新 back:全部 200 + `{ code: 0, msg: "成功", data: { classes: [...] } }`(列表) 或 `data: { ... }`(单对象);删除返回 `data: { message: "已删除" }`
   - 前端:统一从 `response.data.classes` / `response.data` 取数据

### 功能

4. **新模块 `internal/class/` 落地 14 个 HTTP 端点**
   - `GET /api/classes` 列班级 / `POST /api/classes` 创建(仅教师) / `POST /api/classes/join` 凭邀请码加入
   - `GET /api/classes/:classId` 详情 / `PATCH` 更新(仅 owner) / `DELETE` 删除(仅 owner,联带删 members + OSS 头像)
   - `POST/GET/DELETE /api/classes/:classId/avatar` 头像 CRUD(写入 owner only,读 member only)
   - `POST /api/classes/:classId/leave` 退出 / `GET /:classId/members` 成员列表 / `PATCH/DELETE /:classId/members/:memberId` 改角色/移除
   - 新增 [internal/class/{model,repo,service,handler,route,eventhub,invite}.go](internal/class/)
   - 新增数据表 `classes` + `class_members`,启动期 AutoMigrate 建立;主键 UUID;`(class_id, user_id)` unique 防重复加入

5. **`GET /api/classes/events` SSE 失效事件流**
   - 内存订阅中枢 [internal/class/eventhub.go](internal/class/eventhub.go),单进程版,buffer 32,满了丢
   - 写操作(join/leave/update/delete/avatar/改角色/移除)后异步 `EmitToUsers` 推 `class.invalidate` 帧到相关用户
   - 帧形状:`{ type: "class.invalidate", class_id, targets: ["classes"|"class_detail"|"members"], reason }`,前端按 targets 决定重拉哪些查询
   - 25s 一行 `: ping\n\n` 心跳;复用 `internal/sseutil/Writer`

6. **小重构:抽出 `internal/sseutil/Writer` 公共 SSE 写帧 helper**
   - 原 `internal/solochat/streamer.go` 里的 package-private `sseWriter` 提到新包 [internal/sseutil/writer.go](internal/sseutil/writer.go),`Write(any)` + `Heartbeat()` 通用化
   - solochat 改用 `sseutil.New(c)` / `w.Write(...)` / `w.Heartbeat()`;`writeFrame` 形参从 `*sseWriter` 改 `*sseutil.Writer`
   - `internal/solochat/streamer.go` 留空 package 头,不删文件避免 git 历史断裂

### 杂项

7. **Postman**:新增 `postman/collections/MechHub Backend/class/` 目录与 14 条 `.request.yaml`;集合根 `variables` 加 `classId` / `memberId` / `inviteCode`;`environments/local.environment.yaml` 同步占位
8. **不迁移**:assignments(作业)模块、miniback 的 `default_role` / `bio` / `display_name` 用户字段、班级头像的 width/height/sizeBytes 元数据 —— 留后续轮次

---

## Claude 轮 13 — 2026-05-20 — 对话标题 LLM 自动总结(ChatGPT 风格)

### ⚠️ 破坏性变更

1. **`POST /api/solochat/conversations` 不再接受 `title` 入参**
   - 创建恒为 `"新对话"`,body 给 `{}` 即可
   - [internal/solochat/model.go](internal/solochat/model.go) 删 `CreateConversationReq`
   - [internal/solochat/handler.go](internal/solochat/handler.go) handler 不再绑 body
   - [internal/solochat/service.go::CreateConversation](internal/solochat/service.go) 签名改为 `(ctx, userID) → (*Conversation, error)`
   - 前端:把请求 body 改成 `{}`(或干脆不传 body);需要重命名走原 `PUT /api/solochat/conversations/:id`

### 功能

2. **首次 AI 回复后 LLM 自动出标题**
   - 之前是用户消息前 24 字截断(`autoTitle`),现在改成用 root agent 的模型(跟随 `LLM_PROVIDER`:Gemini 走 Gemini,DeepSeek 走 DeepSeek)总结 user/assistant 对话
   - 新增 [internal/llm/title.go::GenerateTitle](internal/llm/title.go) —— 走 `model.LLM` 非流式 `GenerateContent`,prompt 限定 ≤16 字、纯标题、无引号无标点;5–8s 超时
   - [internal/solochat/service.go](internal/solochat/service.go) 在 stream loop 里累积 assistant `text_delta`,流末若 `isFirstMessage` 且 `finishReason=stop` 调 `GenerateTitle`,失败 / 超时 / 错误结束 / 用户取消 → **回退到旧的 `autoTitle`**(主流程不受影响)
   - SSE 帧形状不变(仍是 `conversation_title` + 整 `conversation` DTO)

### 杂项

3. **Postman 同步**:[Create conversation.request.yaml](postman/collections/MechHub Backend/solochat/conversations/Create conversation.request.yaml) body 改空 `{}`,描述说明"首次 AI 回复后由 root LLM 总结标题"

---

## Claude 轮 12 — 2026-05-19 — Root agent 可换 OpenAI-compat 后端(DeepSeek V4-Pro)

### 功能

1. **新增 ADK Go 的 OpenAI ChatCompletions-兼容 model 适配器**
   - 新增 [internal/llm/openai/openai.go](internal/llm/openai/openai.go)(改造自 [byebyebruce/adk-go-openai](https://github.com/byebyebruce/adk-go-openai),MIT)。原版钉 ADK v0.2.0,这里对齐 v1.2.0 接口
   - 覆盖:streaming(`Partial=true` 增量 + 收尾 aggregated)/ tool calling(按 `tool_call.index` 跨 chunk 累积 args)/ vision(`InlineData` → data URL)/ structured output(`ResponseSchema` → `response_format: json_schema`)/ `ReasoningEffort` low/medium/high / 多 tool response 合并 / system instruction 前置
   - **打开 reasoning_content 通道**(原版被注释成 TODO):DeepSeek R1 / V4 的思考链 → `genai.Part{Thought: true}` + `Partial: true`,直接走 `sse.go` 现有的 `reasoning_delta` 帧路径,与 Gemini thinking 一致
2. **Provider switch**
   - [internal/config/config.go](internal/config/config.go) `LLMConfig` 加 `Provider` + 3 个 OpenAI-compat 字段;新 env `LLM_PROVIDER` / `OPENAI_COMPAT_BASE_URL` / `OPENAI_COMPAT_API_KEY` / `OPENAI_COMPAT_MODEL`
   - [internal/llm/runner.go::buildRootModel](internal/llm/runner.go) 按 `Provider` 选 `gemini.NewModel` 或 `openai.NewOpenAIModel`。`Provider` 留空 / `"gemini"` 都走原来的 Gemini 路径(向后兼容)
3. **grader 不受影响**:`grade_with_ocr` 内部 vision+structured 仍走 `google.golang.org/genai` SDK 直连(不经 ADK model 层),即便 root agent 换成 DeepSeek 也能照常批改

### 杂项

- [.env.example](.env.example) 加 `LLM_PROVIDER` 与 OpenAI-compat 三件套样例(默认 `gemini`,DeepSeek 切换样例写在注释里)
- 新增 `github.com/sashabaranov/go-openai v1.41.2` 依赖

### 切到 DeepSeek V4-Pro 步骤
```
LLM_PROVIDER=openai-compat
OPENAI_COMPAT_BASE_URL=https://api.deepseek.com/v1
OPENAI_COMPAT_API_KEY=sk-...
OPENAI_COMPAT_MODEL=deepseek-v4-pro
```

---

## Claude 轮 11 — 2026-05-19 — 图片传输从磁盘改内存缓存,消除 LLM 路径幻觉

### ⚠️ 破坏性变更

1. **工具参数 `image_paths` → `image_indices`**
   - `ocr_images_cached` 和 `grade_with_ocr` 现在接受 `image_indices: [0, 1, 2]` 而非文件路径
   - LLM 不再需要抄写完整文件路径,提示中只显示简短索引(如 `[0] 题目01.png`),根治 UUID 路径幻觉
2. **图片数据走内存缓存,不再落盘**
   - 新增 [internal/llm/tools/image_cache.go](internal/llm/tools/image_cache.go):`StoreSessionImages` / `GetSessionImages` / `DeleteSessionImages`
   - `buildUserContent` 不再写临时文件,改为存入内存缓存;`ProcessImages` / `callGeminiGrader` 直接读 `[]CachedImage` bytes
   - 删除 `cleanupTempFiles`、`safeFilename` 及 `os` import
   - `CacheKey` 简化为索引排序拼接(`0:1:2`),不再依赖文件 UUID 前缀
3. **`StreamOptions` 新增 `StateDelta`**,启动时注入 `_solochat_session` 到 session state,工具通过它查找图片缓存

### 杂项

- 补全 `.env` / `.env.example` 的 `OCR_IMAGELESS_MODE` / `OCR_ENABLE_IMAGE_QUALITY` / `OCR_ENABLE_MATH_OCR` 三个配置项(之前缺失导致公式识别质量差)
- 更新 HANDOFF.md 反映上述变更

---

## Claude 轮 10 — 2026-05-19 — SSE 帧去重 + 回归 OCR/批改两步 SOP

### ⚠️ 破坏性变更

1. **`grade_submission` 工具改名为 `grade_with_ocr`,且不再内部 OCR**
   - 改名:[internal/llm/tools/grader.go](internal/llm/tools/grader.go) 注册名 `grade_submission` → `grade_with_ocr`
   - 行为收紧:工具内部**不再**回退 `ProcessImages` 兜底 OCR;OCR 缓存未命中直接返回 error,提示 "请先用相同的 image_paths 调用 ocr_images_cached"
   - 系统提示词([internal/llm/prompts/prompts.go](internal/llm/prompts/prompts.go))同步改成严格两步 SOP:`ocr_images_cached(image_paths)` → `grade_with_ocr(image_paths)`,两次 image_paths 必须完全一致(与 `mechhub-agent/mechhub_agent/prompts.py::ROOT_SYSTEM_PROMPT` 对齐)
   - 前端/调用方:不需要改 HTTP 路径或请求体;但流里现在会观察到 OCR + grade 两段 `tool_call_start` / `tool_result` 帧,需要正确渲染两段 tool 状态

### 修复

1. **新建 session 的首条消息 `stale session error`**([internal/llm/sse.go](internal/llm/sse.go))
   - 现象:新会话发的第 1 条消息触发 `failed to append event to sessionService: stale session error: last update time from request (...) is older than in database (...)`,差值正好 1 µs
   - 根因:ADK Go v1.2.0 `runner.Run` 在 session 不存在时走 auto-create 分支,内存里的 `updatedAt = time.Now()` 带纳秒;MySQL `DATETIME(6)` 默认四舍五入到微秒,而 Go `UnixMicro()` 截断 → DB 比内存值大 1 µs,首条 `AppendEvent` 乐观锁误报
   - 修复:在 `StreamChat` 开头 `Get-or-Create` 一次 session([internal/llm/sse.go::ensureSession](internal/llm/sse.go)),让 runner.Run 内部走 Get 分支(返回的 session.updatedAt 已是 DB 截断后的值),绕过 ADK 的 1 µs bug。**注意**:首次尝试过 DSN 参数 `time_truncate_fractional=true`,但要求 MySQL ≥ 8.0.26;低版本会启动失败(`Unknown system variable`),所以放弃 DSN 方案改走应用层
2. **SSE 帧重复**([internal/llm/sse.go](internal/llm/sse.go))
   - 此前同一次工具调用会发出**两次** `tool_call_start`;最后一条 `text_delta` 的 `delta` 字段会塞**全文**;`text_complete` 的 `text` 字段被**拼接两次**
   - 根因:ADK Go 在 `StreamingModeSSE` 下对纯文本先 yield 若干 `Partial=true` 增量 event,再 yield 一个 `Partial=false` 的 aggregated event 回灌整段文本;旧代码两种 event 一视同仁,partial 阶段已累积过的内容在 aggregated event 又被处理了一遍。函数调用 event 偶尔也会被 ADK 重发
   - 修复:`emitEvent` 对 text part 跳过 `Partial=false` 事件(只用 partial 累积);对 function_call / function_response 按 `tool_use_id` 去重

### 杂项

1. **Postman example 同步**
   - [postman/.../Send message stream-1.example.yaml](postman/collections/MechHub Backend/solochat/messages/.resources/Send message stream.resources/examples/Send message stream-1.example.yaml):OCR 场景示例去掉了重复的 `tool_call_start` 与重复的 `text_delta` / `text_complete`,与修复后的实际流一致
   - [postman/.../Send message stream.example.yaml](postman/collections/MechHub Backend/solochat/messages/.resources/Send message stream.resources/examples/Send message stream.example.yaml):工具介绍场景里 `grade_submission` → `grade_with_ocr`,描述加上"前置必须先调 ocr_images_cached";描述里补充"批改两步流程"的典型帧序列

---

## Claude 轮 9 — 2026-05-18 — 简化 env 配置

### ⚠️ 破坏性变更

1. **删除 `GOOGLE_REDIRECT_URL` env**
   - Google OAuth redirect URL 改为 `BackendBaseURL + "/api/auth/google/callback"` 拼接
   - `config.go` 中 `GoogleConfig.RedirectURL` 字段移除

### 杂项

1. **`BACKEND_BASE_URL` 改为可选**
   - 默认 `http://localhost:<PORT>`,开发无需设置
   - 生产设 `BACKEND_BASE_URL=https://mechhub.oftheloneliness.cn` 即可
   - `avatar_url` / `attachment.url` / Google callback 统一从 `BackendBaseURL` 拼

2. **Postman `Upload attachments` 补全 body 段**
   - 添加 `formdata` body,字段 key `files`
   - 描述更新为 Go/ADK,删过时的 Python 引用

3. **`allowedMimeKind` 增加 `image/jpg`**
   - 兼容部分客户端发出的非标准 MIME

---

## Claude 轮 8 — 2026-05-16 — OSS 私有化 + 全 stream-through

### ⚠️ 破坏性变更

1. **OSS bucket 切到全新 `mechhub-oss`(私有)**
   - 旧 `mechhub-avatar`(公共读 + 自定义域名 + Let's Encrypt 证书)废弃
   - 新 bucket 默认私有,**任何直接 URL 都 403**;不需要绑域名 / 上证书
   - 旧 OSS 数据不迁移(无生产用户)

2. **`OSS_PUBLIC_BASE_URL` env 删除**
   - 不再需要,bucket 没有公共 URL
   - 旧 bucket 的自定义子域(`avatar.oftheloneliness.cn`)+ 上传的 wildcard 证书都不再用

3. **新增 env `BACKEND_BASE_URL`**(必填)
   - 后端自身对外 URL,用于拼 `avatar_url` / `attachment.url` 等 stream-through URL
   - 开发期 `http://localhost:8080`,生产同前端域名(同域 path-routing 模式)

4. **`avatar_url` 形态变了**:
   - **旧** 阿里云公共 URL `https://avatar.oftheloneliness.cn/avatars/<uid>/<hex>.jpg`
   - **新** 后端 stream-through URL `${BACKEND_BASE_URL}/api/user/avatar/<user_id>?v=<key-suffix>`
   - 前端 `<img src={user.avatar_url}>` 用法不变,**URL 字符串值变了**

5. **附件 `URL` 字段形态变了**(`AttachmentDTO.url`):
   - **旧** OSS 公共 URL
   - **新** stream-through URL `${BACKEND_BASE_URL}/api/solochat/attachments/<id>`

6. **`GET /api/solochat/attachments/:id` 行为变化**:
   - **旧** 302 重定向到 OSS 公共 URL
   - **新** 直接返回字节流(`Content-Type` 按附件 MIME,`Content-Disposition: inline`)
   - 鉴权语义加强:owner_user_id 之外的用户拿到 URL 也下不到(以前能直链 OSS,现在必须过 Go)

### 功能新增

7. **`GET /api/user/avatar/:userID` 新公共端点**
   - 无需登录,任何人按 user_id 拉头像 stream(类似 GitHub avatars)
   - `Cache-Control: public, max-age=86400` 让 CDN / 浏览器缓存
   - `?v=<key-suffix>` query 参数前端用于缓存破坏(用户换头像后 key 变,v 变,浏览器自动重拉)
   - 没设头像 / userID 不存在 → 404

8. **`GET /api/solochat/attachments/:id` 改 stream-through**
   - 后端用 OSS AK 下载 → `c.DataFromReader` 边读边写浏览器
   - 内存占用恒定(16KB buffer),不全缓存

### 配置 / 代码变化

9. **`internal/storage/oss.go`** 删 `PublicURL` 方法 + `publicBase` 字段
10. **`internal/user/service.go`** 加 `OpenAvatar` / `AvatarURL(userID, key)` / `cacheBust` / `mimeFromKey`
11. **`internal/solochat/service.go`** 加 `OpenAttachment` / `AttachmentURL(fileID)`
12. **`internal/config/config.go`** 删 `OSSConfig.PublicBaseURL`,`AppConfig` 加 `BackendBaseURL`
13. **Postman** `Get attachment` 描述改 stream-through;新增 `Get avatar.request.yaml`;集合变量加 `userId`

### 部署影响

- **生产 `.env`** 必须加 `BACKEND_BASE_URL=https://mechhub.oftheloneliness.cn`(同前端域名)
- **`docker-compose.yml`** 父目录无需改(BACKEND_BASE_URL 从 `mechhub-back/.env` 透传)
- 阿里云控制台:
  - 新建 `mechhub-oss` bucket,**权限选私有**(默认)
  - **不需要**绑域名,**不需要**上证书,**不需要**开公共读
  - 旧 `mechhub-avatar` 可以删(或留着归档)

### 给前端的 TODO

- [ ] `<img src={user.avatar_url}>` 行为不变;但**如果跨域**(dev 前端 5173 / 后端 8080)、且未来要给附件加 `<img crossorigin="use-credentials">`,确认 CORS 已放行
- [ ] 上传头像后,API 返回的新 `avatar_url` 带 `?v=` 跟旧 URL 不同,React state 自然刷新 ✓

---

## Claude 轮 7.1 — 2026-05-15 — 加 Stop API

### 功能

1. **`POST /api/solochat/conversations/:id/messages/stop`** 取消该对话内
   in-flight 的 SSE stream。幂等(没有 in-flight 也返 200)。鉴权同其它端点
2. **implicit-cancel-on-new-send**:同一对话再发 `/stream` 时,自动取消旧的
   in-flight stream,与 ChatGPT / Claude UI 行为一致
3. **`finish_reason="cancelled"`**:用户主动停时,`message_done` 帧的
   `finish_reason` 是 `cancelled`,**不再发 `error` 帧**。前端可据此区分
   "用户停的" vs "agent 错的"

### 实现

- `internal/solochat/service.go`:`Service.activeStreams sync.Map` 记录
  `userID:conversationID → *streamHandle{cancel}`;`SendMessageStream`
  入口 `Swap` 注册 + cancel 旧 entry,出口 `CompareAndDelete` 防误删
- `internal/llm/sse.go::StreamChat`:在 iter 循环和退出后双重检测
  `context.Canceled`,把 finishReason 改成 `cancelled`;附件绑定的
  append_event 用独立 timeout ctx,不被取消 ctx 影响
- 新 handler / route / Postman 文件
- `stop_test.go`:并发原语单测覆盖 Swap / Cancel / CompareAndDelete 三个场景

---

## Claude 轮 7 — 2026-05-15 — Python 退役,Go + ADK Go + MySQL 单仓收敛

### ⚠️ 破坏性变更

1. **`mechhub-agent` Python 服务整体退役**
   - Go 端直接用 ADK Go (`google.golang.org/adk` v1.2.0) 跑 Gemini 2.5 Flash,自带 session 持久化(GORM dialector → MySQL),不再依赖 Python HTTP agent
   - `internal/agent/` 包(HTTP 客户端)**整个删除**;`AGENT_BASE_URL` / `AGENT_REQUEST_TIMEOUT_SECONDS` env 不再使用
   - 父目录 `docker-compose.yml` 需要:**删** `agent` 服务、**删** `mongo` 服务、**加** `mysql:8` 服务,并把 `GEMINI_API_KEY` / `MYSQL_DSN` / `DOCUMENTAI_*` 加入 go-backend 的 env(本仓库不维护 compose 文件)
   - mechhub-agent 仓库**归档不删**,以后想切回 LiteLLM/qwen 还能看到参考实现

2. **Mongo → MySQL 全量迁移**
   - 五张业务集合(users / tokens / sessions / solochat_conversations / uploaded_files)走 GORM + `gorm.io/driver/mysql`
   - `cookie session` 表改名 `user_sessions`,避开和 ADK Go 自动建的 `sessions` 表撞名(同一个 MySQL database 内)
   - 主键类型:`bson.ObjectID`(24 hex)→ UUID v4 字符串(36 char)。前端 API 响应中 `Message.id` / `Conversation.id` / `User.id` 等所有 ID 长度都变了,前端继续把它们当 opaque 字符串就行
   - TTL:Mongo 的 `expires_at` TTL index 没了,改用 `internal/db.StartTTLCleanup` 后台 goroutine 每 60s `DELETE WHERE expires_at < NOW()`,等价行为
   - 启动期 GORM `AutoMigrate` 自动建表 + ADK Go 自动建 sessions/events/app_states/user_states。无生产数据,不做老数据迁移

3. **env 变化**
   - **删**:`MONGO_URI` / `MONGO_DB` / `AGENT_BASE_URL` / `AGENT_REQUEST_TIMEOUT_SECONDS` / `SOLOCHAT_MIGRATE_DROP_GRADING` / `SOLOCHAT_MIGRATE_DROP_MESSAGES`
   - **加**:`MYSQL_DSN` (e.g. `mechhub:pwd@tcp(mysql:3306)/mechhub?charset=utf8mb4&parseTime=true&loc=UTC`)、`GEMINI_API_KEY`、`GEMINI_MODEL` (默认 `gemini-2.5-flash`)、`GEMINI_GRADER_MODEL` (可选,留空复用 GEMINI_MODEL)、`GOOGLE_CLOUD_PROJECT_ID` / `DOCUMENTAI_LOCATION` / `DOCUMENTAI_PROCESSOR_ID` / `GOOGLE_APPLICATION_CREDENTIALS`(Document AI 鉴权)

### 新增 / 内部架构

4. **`internal/llm/` 包(新)** 取代 `internal/agent/`:
   - `runner.go::Bootstrap` —— Gemini 模型 + LlmAgent + Runner + `session/database.NewSessionService(mysql.Open(dsn))` + AutoMigrate
   - `sse.go::StreamChat` —— 跑一轮 agent,把 `iter.Seq2[*session.Event, error]` 翻译成 8 种 SSE 帧(message_start / reasoning_delta / text_delta / text_complete / tool_call_start / tool_result / error / message_done);沿用 Round 6 的 `_solochat_attachments_<invocation_id>` state 模式做附件绑定
   - `sessions.go::ListMessages` —— 直接查 ADK session,events 按 invocation_id 分组翻译成 MessageDTO
   - `tools/ocr.go` —— Document AI Go 客户端,移植自 Python `tools/ocr.py`,cache key 仍按 `file_id` 反查
   - `tools/grader.go` —— Gemini structured-output(`ResponseSchema=schemas.Schema()`),移植自 Python `providers/google.py`
   - `prompts/prompts.go` —— ROOT_SYSTEM_PROMPT + BuildGradingPrompt,1:1 移植
   - `schemas/grading.go` —— GradingOutput Go struct + `*genai.Schema`,对应 Pydantic GradingOutput

5. **`cmd/adkpoc/main.go`** 保留作为 Stage 1 PoC 参考。可随时 `go run ./cmd/adkpoc` 单跑一次 ADK Go + sqlite 流程验证

6. **`internal/db/mysql.go`** —— `Connect(dsn) -> *gorm.DB`;`StartTTLCleanup` 替代 Mongo TTL 索引
7. **`internal/db/smoke_test.go`** —— Stage 2 GORM 五表 roundtrip 测试(sqlite 内存库),`go test ./internal/db/` 跑一遍验证 repo 没漏改

### 移除

8. 删:`internal/agent/` 整个包(client.go / model.go)、`internal/db/mongo.go`、`Message` / `MessageFile` Mongo 持久化结构(round 6 已删)、`go.mongodb.org/mongo-driver/v2` 依赖

### 编码前未验证假设(已实测)

- ADK Go `session/database.NewSessionService(gorm.Dialector)` 接 sqlite + GORM ✅(Stage 1 PoC);MySQL via `gorm.io/driver/mysql` 同样走 dialector 接口,理论上等价。生产部署时第一次启动确认 ADK 自动建表能跑通即可
- `runner.WithStateDelta` 写 session.state 跨 instance 持久 ✅(Stage 1 PoC 跑过)
- Gemini 2.5 vs qwen 中文 / 数学 / 视觉能力差异:本轮先跑通,效果差再 round 8 加 LiteLLM-go 等价物 / qwen 适配器

---

## Claude 轮 6 — 2026-05-15 — 单一事实源:ADK Session 持久化到 SQL,Go 不再存消息

### ⚠️ 破坏性变更

1. **消息源整体迁到 mechhub-agent 的 ADK SQL 库,`solochat_messages` 集合废弃**
   - Mechhub-agent 用 ADK 官方 `DatabaseSessionService` 持久化 session events + state(含 OCR 缓存、本期新增的附件绑定),开发默认 `sqlite+aiosqlite:///./adk_sessions.db`,线上切 `mysql+asyncmy://...`
   - Go 端的 `solochat_messages` / `solochat_message_files` 两张表彻底不用,`Message` / `MessageFile` struct + 相关 repo 方法删除
   - `GET /api/solochat/conversations/:id/messages` 改为**代理** Python `GET /sessions/:id/messages`,Python 把 ADK events 按 invocation_id 分组翻译成 `MessageDTO[]`,Go 端只 hydrate 附件元数据
   - 收益:Python 重启不再失忆;OCR 缓存跨重启不丢;同一份数据,前端按 part type 选择性展示
   - **前端**:`GET /messages` 响应形状不变(`{messages: [...]}`),但 `Message.id` 不再是 Mongo ObjectID,变成 ADK event UUID。代码继续把它当字符串 key 用即可,不要硬编码长度

2. **`POST /messages/stream` 不再持久化 user / assistant 消息**
   - Go 端 `SendMessageStream` 不再 `InsertMessage` / `FinalizeMessage`,真正持久化全在 Python 的 ADK 流里完成
   - `StreamUserInput` 事件的 `message.id` 改为 `pending-user-<random>` 临时占位;待 stream 结束后,前端如需 canonical ID 可重新 `GET /messages` 拿
   - `Message.status` 字段在持久化层取消(ADK events 本身没有 streaming/completed/failed 状态机概念),响应里固定回 `completed`;失败走 `error` 事件,不再二态

3. **新增 env `SOLOCHAT_MIGRATE_DROP_MESSAGES`**
   - 默认 false。首次部署轮 6 时设 true,会启动期一次性 drop `solochat_messages` + `solochat_message_files` 两个集合(类似轮 4 的 `SOLOCHAT_MIGRATE_DROP_GRADING`),drop 完改回 false 即可

4. **`mechhub-agent` 新增 env `ADK_DB_URL`**
   - 开发不设默认走 `sqlite+aiosqlite:///./adk_sessions.db`(进程工作目录下)
   - 生产请设 `mysql+asyncmy://user:pwd@mysql:3306/mechhub_adk` 等异步驱动地址
   - 注意:ADK `DatabaseSessionService` 走 SQLAlchemy **异步引擎**,所以 sqlite 必须 `+aiosqlite`、mysql 必须 `+asyncmy`(或 `+aiomysql`),同步驱动 `pymysql` 会启动报错
   - 父目录的 `docker-compose.yml` 需要新增 `mysql:8` 服务并把 `ADK_DB_URL` 加入 agent 的 env(本仓库不维护 compose 文件,请同步改父目录)

### 功能

5. **POST /chat 接受新 form 字段 `file_ids`**:Go 端发起时把附件的 Mongo `uploaded_files._id` 按 files 顺序附带传过去(JSON 数组字符串)。Python 用作两件事:
   - 流跑完后写 session.state 的 `_solochat_attachments_<invocation_id>` 键,绑定附件与本轮用户消息
   - 落盘文件名前缀从时间戳改成 `<file_id>-<filename>`,让 `ocr_images_cached` 工具的 cache key 用 file_id 反查,跨重启稳定

6. **新增 `GET /sessions/:session_id/messages` (Python 端)**:把 ADK events 按 invocation_id 分组,user 与 model events 折成两条 `MessageDTO`,parts 类型齐全(text/thinking/tool_use/tool_result),user message 带 `attachments: [{id}]`(file_id 数组,URL/MIME 等元数据 Go 端 hydrate)

### 移除

7. **删除 Go 端文件 / 函数**:
   - `internal/solochat/model.go`:`Message`、`MessageFile`、`MessageStatusStreaming/Completed/Failed`、`textPart`、`toMessageDTO`
   - `internal/solochat/repo.go`:`messages` / `messageFiles` 集合句柄;`InsertMessage` / `ListMessages` / `FinalizeMessage` / `BindMessageFiles` / `FindMessageFiles` / `CountConversationMessages`
   - `internal/db/mongo.go`:`solochat_messages` 与 `solochat_message_files` 的索引注册
   - `internal/solochat/service.go::consumeAgentStream` 简化为 `forwardAgentStream`(不再 parts 累积)

### 杂项

8. **HANDOFF.md** 更新对接图与典型时序;**Postman**:`List messages` 描述讲清是代理 Python 端点,`Send message stream` 增加 file_ids 字段
9. **CLAUDE.md「数据库」段** 同步:删 `solochat_messages` 描述,加一段「消息源由 mechhub-agent ADK SQL 库托管」说明

### 编码前未验证假设(已实测)

- ADK 1.33 `DatabaseSessionService` 走 SQLAlchemy **异步**引擎 ✅(需 `+aiosqlite` / `+asyncmy`)
- `Runner.run_async(invocation_id=...)` 被 ADK 内部覆盖为 `e-<uuid>`,**外部传入无效** —— 改为流过程中捕获实际 invocation_id,流末追加 state-only event 写 state_delta
- 同 session 跨 SessionService 实例重建,events + state 完整保留 ✅(SQLite 实测)

---

## Claude 轮 5 — 2026-05-14 — 多模态附件真喂 LLM + 协议切到 SSE

### ⚠️ 破坏性变更

1. **`POST /messages/stream` 响应从 NDJSON 改为 SSE**
   - **旧** Content-Type `application/x-ndjson`,帧 `{json}\n`
   - **新** Content-Type `text/event-stream`,帧 `data: {json}\n\n`,每 25s 心跳 `: ping\n\n`
   - 事件 type 字段不变(共 10 种,见 HANDOFF)。前端解析改:从 `splitLines().map(JSON.parse)` 改为按 `\n\n` 切帧、去掉 `data: ` 前缀、忽略 `:` 开头的注释行、再 `JSON.parse`。
   - 与 OpenAI / Anthropic / Vercel AI SDK 等业界主流流式 API 对齐。**不使用** EventSource(POST 不支持),前端继续用 `fetch` + `ReadableStream`

2. **附件 MIME 白名单收紧**
   - 删除 `text/markdown` 之外的灰区,保留:`image/jpeg|png|webp|gif`、`application/pdf`、`text/plain`、`text/markdown`
   - DOCX / Office 文档明确拒绝(返回 400 中文提示)。Python agent 不再对未知 MIME 静默丢弃

### 功能修复

3. **附件真正喂给 LLM(轮 4 的 bug 修复)**
   - 旧实现:`build_message_with_attachments` 只把文件路径作为字符串拼到 prompt,LLM 看不见图片;只有主动调 `grade_submission`/`ocr_images_cached` 才能看图
   - 新实现:`mechhub-agent/server/upload.py::build_user_content` 把附件读字节后:
     - 图片 → `Part.from_bytes(mime_type="image/...", data=)` 直接喂 ADK,LLM 能看
     - PDF → `Part.from_bytes(mime_type="application/pdf", data=)`,qwen-vl 原生消化
     - 文本 / Markdown → 读 utf-8 内容 inline 拼 prompt,30k 字符截断
   - 图片同时落盘一份(`tempdir/mechhub/<session>/`),保留给工具复用

### 杂项

4. **CLAUDE.md「流式接口」段重写** —— 删 NDJSON 约定,统一 SSE。POST + SSE 单端点为本项目唯一流式形态。
5. **Postman 同步**:`Send message stream` / `Upload attachments` 描述更新事件帧 + MIME 白名单

### 编码前未验证假设

- qwen-vl 通过 LiteLlm(OpenAI 兼容端点)能否吃 `Part.from_bytes(mime_type="application/pdf")` —— 不行就降级 pypdf 抽文本
- ADK Content with multimodal Parts 透传 LiteLlm 后是否被正确转成 OpenAI vision messages —— 实测验证
- SSE 帧在反代下不被缓冲 —— `X-Accel-Buffering: no` + 心跳两道保险

---

## Claude 轮 4 — 2026-05-14 — Solochat 重构为通用 agent chat,前端可见 thinking + tool

把 grading 从独立路径并入通用对话流,前端从此能渲染 agent 的思考过程 + 工具调用 + 工具结果。

### ⚠️ 破坏性变更(前端 / Postman / DB 全部需要跟改)

1. **`POST /messages/stream` 事件协议完全更换**
   - **旧** 6 种: `user_input` / `assistant_start` / `assistant_delta` / `assistant_done` / `assistant_error` / `conversation_title`
   - **新** 10 种(典型时序):
     ```
     user_input → message_start → reasoning_delta* → tool_call_start
       → tool_result → text_delta* → text_complete → message_done
     ```
     额外: `error`、`conversation_title`(只在首条消息)
   - 前端需要按 `type` 分别渲染:`reasoning_delta` 进思考折叠区,`tool_call_start` + `tool_result` 配对渲染成工具调用块,`text_delta` 流式追加到当前文本块
   - `assistant_delta` 不再发,改用 `text_delta`;`assistant_done` 不再发,改用 `message_done`(包含 `finish_reason`)
   - 错误事件字段:`{type:"error", code, error}`,以前是 `assistant_error`

2. **`Message.Content string` 字段消失,改为 `Message.Parts []MessagePart`**
   - 每个 part 类型: `text` / `thinking` / `tool_use` / `tool_result`
   - 前端 `GET /messages` 拿到的每条 message 现在是 `parts[]`,按数组顺序逐块渲染
   - DB 层 `solochat_messages` 文档 schema 改变 —— **历史消息不做迁移**(单人开发,无生产用户)

3. **`/api/solochat/grading-tasks/*` 全部 5 个端点下线**
   - `GET/POST /conversations/:id/grading-tasks` / `GET /grading-tasks/:id` / `POST /grading-tasks/:id/retry` / `GET /grading-tasks/:id/events`(SSE)全部删除
   - 批改不再是独立功能。用户上传作业图片后,agent 自主决定是否调用 `grade_submission` 工具,完整 `GradingOutput` 通过 `tool_result.output` 字段返回给前端
   - 前端原批改页 UI 改为从消息流里识别 `tool_use.name == "grade_submission"` 的块,从对应的 `tool_result.output` 渲染分数 + 评语 + 步骤分析

4. **Mongo 三张表废弃**: `solochat_grading_tasks` / `solochat_grading_task_files` / `solochat_grading_annotations`
   - 启动时设 `SOLOCHAT_MIGRATE_DROP_GRADING=true` 让后端在 `EnsureIndexes` 后一次性 drop 这三张表(`internal/db/mongo.go::MaybeDropLegacyGradingCollections`),drop 完改回 false

5. **附件 MIME 白名单放开** —— Go 把所有附件转给 Python(不再只过滤 image)。Python 端 `server/upload.py::ALLOWED_MIMES` 新增 `text/markdown`

6. **env 删除**: `SOLOCHAT_HISTORY_REPLAY_LIMIT`(从未被代码引用,清理掉)
   **env 新增**: `SOLOCHAT_MIGRATE_DROP_GRADING`(一次性迁移开关)

### 功能新增

7. **思考过程可见** —— `mechhub-agent` 的 LiteLlm 默认传 `extra_body={"enable_thinking": True}`(`mechhub_agent/agent.py::_build_model`),让 qwen 把推理过程作为 `thought=True` 的 part 返回,server/sse.py 转成 `reasoning_delta` SSE 事件,Go 端再 1:1 转 NDJSON,前端可流式展示。`AGENT_ENABLE_THINKING=false` 可关。

8. **工具调用可见** —— Python 端 `before_tool_callback` / `after_tool_callback` 通过 contextvar 把 `tool_call_start` / `tool_result` 事件入队(`mechhub_agent/callbacks.py`),server/sse.py 把队列和 ADK 事件流合并发出,Go 端 1:1 转发。每次 tool 调用前端能看到工具名 + 完整入参 + 完整结果 + 耗时。

9. **ADK 真流式** —— `server/runner.py::get_run_config` 默认开启 `RunConfig(streaming_mode=StreamingMode.SSE)`,LLM 文字按 token 增量回来,前端看到的就是真正逐字流式。

10. **附件存到消息上** —— `GET /messages` 返回的 `MessageDTO.attachments[]` 包含完整 AttachmentDTO 列表(以前要客户端拼)

### 删除

- `internal/solochat/grading.go`(258 行,grading 异步任务编排)
- `internal/solochat/events_hub.go`(63 行,grading 专用 pub/sub)
- `internal/solochat/streamer.go::newSSE` + 心跳逻辑(grading 走完后不再需要 SSE)
- `mechhub_agent/tools/grader.py::grade_with_ocr` → 改名 `grade_submission`,删除"必须先 OCR"硬校验(交给 LLM 自主决定)
- Postman 路径 `solochat/grading-tasks/*` 全部 5 个 yaml + 根集合 `taskId` 变量

### 编码前需验证的假设

- qwen3.6-max-preview 通过 LiteLlm + DashScope OpenAI-兼容端点 + `extra_body={"enable_thinking": True}` 能否把 thought parts 透出 —— 没透出就降级到 `AGENT_ENABLE_THINKING=false`
- ADK callback 在 async 上下文执行(目前 `queue.put_nowait` 假设是 async 同线程,若实际是工作线程需要改 `run_coroutine_threadsafe`)
- ADK `function_call` 事件粒度 —— 协议留了 `tool_call_delta` 但目前不发,因为 ADK 一次性给完整 args

---

## Claude 轮 3 — 2026-05-13 — Solochat 模块 + Python Agent 对接 + Docker 化

### 功能新增

1. **`internal/agent/` 新包**:Python ADK agent 的 HTTP 客户端
   - `POST /chat` multipart 构造 + SSE 解析
   - 支持 session_id / message / 0~N 张 images
   - 事件类型:`tool_start` / `tool_done` / `text` / `error` / `done`

2. **`internal/solochat/` 新业务模块**:复刻 `Mechhub-miniback` 的对话 + 批改架构,共 **13 条端点**
   - 对话:`GET/POST/PATCH/DELETE /api/solochat/conversations[/:id]`
   - 消息:`GET /messages` + `POST /messages/stream`(**NDJSON 流式**)
   - 批改任务:`GET/POST /conversations/:id/grading-tasks`、`GET/POST /grading-tasks/:id`、`/retry`、`/events`(**SSE**)
   - 附件:`POST /attachments`(multipart 上传)、`GET /attachments/:id`(302 跳 OSS)

3. **NDJSON 消息流式协议**(`POST /messages/stream`)
   - 事件:`user_input` / `assistant_start` / `assistant_delta` / `conversation_title` / `assistant_done` / `assistant_error`
   - 首条消息自动用前 24 字符当 conversation 标题

4. **SSE 批改进度协议**(`GET /grading-tasks/:id/events`)
   - 事件:`ready` / `grading_status` / `grading_annotation`(预留)
   - 心跳每 25s,防代理超时
   - 任务到达 completed/failed 自动断开

5. **批改任务异步执行**
   - HTTP 请求只返回 NDJSON 起步事件(`grading_start`),实际执行在 goroutine
   - 内存事件总线(`solochat/events_hub.go`)做 fan-out 给 SSE 订阅者
   - 多个 SSE 客户端可同时订阅同一个 task

6. **附件上传流转**
   - 前端 multipart → Go 写 OSS → DB 存 key
   - 发消息时 Go 下载 OSS → multipart 转发 Python agent
   - MIME 白名单:image/* + text/plain + application/pdf,单文件 ≤ 20 MiB,每条消息 ≤ 4 张

7. **失败恢复**:启动时 `MarkAllProcessingFailed` 把上次重启时挂着的批改任务全标 `failed`(避免孤儿任务)

8. **Docker 化**
   - `mechhub-back/Dockerfile`:Go 多阶段构建,alpine 底
   - `mechhub-agent/Dockerfile`:python:3.11-slim + uvicorn
   - 父目录 `mechhub/docker-compose.yml`:go-backend + agent + mongo 三服务,内部网络通信,只暴露 8080

### 配置新增(`.env.example` + `internal/config/config.go`)

- `AGENT_BASE_URL`(必填,本机开发用 `http://localhost:8001`,Docker 内用 `http://agent:8001`)
- `AGENT_REQUEST_TIMEOUT_SECONDS`(默认 600,即 10 分钟)
- `SOLOCHAT_MAX_ATTACHMENTS_PER_MESSAGE`(默认 4)
- `SOLOCHAT_MAX_FILE_SIZE_BYTES`(默认 20 MiB)
- `SOLOCHAT_HISTORY_REPLAY_LIMIT`(默认 20,后续给 agent 重放上下文用)

### 数据库

- 7 个新集合 + 索引:`solochat_conversations` / `solochat_messages` / `solochat_grading_tasks` / `solochat_grading_task_files` / `solochat_grading_annotations` / `uploaded_files` / `solochat_message_files`
- 全部加 user_id / conversation_id / task_id 索引

### 未完成 / 已知 gap

- **annotations 还是空表**:当前 Python agent 的 SSE 不输出结构化 `GradingOutput`(只输出 LLM 文本 + tool_done summary)。Go 解析 summary 拿到 `overall_score`,但 bbox + 文本识别 + 评语等明细需要 agent 端扩展 SSE 协议输出 `grading_result` 事件
- **agent session 持久化**:agent 当前是 in-memory session,重启缓存丢。每次 grading 都重传图片,先接受这个成本
- **agent 不带 auth**:`http://agent:8001` 在 Docker 内不暴露,假设内网可信。若以后跨主机部署需加 mTLS 或 shared secret

### 给前端 / 测试方的 TODO

- [ ] zuChat / 类似 store 接 NDJSON 流(Response 用 `getReader()` + TextDecoderStream 按行切)
- [ ] SSE 用浏览器原生 `EventSource`(注意 EventSource 不支持自定义 header,session_id cookie 自动带 OK)
- [ ] Postman 测试:Phase 4 完成后可全套跑通,见 `postman/collections/MechHub Backend/solochat/...`

---

## DeepSeek 轮 — 2026-05-13 — 邮件模板美化 + 国际化 + API 形状调整

### ⚠️ 破坏性变更(前端 / Postman / 调用方注意)

1. **`POST /api/auth/login` 响应形状改变**
   - **旧**: `{ code, msg, data: { message, userdata: { id, email, name, role, avatar_url, ... } } }`
   - **新**: `{ code, msg, data: { id, email, name, role, avatar_url, ... } }`(直接是 `MeResp`,无 wrapper)
   - 前端如果之前读 `response.data.userdata`,现在改读 `response.data`
   - `LoginResp` / `UpdateProfileResp` 两个 wrapper 类型已从 `internal/user/model.go` 删除

2. **`POST /api/user/update-profile` 响应形状改变**
   - 同上,直接返回 `MeResp`,不再包 `{ message, userdata }`

3. **邮件中链接路径全部改成 `/verify/<type>?token=` 统一前缀**
   - `/verify?token=` → **`/verify/student?token=`**
   - `/approve-teacher?token=` → **`/verify/teacher?token=`**
   - `/reset-password?token=` → **`/verify/reset-password?token=`**
   - 这是邮件正文里的链接路径,**对应前端路由 `/verify/:type`**。后端的接收端点(`/api/auth/verify` 等)路径**不变**,只是邮件里前端落地页 URL 变了
   - `APP_BASE_URL` 仍是前端根 URL

### 功能新增

4. **邮件模板全面卡片化**(`internal/mail/resend.go`)
   - 新增统一 `cardLayout` HTML 模板:深色背景、品牌色按钮、Logo + 背景图、表格布局
   - 三种邮件(验证学生 / 审批教师 / 重置密码)共用 `cardLayout`,只传不同 `cardArgs`
   - 教师审批邮件加 `infoBlock`(显示姓名 / 邮箱 / 角色)
   - 邮件正文全部中文
   - HTML 转义 title / description / footer / 用户输入字段防 XSS

5. **响应 / 错误消息全部中文化**
   - `response.OK` 默认 `msg` 从 `"ok"` 改成 `"成功"`(`internal/response/response.go`)
   - Auth 中间件 401 msg 从 `"unauthorized"` 改成 `"未登录"`
   - 所有 handler 的 Fail msg 全部中文(`"邮箱或密码错误"` / `"账号尚未验证"` / `"当前密码错误"` 等)

6. **Google 头像拉大图**(`internal/user/service.go::largeGooglePictureURL`)
   - 镜像 Google 头像到 OSS 前,把 URL 的 `=s96-c` 之类的尺寸参数替换成 `=s512-c`,默认拿到 512×512 高清版
   - 只对 `googleusercontent.com` 域名生效,其他 URL 原样返回

### 配置新增

7. **新增可选 env**(`.env.example` + `internal/config/config.go::MailConfig`)
   - `MAIL_LOGO_URL` — 邮件 header 显示的 Logo URL(可选,空字符串表示不显示)
   - `MAIL_BG_URL` — 邮件外层背景图 URL(可选)
   - 两个都用 `getEnv(..., "")`,**不是必填**。没填邮件也能正常发,只是没图

### 杂项

8. `.gitignore` 加 `.kilo`(kilocode 工具本地目录)

### 给前端的 TODO(下一步)

- [ ] zuAuth.login 把 `state.data = response.data.userdata` 改成 `state.data = response.data`(如果之前是前者)
- [ ] zuAuth.changeProfile 同上
- [ ] 前端 `verify/:type` 路由要能处理 `student` / `teacher` / `reset-password` 三种 type(看代码已经有 `VerifyPage`,确认逻辑对得上)

---

## Claude 轮 2 — 2026-05-12 — Google OAuth

- 新增 `GET /api/auth/google` 和 `/callback` 端点
- 新增 `internal/oauth/google.go` 封装 oauth2 客户端
- User 模型加 `GoogleSub` 字段,首次登录自动镜像 Google 头像到 OSS
- 简化:登录后一律跳 `GOOGLE_DEFAULT_RETURN_URL`,**不接受 `?return=` 参数**(消除 Open Redirect 攻击面)
- HANDOFF.md 加部署计划、Cookie 配置矩阵、未结安全债务清单

## Claude 轮 1 — 2026-05-09 ~ 2026-05-11 — 用户系统骨架

- 项目骨架建立:`CLAUDE.md` 项目宪法、目录结构、配置约定
- 11 条端点上线:register / verify / login / logout / forgot-password / reset-password / me / update-profile / avatar / change-password / approve-teacher
- 角色系统(student / teacher)+ 老师 admin 审批流程
- 头像上传至阿里云 OSS(代理上传 + 内容寻址 + 旧文件 best-effort 删除)
- Postman Spec Hub 格式集合 + `postman/README.md`
- 修了几个安全 bug:重复注册不覆盖密码(防账号劫持)、改密后 session 全清等
