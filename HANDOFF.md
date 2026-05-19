# 交接说明 — 给下一个 AI

> 这份文档假设你刚被拉进来,什么都不知道。读完它你应该能立刻接着干活。

## 这是个什么项目

`mechhub-back` 是一个 Go 后端,现已完成:
- **用户系统**:注册 / 邮箱激活 / role 双轨(student / teacher,后者由 admin 审批) / 登录 / 登出 / 找回密码 / 改密 / 资料 / 头像 / Google OAuth
- **Solochat 模块**:通用 agent 对话,前端可见 thinking + 工具调用 + 工具结果。**轮 7 起 Go 直接接 ADK Go(`google.golang.org/adk` v1.2.0)+ Gemini 2.5 Flash 原生 SDK,Python 服务退场**。批改不再是独立功能,是 agent 自主调用 `grade_with_ocr` 工具的一个特例。OCR / 批改严格两步 SOP,图片数据走内存缓存不走磁盘
- **数据库**:MySQL via GORM(轮 7 起,Mongo 整体退场);ADK Go 的 sessions/events/state 表与我们的 users/tokens/user_sessions/solochat_conversations/uploaded_files 业务表同库不同表
- **Docker 化**:父目录 `mechhub/docker-compose.yml` 起 go-backend + mysql 两服务(`agent` / `mongo` 已退场)

## 必读的两份文档

按顺序看:

1. **`CLAUDE.md`** — 项目宪法。技术栈、目录约定、代码风格(8 条)、配置约定、CORS、数据库、Postman 约束。**这是硬规则,不能违反**。
2. **`postman/README.md`** — Postman Spec Hub 文件夹格式的 schema 与命名规则。新增接口必须同步加 Postman 文件,这是 CLAUDE.md 里写明的硬约束。

读完这两份再读这份交接说明剩余部分。

## 当前已实现

### 已开放的接口(详见 `internal/user/route.go`)

```
POST /api/auth/register           邮箱+密码+name+role 注册;student 发验证邮件,teacher 发审批邮件给 admin
GET  /api/auth/verify             ?token=xxx,激活 student 账号
GET  /api/auth/approve-teacher    ?token=xxx,admin 审批通过后激活 teacher 账号(不签发 session)
POST /api/auth/login              登录,set-cookie session_id,返回 userdata
POST /api/auth/logout
POST /api/auth/forgot-password    发重置邮件
POST /api/auth/reset-password     用 token 设新密码,踢掉所有 session
GET  /api/auth/google             302 跳 Google 授权页(state 写入 short cookie)
GET  /api/auth/google/callback    Google 回调,换 token、拉用户、首登镜像头像到 OSS、set session cookie、302 回 GOOGLE_DEFAULT_RETURN_URL
GET  /api/user/avatar/:userID     **公共**(无需登录),stream-through 拉指定用户头像;`?v=` 缓存破坏
GET  /api/user/me                 (需登录) 返回 id/email/name/role/avatar_url/verified/created_at
POST /api/user/update-profile     (需登录) 改 name,返回最新 userdata
POST /api/user/avatar             (需登录) multipart 上传头像 → 阿里云 OSS → 返回新 URL
POST /api/user/change-password    (需登录) 改密码,踢掉所有 session

Solochat (全部需要登录):
GET    /api/solochat/conversations                          列对话(updated_at desc)
POST   /api/solochat/conversations                          建对话
PATCH  /api/solochat/conversations/:id                      改标题
DELETE /api/solochat/conversations/:id                      删对话 + 所有消息
GET    /api/solochat/conversations/:id/messages             列消息(created_at asc),每条带 parts[] + attachments[]
POST   /api/solochat/conversations/:id/messages/stream      发消息,SSE 流式(10 种事件,见下)
POST   /api/solochat/conversations/:id/messages/stop        取消该对话当前 in-flight stream(同对话再发 stream 也会隐式取消旧的)
POST   /api/solochat/attachments                            multipart 上传附件(image/pdf/text/markdown)
GET    /api/solochat/attachments/:id                        302 跳 OSS 公开 URL
```

### Solochat SSE 事件协议(轮 5 起,POST + SSE 响应体单端点)

