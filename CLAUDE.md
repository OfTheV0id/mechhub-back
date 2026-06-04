# MechHub Backend — 项目规约

> 唯一的协作规约文件。技术栈、约定、关键决策、当前接口、待结账。所有 AI 与人工 PR 必须遵守。

## 工作偏好(最高优先级)

1. **倾向先进的、根本的方案,而不是暂时的解决办法。** 看到位置耦合、隐式状态、临时 fallback、绕开 API 限制的 hack——优先从数据/接口边界改掉根因,不要叠补丁。需要做权宜之计时,主动说明它是临时的、根因在哪、什么时候清掉。
2. **少兜底,多信任内部边界。** validator 在 handler 入口校验一次,service / repo 信任入参,不写防御性代码。错误处理就是 `if err != nil { return err }`。
3. **不预先抽象。** service / repo 用 struct + 方法,不预先定义 interface,需要 mock 时再抽。不为单实现写 factory / builder。三段相似代码不抽函数,五段以上再考虑。
4. **不写废话注释。** 默认不写,只有"为什么这样写"非显而易见时写一行。禁止 `// register 处理用户注册` 这种复读机注释,禁止 PR 上下文注释(`// added for issue #123`)。
5. **依赖注入,无全局变量。** `*gorm.DB`、配置、mail sender、`*llm.Service` 在 `main.go` 装配,`New(...)` 注入。禁止 package-level 全局。
6. **路由可见性。** 每个模块自带 `route.go`,所有路径 + handler 一眼可见。`internal/router/router.go` 只 mount,不写路径。
7. **每加一个 HTTP 接口,必须同步加 Postman 文件。** 见 `postman/README.md`。
8. **重要决策让我选,不要替我做。** 换数据库、加 Redis、安全 trade-off、引入新依赖这种,主动给选项。
9. **遇到安全/隐私 trade-off,主动告诉我**,不默默选默认。
10. 中文交流,直接答案 + 简洁说明,不要长篇铺垫。

## 技术栈

- Go(`go.mod` 已定基线)+ `gin-gonic/gin`
- MySQL via GORM(`gorm.io/driver/mysql`),启动期 `AutoMigrate` 建表;主键 UUID v4 字符串(`char(36)`)
- 配置:`.env` + `joho/godotenv`,`internal/config` 集中加载
- 邮件:Resend(`resend-go/v3`)
- 对象存储:阿里云 OSS(`aliyun/aliyun-oss-go-sdk`),bucket 私有,无公共 URL
- OAuth:Google,`golang.org/x/oauth2`
- 密码:bcrypt cost=12
- 认证:Session ID + HttpOnly Cookie,session 存 MySQL `user_sessions` 表 + 后台 TTL 清理 goroutine。**不用 JWT**
- LLM:**Go 直接接 ADK Go**(`google.golang.org/adk` v1.2.0)+ Gemini 2.5 Flash 原生 SDK(`google.golang.org/genai`)+ Document AI Go 客户端。`internal/llm/` 是边界,Go 内部直接跑 agent / tools / session,**没有 Python 服务**
- 部署:Docker Compose(父目录 `mechhub/docker-compose.yml`,两服务 `go-backend` + `mysql:8`)

## 目录约定

```
mechhub-back/
├── main.go                       # 入口,只做依赖装配 + listen
├── Dockerfile                    # 多阶段构建,alpine 输出
├── .env / .env.example
└── internal/
    ├── config/                   # typed Config,启动时校验必填
    ├── db/                       # gorm MySQL client + AutoMigrate + 后台 TTL cleanup
    ├── mail/                     # 邮件发送
    ├── middleware/               # cors / auth
    ├── session/                  # cookie session(user_sessions 表)
    ├── storage/                  # OSS 客户端封装(Upload / Download / Delete)
    ├── oauth/                    # Google OAuth 客户端
    ├── llm/                      # ADK Go 封装(Gemini + tools + SSE 翻译)
    │   ├── runner.go             # Bootstrap:agent + runner + session DB service
    │   ├── sse.go                # StreamChat:event 流转 SSE 帧
    │   ├── sessions.go           # ListMessages:读 ADK session events 翻译成 MessageDTO
    │   ├── prompts/              # 系统提示词 + grading 提示词
    │   ├── schemas/              # GradingOutput Go struct + genai.Schema
    │   └── tools/                # ocr.go (Document AI) + grader.go (Gemini structured output) + search.go (Tavily web_search) + image_cache.go
    ├── response/                 # 统一响应 + 错误码常量
    ├── router/                   # 装配路由,不写具体路径
    └── <feature>/                # 功能模块,自带五件套
        ├── route.go              # Mount(g, h, ...) 一眼可见全部路由
        ├── handler.go            # 参数解析 + 调 service + 返回
        ├── service.go            # 业务逻辑
        ├── repo.go               # 数据访问 (GORM)
        ├── model.go              # struct + DTO
        └── *.go                  # 其它本模块文件
```

当前 feature module:`user/`、`solochat/`(含 `streamer.go` 写 SSE + `seed.go`)、`course/`(学习板块,含 `assessment.go` / `statics.go`)、`class/`(班级,含 `invite.go`)、`channel/`(班级频道,含 `cursor.go` / `fork.go` / `share.go`)、`assignment/`(作业)、`realtime/`(WebSocket Hub,`conn.go` / `hub.go`,无 repo —— 内存态)。另有基础设施 `reference/`(跨模块富引用快照)、`sseutil/`(SSE 帧 helper)。新增模块 = 复制五件套 + `router/router.go` 加一行 mount。

## 命名 / 响应 / 文案

- URL 路径 kebab-case:`/api/auth/forgot-password`
- Go 导出 PascalCase,私有 camelCase;文件名 lowercase,多词下划线:`reset_token.go`
- 响应格式 `{ "code": 0, "msg": "ok", "data": { ... } }`,`code = 0` 成功,非零业务错误码,HTTP status 同步
- 返回"当前用户信息"的端点(`/auth/login` / `/user/me` / `/user/update-profile` 等),`data` 直接是 `MeResp`,**不包 `{ message, userdata: {...} }`**
- 操作类端点(`/auth/logout` / `/auth/forgot-password` / `/auth/reset-password` / `/user/change-password`),`data` 可以是 `{ message: "..." }`
- **所有用户可见字符串中文**(`response.OK`/`response.Fail` 的 `msg`、邮件正文 / 标题 / 按钮、错误提示)。后端内部 `errors.New(...)` / 日志可以英文,但走到 HTTP 响应或邮件就转中文

## 配置

- 所有配置走 `.env`,绝不硬编码
- 新增配置必须同时更新 `.env.example` 和 `internal/config/config.go`
- 必填项启动时 panic,可选项给合理默认值

## CORS

- `CORS_ENABLED=true|false` 控制是否注册中间件
- `CORS_ORIGINS` 逗号分隔多个 origin,精确匹配,带 cookie 必须 `Allow-Credentials: true`
- 不引第三方 CORS 包,自己写一个简短 middleware

## CHANGELOG

- 每一轮 AI 协作 / 重要 PR 完成后,**必须在 `CHANGELOG.md` 顶部加一条**,按"破坏性 / 功能 / 修复 / 杂项"分组
- 破坏性变更(API 形状改、env 必填项加、链接路径改等)**必须标 ⚠️**,并说明前端 / 调用方该怎么跟着改
- 不允许"静默"修改 API 响应结构、邮件路径、env 必填项——跨边界契约必写
- 一轮工作合并写一条即可,标题用 `## <轮次> — <日期> — <主要内容>`

## 数据库

- MySQL via GORM,启动时 `gormDB.AutoMigrate(...)` 自动建表 + 索引(gorm tag 上声明)
- TTL 清理:MySQL 没有原生 TTL,`internal/db.StartTTLCleanup` 后台 goroutine 每 60s `DELETE WHERE expires_at < NOW()` 清 `tokens` + `user_sessions`
- 表命名:
  - **用户系统**:`users` / `tokens` / `user_sessions`(cookie session 表叫 `user_sessions`,避开 ADK Go 在同库自动建的 `sessions`)
  - **Solochat**:加 `solochat_` 前缀(目前只有 `solochat_conversations`,消息源走 ADK)
  - **通用**:`uploaded_files`(头像 + solochat 附件共用)
- 新模块加表用模块名前缀