`POST /messages/stream`,Content-Type `text/event-stream`,帧格式 `data: {json}\n\n`,
每 25s 一行 `: ping\n\n` 心跳。事件区分走 JSON `type` 字段(不用 SSE `event:` 行),
共 10 种类型。典型时序:

```
user_input → message_start → reasoning_delta* → tool_call_start
  → tool_result → text_delta* → text_complete → message_done
```

| 事件 | 字段 | 用途 |
|---|---|---|
| `user_input` | `message: MessageDTO` (含 attachments[]) | Go 持久化用户消息后立即发,前端乐观 UI 对齐 |
| `message_start` | `message_id, model` | Agent 回合开始 |
| `reasoning_delta` | `message_id, delta` | 思考过程流(可折叠展示) |
| `text_delta` | `message_id, delta` | 文本块流式追加 |
| `text_complete` | `message_id, text` | 当前文本块结束(完整最终文本) |
| `tool_call_start` | `message_id, tool_use_id, name, input` | LLM 调用工具,完整入参 |
| `tool_result` | `message_id, tool_use_id, output, is_error, elapsed_ms` | 工具返回(包括完整 GradingOutput) |
| `conversation_title` | `conversation: ConversationDTO` | 首条消息后自动命名 |
| `error` | `message_id, code, error` | 异常 |
| `message_done` | `message_id, finish_reason` | Agent 回合结束 |

Message 形态为 `Parts []MessagePart`,part type 可选:`text` / `thinking` / `tool_use` / `tool_result`。`GET /messages` 返回这些 parts,前端按数组顺序逐块渲染。

**轮 7 起,消息源走 ADK Go**:`GET /messages` 由 Go 直接查 `internal/llm.Service.ListMessages`(读 ADK session 的 events + state,按 invocation_id 分组翻译成 `MessageDTO[]`),不再走 HTTP 到 Python。Go 仍负责按 user message 的 file_ids hydrate 出附件 URL / MIME 元信息。`StreamUserInput.message.id` 是 `pending-user-<random>` 临时占位(流跑完前端可重新 GET 拿 canonical 的 ADK event UUID)。

### 关键技术决策

| 决策 | 实现 |
|---|---|
| 认证 | Session ID + HttpOnly Cookie,session 存 MySQL(表 `user_sessions`),后台 goroutine 每 60s 清过期(`db.StartTTLCleanup`)。**不用 JWT** |
| 数据库 | MySQL via GORM(`gorm.io/driver/mysql`),UUID v4 字符串作 ID(`char(36)`)。`AutoMigrate` 启动期建表 |
| LLM | ADK Go (`google.golang.org/adk` v1.2.0) + Gemini 2.5 Flash 原生 SDK;sessions/events/state 由 ADK Go 自动在同库建表 |
| 邮件 | Resend,域名 `mechhub.oftheloneliness.cn` 已验证 |
| 头像存储 | 阿里云 OSS,后端代理上传(不是客户端直传)。bucket = `mechhub-avatar`,region = `cn-hangzhou`,公共读 |
| CORS | 自己写的 ~30 行 middleware,通过 `CORS_ENABLED` / `CORS_ORIGINS` 控制 |
| 密码 | bcrypt cost=12 |
| Token | crypto/rand 32B → base64url |

## 重要的设计选择和坑(以后碰到别再讨论)

### 1. 重复注册(未验证账号)允许,但**不覆盖密码**

`Register` 流程:
- 邮箱不存在 → 正常 Insert + 发邮件
- 邮箱已存在 + 已验证 → 1001 拒绝
- 邮箱已存在 + 未验证 → 刷新 token + 重发邮件;**密码保持原样**;**role 允许切换**(此时会 UpdateRole 并按新 role 发对应邮件)

这是为了 cover 邮件失败 / 用户忘记是否注册过 / 注册时填错 role 的场景。**不能允许覆盖密码**——会被攻击者用来劫持账号(攻击者重新注册并设置自己的密码,等真用户点验证邮件后就拿到了带攻击者密码的已验证账号)。如果有人提议"为什么 register 不允许改密码",直接拒绝,这是有意为之。

**已验证账号不能切换 role**——也就是说,student 验证之后不能通过重新注册升级 teacher。要支持升级要单独加个 `/user/request-teacher-role` 端点,目前没需求。

### 2. 角色系统 + 老师审批流程

每个用户有 `role`,值是 `student` 或 `teacher`,**注册时必填**(由前端选)。

**student 注册流程**:跟普通邮箱验证一样——register → 邮箱收到 verify 链接 → 点 → 激活。

**teacher 注册流程**:
1. teacher 调 register,后端写 user (verified=false) 但**不发邮件给 teacher 本人**
2. 后端生成 `teacher_approval` kind 的 token,**发邮件到 `ADMIN_EMAILS` 列表里所有 admin**,邮件正文带 teacher 姓名 + 邮箱 + 审批链接
3. 任一 admin 点链接 → `GET /api/auth/approve-teacher?token=xxx` → 标记 verified=true,token 失效
4. **不签发 cookie**(admin 审批不应该顺便登录被审批人)
5. teacher 自己后续走 login 登录

**关键配置**:
- `ADMIN_EMAILS`:逗号分隔,例如 `admin1@x.com,admin2@x.com`,**只要一个 admin 点了链接就生效**(token 用了 `FindAndDelete`,原子)
- `TEACHER_APPROVAL_TTL_HOURS`:默认 168(7 天)。比 `VERIFY_TOKEN_TTL_HOURS` 长得多,因为 admin 可能没及时看邮件

**HTML 转义**:`SendTeacherApprovalEmail` 用 `html.EscapeString` 转义 name 和 email。**必须保留**——name 是用户输入,不转义会 XSS(admin 点开邮件就中招)。

**token kind 分三类**:`verify`(student 验证)/`reset`(找回密码)/`teacher_approval`(老师审批)。都存同一个 `tokens` 集合,用 kind 字段区分。

### 3. Google OAuth(后端重定向流)

**为什么不走前端 ID-Token 流**:用户选定的方案是后端 redirect。client_secret 不出后端,前端零集成成本。

**两条端点**:
- `GET /api/auth/google` — 生成 32B 随机 `state`,写入短期 cookie(`oauth_state`,10 分钟),302 跳 Google 授权页。**没有 `?return=` 参数,登录完一律跳 `GOOGLE_DEFAULT_RETURN_URL`**——故意不开"用户传任意 URL 跳转"的口子,从设计上消除 Open Redirect 攻击面
- `GET /api/auth/google/callback?code=&state=` — 对比 cookie 里的 state,换 token,调 `userinfo` 拿 sub/email/name/picture,处理用户,设 session cookie,302 跳 `GOOGLE_DEFAULT_RETURN_URL`

**用户处理逻辑**(`service.GoogleSignIn`):
- 必须 `email_verified=true`,否则 `ErrGoogleUnverified` 拒绝
- DB 没这个 email → **自动创建**:`role=student`,`verified=true`(Google 已经验证邮箱),写 `google_sub`,头像异步从 Google 拉一份存 OSS
- DB 有这个 email → **自动 link**:补 `google_sub`(如果之前是空);如果 `verified=false` 顺手补成 true;头像只在 `avatar_key==""` 时才拉
- 任何场景最后都调 `sessions.New` 发 cookie

**state CSRF 防护**:用 HttpOnly cookie `oauth_state` 而不是 DB TTL 记录,简单且免一次查库。callback 第一时间清空。

**Open Redirect 防护**:**通过"不接受用户输入"消除攻击面**。`/auth/google` 不接 query 参数,登录后一律跳 `GOOGLE_DEFAULT_RETURN_URL`。如果未来要支持 deep link("登录前点的链接登录完跳回去")再加白名单参数。不要轻易开 `?return=` 的口子。

**头像镜像**:`mirrorGoogleAvatar` 用 `http.Client` GET picture URL → 检测 content-type → 走 OSS 上传同一个 `avatars/<uid>/<rand>.<ext>` 命名规则。**失败时 swallow error,不阻断登录**——头像没了用户可以登录后自己改。

**Google 头像 URL 是公开的**(`lh3.googleusercontent.com/...`),GET 不需要鉴权。

**错误处理**:OAuth 失败时不返回 4xx/5xx JSON,而是 redirect 回 return URL,带 `?oauth_error=...` 参数,前端读 query 显示错误提示。这是 OAuth 流程的常规做法。