### 消息源:ADK Go 的 sessions/events 表

- 对话消息(events + state + OCR 缓存 + 附件绑定)由 ADK Go `session/database.NewSessionService(mysql.Open(dsn))` 持久化到 MySQL 同库的 `sessions` / `events` / `app_states` / `user_states` 表(ADK Go 自动 migrate)
- 业务表只有:`users` / `tokens` / `user_sessions` / `solochat_conversations` / `uploaded_files`
- `GET /messages` 直接读 ADK session 然后翻译成 MessageDTO,见 `internal/llm/sessions.go`
- 附件 ↔ 用户消息绑定:流末向 session.state 写 `_solochat_attachments_<invocation_id>` = file_ids;读时按同 key 反查

## 邮件

- 所有发出去的邮件**必须经过 `internal/mail/resend.go::cardLayout`**,不要自己拼 HTML 字符串
- 加新邮件类型 = 加一个 `Send<Xxx>Email` 方法,内部构造 `cardArgs` 调 `cardLayout`,统一品牌外观
- 嵌入用户输入的字段(name / email)**必须 `html.EscapeString`** 防 XSS(尤其 admin 收的审批邮件)
- 邮件正文中文。前端落地页链接统一前缀 `<APP_BASE_URL>/verify/<type>?token=...`:
  - `student` — 学生邮箱验证
  - `teacher` — 教师审批
  - `reset-password` — 重置密码
  - 后端 API 端点路径(`/api/auth/verify` 等)不变

## Postman

- 用 Postman **Spec Hub 文件夹格式**(YAML 拆文件,VCS 友好),目录 `postman/`,详细规则见 `postman/README.md`
- 每新增 HTTP 接口,必须同步在 `postman/collections/MechHub Backend/<group>/<slug>/<Title>.request.yaml` 加一条
- 新增/修改的占位变量同步进集合根 `.resources/definition.yaml` 的 `variables` 段以及 `environments/local.environment.yaml`
- 不要用单文件 `*.postman_collection.json` 格式

## 流式接口

- **SSE** (`text/event-stream`):所有流式接口统一用 SSE。帧格式 `data: <json>\n\n`,**不用 `event:` 行**,事件类型走 JSON 内的 `type` 字段。每 25s 一行 `: ping\n\n` 心跳,防代理超时
- POST + SSE 响应体单端点(`/api/solochat/conversations/:id/messages/stream`)。前端 `fetch()` 读 response body stream,按 `\n\n` 切帧,去 `data: ` 前缀再 `JSON.parse`。**不用浏览器原生 `EventSource`**(只支持 GET 且不能带 body)
- Go 端 helper:`internal/solochat/streamer.go::newSSE` + `c.Writer.Write` + `http.Flusher.Flush`
- **必须** set `X-Accel-Buffering: no` 防 nginx / Cloudflare 缓冲

## LLM 对接(`internal/llm/`)

- 走 ADK Go + Gemini 2.5 Flash 原生 SDK。Go 自己跑 agent / tools / session,没有 Python,没有 LiteLLM 适配层
- 关键代码位置:
  - `runner.go::Bootstrap` — Gemini 模型 + LlmAgent + Runner + database session service,启动期建一次
  - `sse.go::StreamChat` — 跑一轮 agent,迭代 `iter.Seq2[*session.Event, error]` 翻译成 SSE 帧;流末用 `appendAttachmentBinding` 写 `_solochat_attachments_<invocation_id>` 到 session.state
  - `sessions.go::ListMessages` — 读 ADK session,events 按 `invocation_id` 分组翻译成 MessageDTO
  - `tools/ocr.go` — Document AI;cache key 按图片索引;state 里走 `ocr_cache`
  - `tools/grader.go` — Gemini structured output (`ResponseSchema = schemas.Schema()`);先读 OCR 缓存再批改;缓存 miss 返回 error 让 LLM 补 OCR;结果回带 `ImageRefs`(图片稳定引用),前端不需要靠"上一条消息"推导
  - `tools/image_cache.go` — 内存图片缓存(key=sessionID),LLM 用简短索引引用;`CachedImage` 带 `AttachmentID` / `URL`