**Google Console 必须匹配的设置**:
- "Authorized redirect URIs" 必须包含 `<BackendBaseURL>/api/auth/google/callback`(由 `BACKEND_BASE_URL` env 拼接;开发默认 `http://localhost:<PORT>`,生产填域名)
- scopes 默认 `openid email profile`,后端代码里硬编码

### 4. Solochat:通用 agent chat(轮 4 重构,轮 7 收敛为单 Go 服务)

**职责划分**:
- **Go 后端 = 一切**:权限、会话元数据、附件 OSS 编排、LLM 推理、工具调用、消息持久化。**没有 Python 服务**(轮 7 起 `mechhub-agent` 仓库归档)
- LLM 通过 ADK Go (`google.golang.org/adk` v1.2.0) + Gemini 2.5 Flash 原生 SDK 跑;持久化通过 ADK Go 的 `session/database.NewSessionService(mysql.Open(dsn))`
- ADK Go 在 MySQL 同库自动建 `sessions` / `events` / `app_states` / `user_states` 四张表;我们的 cookie session 表改名 `user_sessions` 避开撞名

**关键代码位置**:
- `internal/llm/runner.go::Bootstrap`:Gemini 模型 + LlmAgent + Runner + database session service,启动期建一次
- `internal/llm/sse.go::StreamChat`:跑一轮 agent,迭代 `iter.Seq2[*session.Event, error]` 翻译成 SSE 帧;流末用 `appendAttachmentBinding` 写 `_solochat_attachments_<invocation_id>` 到 session.state
- `internal/llm/sessions.go::ListMessages`:`session.Service.Get` 读 events,按 invocation_id 分组翻译成 MessageDTO
- `internal/llm/tools/ocr.go`:Document AI Go 客户端;cache key 按图片索引;ProcessImages 直接读内存 `[]CachedImage`;state 里走 `ocr_cache`
- `internal/llm/tools/image_cache.go`:内存图片缓存(key=sessionID),LLM 用简短索引引用图片
- `internal/llm/tools/grader.go`:Gemini structured output (`ResponseSchema = schemas.Schema()`);先读 OCR 缓存再批改;缓存 miss 返回 error 让 LLM 补 OCR
- `internal/llm/prompts/prompts.go`:`RootSystemPrompt` + `BuildGradingPrompt`
- `internal/llm/schemas/grading.go`:GradingOutput Go struct + `*genai.Schema`
- `internal/solochat/service.go::SendMessageStream`:校验 + 下载附件 + `s.llm.StreamChat(...)` + SSE 透给前端;ListMessages 调 `s.llm.ListMessages(...)` 然后 hydrate 附件 URL
- `cmd/adkpoc/main.go`:Stage 1 PoC,`go run ./cmd/adkpoc` 单独跑一次 ADK Go + sqlite 验证

**事件协议**:8 种 SSE 帧(`message_start` / `reasoning_delta` / `text_delta` / `text_complete` / `tool_call_start` / `tool_result` / `error` / `message_done`)+ 2 种 Go-only(`user_input` / `conversation_title`)。所有事件 type 走 JSON `type` 字段。前端解析:按 `\n\n` 切帧 + 去 `data: ` 前缀 + `JSON.parse`。

**附件流转**:前端 → multipart 上传到 Go(MIME 白名单 image/PDF/text/markdown)→ Go 写 OSS → DB 存 key → 发消息时 Go `buildUserContent` 读 OSS 字节 → 图片 / PDF 包成 `*genai.Part{InlineData: ...}` 直接给 ADK,LLM 能"看见";text/markdown 内容 inline 拼到 prompt 文本。图片数据同时存内存缓存(`tools.StoreSessionImages`),工具(OCR / grading)通过简短索引(如 `[0,1,2]`)引用,无磁盘 I/O,LLM 不再接触文件路径。

**批改不是独立功能**:LLM 自主决定何时调 `grade_with_ocr(image_indices)` 工具。工具内部按需复用 OCR 缓存,调 Gemini structured-output 拿 GradingOutput,返回结构化 JSON。前端从对应 `tool_result.output` 渲染分数 + 评语 + 步骤分析。`/grading-tasks/*` 端点轮 4 已下线。

**Session 持久化**:ADK Go 用 GORM 写 sessions/events/state 三张表;进程重启历史完整保留,OCR 缓存跨重启复用。附件绑定通过 `_solochat_attachments_<invocation_id>` state key,读时按 key 反查 file_ids。

**为什么换掉 Python**:轮 4-6 期间用 Python 是因为 ADK 当时只有 Python SDK;ADK Go 2026-04 GA 之后,微服务拆分带来的复杂度不再值得。代价:LiteLLM 没有 Go 等价物,接非 Google 模型(qwen / Claude)需要自己实现 `model.LLM` 接口 200-400 行 —— 但本期锁 Gemini 不踩这个坑。

**ADK Go 自动生成的 invocation_id 是 `e-<uuid>`**(它内部覆盖外部传入的值)—— `internal/llm/sse.go::StreamChat` 在流过程中捕获实际 invocation_id,流末才写 state_delta。不要尝试在调用前就生成 invocation_id 并相信它。

### 5. 改密 / 重置密码后所有 session 失效

`ChangePassword` 和 `ResetPassword` 末尾都调 `sessions.DeleteByUser`。这是行业标准,不要去掉。前端要相应处理"改密后跳登录页"。

### 6. 头像存 `avatar_key` 不存 URL

DB 里只存对象 key(`users.avatar_key`,如 `avatars/<uid>/<rand>.png`)。**OSS bucket 私有,无公共 URL**——`avatar_url` 由后端拼 `<BackendBaseURL>/api/user/avatar/<user_id>?v=<key-suffix>`,浏览器拉这个端点,Go 后端做 stream-through 从 OSS 取字节流转发(`internal/user/service.go::OpenAvatar`)。`?v=` 让 key 变化时 URL 跟着变,浏览器缓存自动失效。

### 7. SwapAvatarKey 是原子的

`internal/user/repo.go::SwapAvatarKey` 用 GORM transaction(`tx.Select("avatar_key").First` → `Update("avatar_key", newKey)`)一次性拿旧 key + 设新 key。流程是:**先上传新文件成功 → swap → 删旧文件(best-effort)**,任何一步失败都不会出现"DB 指向不存在 OSS 对象"的孤儿状态。

### 8. ForgotPassword 邮箱不存在也返回成功

防枚举。不要改。

### 9. Resend / Gmail 折叠对话的坑

用户之前以为"每次注册收到的 token 都一样" —— 实际是 Gmail 把同主题邮件折叠成 thread,默认显示最早一封。**后端确实每次生成不同 token**,这点已验证。如果用户再提这个问题,引导他看 Resend 控制台 Logs 或换非 Gmail 邮箱测试。

### 10. OSS bucket 私有 + stream-through(头像与附件统一)

OSS bucket(`mechhub-oss`)**私有**,任何直接 URL 都 403。所有访问都过后端:

- **头像** `GET /api/user/avatar/:userID`:**公共端点**(无 cookie 要求,像 GitHub avatar)。后端按 user_id 查 `avatar_key` → `s.oss.Download(key)` → `c.DataFromReader(...)` 流回。响应 `Content-Type: image/*` + `Cache-Control: public, max-age=86400`。`avatar_url` 带 `?v=<key-suffix>` 做缓存破坏
- **附件** `GET /api/solochat/attachments/:id`:**需登录**。校验 owner_user_id → `OpenAttachment` 返回 `(*UploadedFile, io.ReadCloser)` → stream-through,响应 `Content-Disposition: inline`
- 浏览器**永远不直连 OSS**,所以不需要给 bucket 绑自定义域名 / 上证书 / 开公共读

`BACKEND_BASE_URL` env 控制 `avatar_url` / `attachment.url` / Google callback 的域名部分。默认 `http://localhost:<PORT>`,开发无需设置;生产填 `https://mechhub.oftheloneliness.cn`。

## 部署计划(用户已经决定的)

线上目标:`https://mechhub.oftheloneliness.cn`,**前后端同域 + 路径分流**。意味着会有一层反向代理(nginx / Caddy)按路径分流:

```
https://mechhub.oftheloneliness.cn/api/*  → 后端(Go 服务,内部 :8080)
https://mechhub.oftheloneliness.cn/*       → 前端(React 静态文件 / SPA)
```