- 切其它 LLM(qwen / Claude / OpenAI)需实现 `model.LLM` 接口(`Name()` + `GenerateContent(...) iter.Seq2[*LLMResponse, error]`),~200-400 行
- Solochat 模块**只通过 `internal/llm.Service` 调 LLM**,不应该自己 import `google.golang.org/genai` 或 `google.golang.org/adk`

### Solochat SSE 协议(10 种帧)

典型时序:`user_input → message_start → reasoning_delta* → tool_call_start → tool_result → text_delta* → text_complete → message_done`。

| 事件 | 字段 | 用途 |
|---|---|---|
| `user_input` | `message: MessageDTO`(含 attachments[]) | Go 持久化用户消息后立即发,前端乐观 UI 对齐 |
| `message_start` | `message_id, model` | Agent 回合开始 |
| `reasoning_delta` | `message_id, delta` | 思考过程流(可折叠) |
| `text_delta` | `message_id, delta` | 文本块流式追加 |
| `text_complete` | `message_id, text` | 当前文本块结束(完整最终文本) |
| `tool_call_start` | `message_id, tool_use_id, name, input` | LLM 调用工具,完整入参 |
| `tool_result` | `message_id, tool_use_id, output, is_error, elapsed_ms` | 工具返回(包括完整 GradingOutput) |
| `conversation_title` | `conversation: ConversationDTO` | 首条消息后自动命名 |
| `error` | `message_id, code, error` | 异常 |
| `message_done` | `message_id, finish_reason` | Agent 回合结束 |

Message 形态 `Parts []MessagePart`,part type 可选:`text` / `thinking` / `tool_use` / `tool_result`。

**ADK Go 自动生成的 `invocation_id` 是 `e-<uuid>`**(它内部覆盖外部传入的值)—— `sse.go::StreamChat` 在流过程中捕获实际 invocation_id,流末才写 state_delta。不要尝试在调用前生成 invocation_id 并相信它。

### 附件流转

前端 multipart 上传 → Go(MIME 白名单 image/PDF/text/markdown)→ Go 写 OSS → DB 存 key → 发消息时 `buildUserContent` 读 OSS 字节 → 图片/PDF 包成 `*genai.Part{InlineData: ...}` 直接给 ADK;text/markdown 内容 inline 拼到 prompt 文本。图片同时存内存缓存(`tools.StoreSessionImages`),OCR / grading 工具用索引(`[0,1,2]`)引用,无磁盘 I/O,LLM 不再接触文件路径。

## 当前已开放接口