**这种部署最舒服**:
- 同 Origin / 同 Site → 没有 CORS、没有 SameSite=None 的麻烦
- 一张 Let's Encrypt 证书覆盖全部
- session cookie 默认就到位

**Google Cloud Console 已登记的 Authorized redirect URIs**(两套环境都要,Google 严格匹配):
- `https://mechhub.oftheloneliness.cn/api/auth/google/callback`(生产)
- `http://localhost:8080/api/auth/google/callback`(开发)

**生产 `.env` 切换时改这些(本机开发 `.env` 不动)**:

```env
MYSQL_DSN=<生产 mysql DSN>
CORS_ENABLED=false                ← 同域不需要 CORS
SESSION_COOKIE_SECURE=true        ← HTTPS 必开
SESSION_COOKIE_SAMESITE=lax       ← 同域,lax 够用
APP_BASE_URL=https://mechhub.oftheloneliness.cn
BACKEND_BASE_URL=https://mechhub.oftheloneliness.cn   ← 生产必设
GOOGLE_DEFAULT_RETURN_URL=https://mechhub.oftheloneliness.cn
GEMINI_API_KEY=<生产 key>
GEMINI_BASE_URL=                   ← 留空走官方;走中转填代理地址
```

代码层面**不需要改任何东西**。如果用户后来改成跨子域 / 跨站部署,SameSite + Secure 那张表还在对话历史里,自己查。

## Cookie 配置参考矩阵

四种部署场景该怎么配:

| 场景 | SECURE | SAMESITE | 备注 |
|---|---|---|---|
| 本机开发(http://localhost:5173 + http://localhost:8080) | `false` | `lax` | localhost 同 site,跨端口 fetch 能带 cookie |
| 生产同域 + path 分流(当前计划) | `true` | `lax` | 推荐路径 |
| 跨子域(`app.x.com` + `api.x.com`) | `true` | `lax` | 同 site,但需要 set cookie 时加 `Domain=.x.com`(代码里 `SetCookie` 第 5 参数) |
| 完全跨站(`mechhub-app.com` + `api.different.com`) | `true` | `none` | None 强制 Secure,IP 也不行,需要 HTTPS |

`SetSameSite` / `Secure` 已经从 `.env` 注入到 `SessionConfig`,改 env 重启即可,代码不动。

## 未结的安全债务(用户需要确认)

进入下一阶段前**必须确认这两件已经做完**,否则可能已经泄露:

### 1. ⚠️ OSS AccessKey 是否已轮换并降权

之前的对话里,用户曾在 `.env` 里贴出过 OSS AccessKey ID 和 Secret(`LTAI5t5mh2BkeK5wnAUBk5RP` 等)。即使他后来改过 `.env`,这个 secret 已出现在 AI 对话记录里,严格说算泄露。

应做:
1. 阿里云 RAM → 禁用旧 AK
2. 新建 RAM 用户,只授**该 bucket 的 `oss:PutObject` + `oss:DeleteObject`** 权限(最小权限)
3. 新 AK 填 `.env`

**先问用户做了没**,没做就立刻做。

### 2. ⚠️ Google client_secret 是否已轮换

用户曾在 `.env.example` 里写过真实 Google client_secret(`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`)。`.env.example` 是会进 git 的文件,如果他 commit + push 过就永久暴露在 git 历史。

应做:
1. **先检查 git 历史**:`git log --all -p -- .env.example` 看有没有把真值 commit 过
2. 进 Google Cloud Console → Credentials → OAuth client → **Reset Secret**
3. 新 secret 填本机 `.env`(**不是** `.env.example`),`.env.example` 必须是占位符
4. 如果历史里已经有,要么用 `git filter-repo` 重写历史,要么接受"已泄露",反正 secret 已换

**先问用户做了没**。

---

## 还没做、用户已经知道、可以视情况继续做的

- **速率限制**(register / forgot-password 防邮件轰炸):未做。设计选择没定(MySQL 计数 vs Redis)。
- **信息泄漏收紧**(把"邮箱已注册"和"未注册"两种响应统一):未做。等接前端、看产品形态再说。
- **resend verification email 独立端点**:未做。当前用"重新 register"已经能 cover。
- **单元测试**:目前只有 `internal/db/smoke_test.go`(GORM 五表 roundtrip,sqlite 内存)和 `internal/solochat/stop_test.go`(stop 并发原语)。`internal/user/service_test.go` 起 sqlite 内存库跑 happy-path 是下一个合理起点。
- **多模块**:目前只有 `internal/user/`。新加业务模块按 CLAUDE.md 复制五件套。

## 用户非常在意的几件事

读到这里你应该已经看过 CLAUDE.md 了,但有几条用户反复强调过,值得再点一下:

1. **少兜底,多信任内部边界** — service / repo 不做 nil 检查,validator 在 handler 入口校验一次就够了。**不要写防御性代码**。
2. **不预先抽象** — service 和 repo 不写 interface,直接 struct。需要 mock 时再抽。OSS 客户端也是 struct,不是接口。
3. **不写注释** — 默认不写,只有"为什么这样写"非显而易见时写一行。**禁止**写 `// register 处理用户注册` 这种重复函数名的废话注释。
4. **路由可见性** — 看 `internal/<feature>/route.go` 必须一眼看到该模块所有路径。`internal/router/router.go` 只 mount,不写路径。
5. **每加一个 HTTP 接口,必须同步加 Postman 文件** — 见 `postman/README.md`。不允许只加 handler 不加 Postman。
6. **依赖注入,无全局变量** — 所有依赖在 `main.go` 装配,`New(...)` 注入。

## 用户的工作风格

- 中文交流(简体)。
- 喜欢直接答案 + 简洁说明,不要长篇大论铺垫。
- 看到差不多的设计/方案会自己微调(linter 修过几次 Postman 文件,这是正常的)。
- 重要决策会主动用 AskUserQuestion 让你给选项,他直接选。**不要替他做大决策**(比如要不要换数据库、要不要加 Redis、安全 trade-off 这种)。
- 对安全风险敏感。我之前提了"重新注册可能被劫持密码"的攻击,他立刻让我修。**遇到安全/隐私 trade-off,主动告诉他**,不要默默选默认。
- 对已经讨论过的话题不喜欢重复(已讨论的会在本文档"重要设计选择"那段列出)。

## 一些环境相关的事情

- 操作系统:Windows 10/11,bash 环境是 Git Bash + MSYS。
- 项目路径:`C:\Users\oft\Documents\workspace\mechhub\mechhub-back`
- 用户**没有** docker 在 PATH。如果需要临时跑数据访问代码(清表 / 查状态),写一次性 Go 程序到 `/tmp/<x>/main.go` 用 `gorm.Open(mysql.Open(dsn))` 直连即可,跑完删除。
- 用户的 MySQL 在自己服务器上(`mechhub:...@tcp(oftheloneliness.cn:3306)/mechhub`),不是本地。期间 Mongo→MySQL 完整迁移由 Opus 在轮 7 完成,不再走 Mongo。
- Resend 的 `MAIL_FROM` 用 `MechHub <no-reply@mechhub.oftheloneliness.cn>`,域名已经在 Resend 验证过。
- OSS:`mechhub-avatar` bucket,`cn-hangzhou`。AccessKey 见上文"未结的安全债务"。
- Google OAuth Client:已在 Cloud Console 申请,registered redirect URIs 见"部署计划"段。client_secret 状态见"未结的安全债务"。
- 前端:用户在 `http://localhost:5173`(Vite 默认端口)开发,React + react-router。具体技术栈用户还没说,提到过 Zustand 是合理选项但他没确认是否用。

## 项目当前状态

- `go build ./...` 通过。
- 用户已实测跑通用户系统完整流程(注册 → 验证 → 登录 → me → 修改 name → 上传头像 → 改密码 → 找回密码)。
- 轮 4 重构(通用 agent chat)**未实测**,**编码前的三个验证项还没做**:
  1. qwen 通过 LiteLlm + `extra_body={"enable_thinking": True}` 是否真能把 thought parts 透出
  2. ADK callback 在 async 上下文还是工作线程跑(影响 `put_nowait` 是否安全)
  3. ADK `function_call` 事件粒度(协议留了 `tool_call_delta` 占位但当前不发)
- 启动新版后端时必须先把 `SOLOCHAT_MIGRATE_DROP_GRADING=true` 设一次,drop 掉旧的三张 grading 表,然后改回 false。

接下来等用户提新需求。Good luck.