```
# 认证
POST /api/auth/register           邮箱+密码+name+role 注册;student 发验证邮件,teacher 发审批邮件给 admin
GET  /api/auth/verify             ?token=xxx,激活 student 账号
GET  /api/auth/approve-teacher    ?token=xxx,admin 审批通过激活 teacher(不签发 session)
POST /api/auth/login              登录,set-cookie session_id
POST /api/auth/logout
POST /api/auth/forgot-password    发重置邮件(邮箱不存在也返成功,防枚举)
POST /api/auth/reset-password     用 token 设新密码,踢掉所有 session
GET  /api/auth/google             302 跳 Google 授权页(state 写入 short cookie)
GET  /api/auth/google/callback    Google 回调,换 token、拉用户、首登镜像头像到 OSS、set session cookie、302 回 GOOGLE_DEFAULT_RETURN_URL

# 用户
GET  /api/user/avatar/:userID     公共,stream-through 拉用户头像;`?v=` 缓存破坏
GET  /api/user/me                 返回 id/email/name/role/avatar_url/verified/created_at
POST /api/user/update-profile     改 name
POST /api/user/avatar             multipart 上传头像 → OSS
POST /api/user/change-password    改密码,踢掉所有 session

# Solochat(都需登录)
GET    /api/solochat/conversations                          列对话(updated_at desc)
POST   /api/solochat/conversations                          建对话
PATCH  /api/solochat/conversations/:id                      改标题
DELETE /api/solochat/conversations/:id                      删对话 + 所有消息
GET    /api/solochat/conversations/:id/messages             列消息(created_at asc),含 parts[] + attachments[]
POST   /api/solochat/conversations/:id/messages/stream      发消息,SSE 流式
POST   /api/solochat/conversations/:id/messages/stop        取消该对话当前 in-flight stream
POST   /api/solochat/attachments                            multipart 上传附件(image/pdf/text/markdown)
GET    /api/solochat/attachments/:id                        stream-through 取附件(私有 OSS,经后端)

# 课程 / 学习板块(都需登录)
GET    /api/course/courses                          列课程
GET    /api/course/mine                             我的课程
POST   /api/course/courses                          建课程
GET    /api/course/courses/:id                      课程详情
PATCH  /api/course/courses/:id                      改课程
DELETE /api/course/courses/:id                      删课程
GET    /api/course/courses/:id/progress             课程进度
POST   /api/course/courses/:id/nodes                建章节节点
POST   /api/course/courses/:id/nodes/move           移动节点
GET    /api/course/nodes/:id                        节点详情
PATCH  /api/course/nodes/:id                        改节点
DELETE /api/course/nodes/:id                        删节点
POST   /api/course/nodes/:id/assess                 评测节点
POST   /api/course/nodes/:id/steps/:stepId/assess   评测某步骤
GET    /api/course/nodes/:id/fbd/solution           受力图(FBD)求解
GET    /api/course/nodes/:id/annotations            列批注
POST   /api/course/nodes/:id/annotations            建批注
PATCH  /api/course/annotations/:id                  改批注
DELETE /api/course/annotations/:id                  删批注
POST   /api/course/attachments                      上传媒体
GET    /api/course/attachments/:id                  取媒体

# 班级(都需登录)
GET    /api/classes                                 列我的班级
POST   /api/classes                                 建班级
GET    /api/classes/invite/:token                   预览邀请
POST   /api/classes/invite/:token/accept            接受邀请
GET    /api/classes/:classId                        班级详情
PATCH  /api/classes/:classId                        改班级
DELETE /api/classes/:classId                        删班级
GET    /api/classes/:classId/invite                 取邀请(owner)
POST   /api/classes/:classId/invite/regenerate      重新生成邀请
DELETE /api/classes/:classId/invite                 禁用邀请
POST   /api/classes/:classId/avatar                 上传班级头像
GET    /api/classes/:classId/avatar                 取班级头像
DELETE /api/classes/:classId/avatar                 删班级头像
POST   /api/classes/:classId/leave                  退出班级
GET    /api/classes/:classId/members                列成员
DELETE /api/classes/:classId/members/:userId        移除成员

# 频道(挂在班级下,都需登录)
GET    /api/classes/:classId/channels                           列频道
POST   /api/classes/:classId/channels                           建频道
GET    /api/classes/:classId/channels/:channelId                频道详情
PATCH  /api/classes/:classId/channels/:channelId                改频道
DELETE /api/classes/:classId/channels/:channelId                删频道
GET    /api/channels/:channelId/messages                        列消息
POST   /api/channels/:channelId/messages                        发消息
POST   /api/channels/:channelId/messages/:messageId/fork        fork 消息
PATCH  /api/channels/:channelId/messages/:messageId             改消息
DELETE /api/channels/:channelId/messages/:messageId             删消息
POST   /api/channels/:channelId/messages/:messageId/reactions   切换表情回应
POST   /api/channels/:channelId/attachments                     上传附件
GET    /api/channels/:channelId/attachments/:fileId             取附件

# 作业(都需登录)
GET    /api/assignments/hub                         作业总览
GET    /api/classes/:classId/assignments            班级作业列表
POST   /api/classes/:classId/assignments            建作业
GET    /api/assignments/:assignmentId               作业详情
PATCH  /api/assignments/:assignmentId               改作业
DELETE /api/assignments/:assignmentId               删作业
GET    /api/assignments/:assignmentId/roster        提交看板(教师)
GET    /api/assignments/:assignmentId/submission    我的提交(学生)
PUT    /api/assignments/:assignmentId/submission    保存/提交作答(学生)
POST   /api/classes/:classId/assignment-files       上传题目媒体 / 图片作答
GET    /api/submissions/:submissionId               批阅视图(教师)
PATCH  /api/submissions/:submissionId/grade         批改打分(教师)
GET    /api/assignment/files/:fileId                取附件(owner / 班级教师)

# 实时(WebSocket,需登录)
GET    /api/ws                                      升级 WebSocket,推班级 / 频道实时事件
```

## 关键设计决策(不要再讨论)

### 重复注册(未验证账号)允许,但**不覆盖密码**

`Register` 流程:
- 邮箱不存在 → 正常 Insert + 发邮件
- 已存在 + 已验证 → 1001 拒绝
- 已存在 + 未验证 → 刷新 token + 重发邮件;**密码保持原样**;role 允许切换(UpdateRole + 按新 role 发对应邮件)

**不能允许覆盖密码** —— 否则攻击者重新注册并设置自己密码,真用户点验证邮件后就把账号送出去了。已验证账号不能切 role,要升级走单独端点(暂未实现)。

### Teacher 审批流程

1. teacher register → 写 user (verified=false),**不发邮件给本人**
2. 后端生成 `teacher_approval` token,发邮件到 `ADMIN_EMAILS` 列表所有 admin,正文带 name+email+审批链接
3. 任一 admin 点链接 → `GET /api/auth/approve-teacher?token=xxx` → 标 verified=true,token 失效(`FindAndDelete` 原子)
4. **不签发 cookie**(admin 审批不顺便登被审批人)
5. teacher 后续自己 login

Token kind 三类:`verify` / `reset` / `teacher_approval`,同表 `tokens` + kind 字段区分。`TEACHER_APPROVAL_TTL_HOURS` 默认 168(7 天)。

### Google OAuth(后端 redirect 流)

不走前端 ID-Token,client_secret 不出后端,前端零集成成本。

- `GET /api/auth/google` —— 32B 随机 `state` 写入短期 cookie (`oauth_state`,10 分钟),302 跳 Google。**没有 `?return=` 参数,登录完一律跳 `GOOGLE_DEFAULT_RETURN_URL`** —— 故意不开口子消除 Open Redirect 攻击面
- `GET /api/auth/google/callback` —— 对比 cookie state,换 token,拉 userinfo,处理用户,set session cookie,302 跳 return URL
- 必须 `email_verified=true`;无此 email → 自动创建 `role=student / verified=true`,异步镜像 Google 头像到 OSS;有此 email → 自动 link,补 `google_sub`、补 verified、避免重复拉头像
- OAuth 错误不返 JSON,而是 redirect 回 return URL 带 `?oauth_error=...`,前端读 query 显示

Google Console 必须匹配:`Authorized redirect URIs` 包含 `<BackendBaseURL>/api/auth/google/callback`(开发 `http://localhost:8080/...`,生产 `https://mechhub.oftheloneliness.cn/...`)。

### 改密 / 重置密码后所有 session 失效

`ChangePassword` 和 `ResetPassword` 末尾都调 `sessions.DeleteByUser`,行业标准,**不要去掉**。前端要相应处理"改密后跳登录页"。

### OSS bucket 私有 + stream-through(头像与附件统一)

OSS bucket `mechhub-oss` 私有,直接 URL 一律 403。所有访问过后端:

- **头像** `GET /api/user/avatar/:userID` —— **公共端点**(无 cookie 要求,像 GitHub avatar)。`avatar_url` 带 `?v=<key-suffix>` 做缓存破坏。响应 `Cache-Control: public, max-age=86400`
- **附件** `GET /api/solochat/attachments/:id` —— **需登录**,校验 owner_user_id → stream-through,响应 `Content-Disposition: inline`
- 浏览器**永远不直连 OSS**,所以不需要给 bucket 绑自定义域名 / 证书 / 公共读

DB 里只存对象 key(`users.avatar_key`),URL 由 `BACKEND_BASE_URL` env 拼。`SwapAvatarKey` 用 GORM transaction 原子拿旧 key + 设新 key:先上传新文件 → swap → 删旧文件(best-effort),不会出"DB 指向不存在 OSS 对象"的孤儿状态。

## 部署计划

线上 `https://mechhub.oftheloneliness.cn`,**前后端同域 + 路径分流**:

```
https://mechhub.oftheloneliness.cn/api/*  → Go 后端(内部 :8080)
https://mechhub.oftheloneliness.cn/*       → React 前端 SPA
```

同 Origin → 没有 CORS、没有 SameSite=None 麻烦,一张 LE 证书覆盖全部,session cookie 默认就到位。

### Cookie 配置矩阵

| 场景 | SECURE | SAMESITE | 备注 |
|---|---|---|---|
| 本机开发(localhost:5173 + localhost:8080) | `false` | `lax` | 跨端口 fetch 能带 cookie |
| 生产同域 + path 分流(当前计划) | `true` | `lax` | 推荐路径 |
| 跨子域(`app.x.com` + `api.x.com`) | `true` | `lax` | set cookie 时加 `Domain=.x.com` |
| 完全跨站 | `true` | `none` | None 强制 Secure,需 HTTPS |

`SetSameSite` / `Secure` 已从 `.env` 注入 `SessionConfig`,改 env 重启即可。

### 生产 `.env` 切换

```env
MYSQL_DSN=<生产 mysql DSN>
CORS_ENABLED=false                ← 同域不需要
SESSION_COOKIE_SECURE=true        ← HTTPS 必开
SESSION_COOKIE_SAMESITE=lax
APP_BASE_URL=https://mechhub.oftheloneliness.cn
BACKEND_BASE_URL=https://mechhub.oftheloneliness.cn
GOOGLE_DEFAULT_RETURN_URL=https://mechhub.oftheloneliness.cn
GEMINI_API_KEY=<生产 key>
GEMINI_BASE_URL=                   ← 留空走官方;走中转填代理地址
```

## Docker

- 父目录 `mechhub/docker-compose.yml` 起 2 服务:`go-backend`(:8080)+ `mysql:8`(持久化卷)
- 容器间互访 `mysql:3306`;DSN 走 env `MYSQL_DSN`
- 各服务 `.env` 走 `env_file:` 引用,**不要 commit 真值**
- 本地开发不强制走 Docker,`go run .` 单进程即可(需本机 MySQL + 配好 `MYSQL_DSN` / `GEMINI_API_KEY` / `DOCUMENTAI_*`)
- Document AI 鉴权:`GOOGLE_APPLICATION_CREDENTIALS` 指向 ADC JSON,或容器里挂 service account

## 未结的安全债务(进入下一阶段前确认)

### ⚠️ OSS AccessKey 是否已轮换并降权

之前对话里贴过 OSS AK ID 和 Secret,严格说已泄露。应:
1. 阿里云 RAM → 禁用旧 AK
2. 新建 RAM 用户,只授该 bucket 的 `oss:PutObject` + `oss:DeleteObject`(最小权限)
3. 新 AK 填 `.env`

### ⚠️ Google client_secret 是否已轮换

之前在 `.env.example` 里写过真实 client_secret,可能已 commit。应:
1. `git log --all -p -- .env.example` 查历史
2. Google Cloud Console → Credentials → OAuth client → **Reset Secret**
3. 新 secret 填本机 `.env`(**不是** `.env.example`)
4. 历史里若有,用 `git filter-repo` 重写或接受已泄露(反正已换)

## 还可以做的(没结的常规债)

- **速率限制**(register / forgot-password 防邮件轰炸):未做。MySQL 计数 vs Redis 待定
- **信息泄漏收紧**(把"邮箱已注册"和"未注册"两种响应统一):未做,等接前端再说
- **resend verification email 独立端点**:未做,目前用"重新 register"覆盖
- **单元测试**:目前只有 `internal/db/smoke_test.go` 和 `internal/solochat/stop_test.go`。`internal/user/service_test.go` 起 sqlite 内存库跑 happy-path 是下个合理起点

## 环境

- Windows 10/11,bash 用 Git Bash + MSYS
- 项目路径:`C:\Users\oft\Documents\workspace\mechhub\mechhub-back`
- **没有** docker 在 PATH。临时跑数据访问写一次性 Go 程序到 `/tmp/<x>/main.go` 用 `gorm.Open(mysql.Open(dsn))` 直连即可,跑完删除
- MySQL 在自有服务器(`mechhub:...@tcp(oftheloneliness.cn:3306)/mechhub`),不是本地
- Resend `MAIL_FROM`:`MechHub <no-reply@mechhub.oftheloneliness.cn>`,域名已验证
- OSS:`mechhub-avatar` bucket,`cn-hangzhou`
- Google OAuth:已申请,redirect URIs 见"部署计划"
- 前端:`http://localhost:5173`(Vite),React + react-router + Zustand
