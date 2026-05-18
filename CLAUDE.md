# MechHub Backend — 项目规约

> 所有 AI 协作与人工 PR 必须遵守。这是项目宪法,优先级高于一般直觉。

## 技术栈

- 语言:Go (`go.mod` 已定基线版本)
- Web 框架:`gin-gonic/gin`
- 数据库:MySQL (`gorm.io/driver/mysql` + `gorm.io/gorm`,GORM AutoMigrate 启动期建表)
- 配置:`.env` + `joho/godotenv`,`internal/config` 集中加载
- 邮件:`resend-go/v3`
- 对象存储:阿里云 OSS (`aliyun/aliyun-oss-go-sdk`)
- OAuth:`golang.org/x/oauth2`(Google)
- 密码:`golang.org/x/crypto/bcrypt`
- 认证:Session ID + Cookie(服务端 session 存 MySQL `user_sessions` 表,带 TTL 后台清理 goroutine),**不用 JWT**
- AI agent:**Go 直接接 ADK Go**(`google.golang.org/adk` v1.2.0)+ Gemini 2.5 Flash 原生 SDK(`google.golang.org/genai`)+ Document AI Go 客户端。`internal/llm/` 是 LLM 边界,**Go 内部直接跑 agent / tools / session,不再有 Python 服务**
- 部署:Docker Compose(父目录 `mechhub/docker-compose.yml`,**两服务** go-backend + mysql。`agent` / `mongo` 已退场)

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
    ├── storage/                  # OSS 客户端封装(Upload / Download / PublicURL)
    ├── oauth/                    # Google OAuth 客户端
    ├── llm/                      # ADK Go 封装(Gemini + tools + SSE 翻译)
    │   ├── runner.go             # Bootstrap:agent + runner + session DB service
    │   ├── sse.go                # StreamChat:event 流转 SSE 帧
    │   ├── sessions.go           # ListMessages:读 ADK session events 翻译成 MessageDTO
    │   ├── prompts/              # 系统提示词 + grading 提示词
    │   ├── schemas/              # GradingOutput Go struct + genai.Schema
    │   └── tools/                # ocr.go (Document AI) + grader.go (Gemini structured output)
    ├── response/                 # 统一响应 + 错误码常量
    ├── router/                   # 装配路由,不写具体路径
    └── <feature>/                # 功能模块,自带 route.go
        ├── route.go              # Mount(g, h, ...) 一眼可见全部路由
        ├── handler.go            # 参数解析 + 调 service + 返回
        ├── service.go            # 业务逻辑
        ├── repo.go               # 数据访问 (GORM)
        ├── model.go              # struct + DTO
        └── *.go                  # 其它本模块文件
```

当前 feature module:`user/`、`solochat/`(后者还有 `streamer.go` 处理 SSE 写出)。

新增功能模块 = 复制这套五件套 + `router.go` 加一行 mount。

## 代码风格(必读)

1. **少兜底,多信任内部边界**
   - `validator` 在 handler 入口校验请求体一次,service / repo 信任传入参数,不再做 nil/空值检查。
   - 不为"理论上不会发生"的情况写防御代码。
   - 错误处理就是 `if err != nil { return err }` 一行,不要包装空上下文。

2. **错误统一在 handler 处理**
   - service / repo 直接 `return err`,handler 用 `response.OK / response.Fail` 返回,不要在每个 handler 里手写 `c.JSON(...)`。
   - 业务错误用 `response/response.go` 里的常量错误码。

3. **不预先抽象**
   - service 和 repo 用 struct + 方法,**不预先定义 interface**,需要 mock 时再抽。
   - 不为单实现写 factory / builder。
   - 三段相似代码不抽函数,五段以上再考虑。

4. **依赖注入,无全局变量**
   - `*gorm.DB`、配置、mail sender、`*llm.Service` 在 `main.go` 初始化,`New(...)` 注入到各模块。
   - 禁止 `var DB *gorm.DB` 这种 package-level 全局。

5. **路由可见性**
   - 每个模块自带 `route.go`,所有路径 + handler 一眼可见。
   - `internal/router/router.go` 只 mount,不写具体路径。

6. **注释规则**
   - 默认不写注释。只在"为什么这样写"非显而易见时写一行。
   - 不写 `// register 处理用户注册` 这种重复函数名的注释。
   - 不写 PR 上下文相关的注释(`// added for issue #123`)。

7. **命名**
   - URL 路径 kebab-case:`/api/auth/forgot-password`
   - Go 导出 PascalCase,私有 camelCase
   - 文件名 lowercase,多词用下划线:`reset_token.go`

8. **响应格式**
   ```json
   { "code": 0, "msg": "ok", "data": { ... } }
   ```
   `code = 0` 成功,非零业务错误码,HTTP status 同步使用。

## 配置约定

- 所有配置走 `.env`,绝不硬编码。
- 新增配置必须同时更新 `.env.example` 和 `internal/config/config.go`。
- 必填项在启动时 panic,可选项给合理默认值。

## CORS

- 通过 `CORS_ENABLED=true|false` 控制是否注册中间件。
- `CORS_ORIGINS` 逗号分隔多个 origin,精确匹配,带 cookie 必须 `Allow-Credentials: true`。
- 不引第三方 CORS 包,自己写一个简短 middleware。

## 更改日志

- 每一轮 AI 协作 / 重要 PR 完成后,**必须在 `CHANGELOG.md` 顶部加一条**,按"破坏性 / 功能 / 修复 / 杂项"分组列出改动。
- 破坏性变更(API 形状改、env 必填项加、链接路径改等)**必须标 ⚠️**,并说明前端 / 调用方该怎么跟着改。
- 不允许"静默"修改 API 响应结构、邮件路径、env 必填项——这些都属于跨边界契约,改必写。
- 不需要 commit 一条改一次 CHANGELOG,**一轮工作合并写一条**即可,标题用 `## <轮次> — <日期> — <主要内容>`。

## 数据库

- MySQL via GORM,启动时 `gormDB.AutoMigrate(...)` 自动建表 + 索引(gorm tag 上声明)
- TTL 清理:不依赖数据库自带 TTL(MySQL 没有),用 `internal/db.StartTTLCleanup` 后台 goroutine 每 60s `DELETE WHERE expires_at < NOW()` 清 `tokens` + `user_sessions`
- 主键:**UUID v4 字符串(char(36))**。**不要**继续用 `bson.ObjectID`,Mongo 已退场
- 表命名:
  - **用户系统**:`users` / `tokens` / `user_sessions`(注意 cookie session 表叫 `user_sessions`,避开 ADK Go 在同库自动建的 `sessions`)
  - **Solochat**:加 `solochat_` 前缀(目前只有 `solochat_conversations`,消息源走 ADK)
  - **通用**:`uploaded_files`(头像 + solochat 附件共用)
- 加新模块时新建的表应该加同样的模块名前缀,便于看名字就知道归属

### 消息源:ADK Go 的 sessions/events 表(轮 7 起)

- 对话消息(events + state + OCR 缓存 + 附件绑定)由 ADK Go `session/database.NewSessionService(mysql.Open(dsn))` 持久化到 MySQL 同库的 `sessions` / `events` / `app_states` / `user_states` 表(由 ADK Go 自动 migrate)
- 我们的业务表只有:`users` / `tokens` / `user_sessions`(cookie session,注意改名避开 ADK 的 `sessions`)/ `solochat_conversations` / `uploaded_files`
- `GET /messages` 直接读 ADK session 然后翻译成 MessageDTO,见 `internal/llm/sessions.go`
- 附件 ↔ 用户消息绑定:流末向 session.state 写 `_solochat_attachments_<invocation_id>` = file_ids;读时按同 key 反查
- 无 Python 服务,无 Mongo,单进程单 DB

## 用户可见文案

- 所有用户可见的字符串(`response.OK`/`response.Fail` 的 `msg`、邮件正文 / 标题 / 按钮、错误提示)**必须中文**。不要用 `"ok"` / `"unauthorized"` / `"email already registered"` 这种英文。
- 后端内部 `errors.New(...)` / 日志可以英文,但**只要走到 HTTP 响应或邮件就转中文**。

## API 响应形状

- 凡是返回"当前用户信息"的端点(`/auth/login` / `/user/me` / `/user/update-profile` 等),`data` 字段**直接是 `MeResp`**,不要包 `{ message, userdata: {...} }` 这种 wrapper。
- 前端约定:`response.data` 拿到用户对象就用,不用再深一层 `response.data.userdata`。
- 操作类端点(`/auth/logout` / `/auth/forgot-password` / `/auth/reset-password` / `/user/change-password`)`data` 可以是 `{ message: "..." }` 形式。
- 加新端点时遵循:**返回数据对象** vs **返回操作结果消息** 选其一,不要混。

## 邮件模板

- 所有发出去的邮件**必须经过 `internal/mail/resend.go::cardLayout`**,不要自己拼 HTML 字符串。
- 加新邮件类型 = 加一个 `Send<Xxx>Email` 方法,内部构造 `cardArgs` 调 `cardLayout`,统一品牌外观。
- 邮件里嵌入用户输入的字段(name / email / 自定义内容)**必须**用 `html.EscapeString` 转义,防 XSS。
- 邮件正文中文。
- 邮件里指向前端的链接,路径用统一前缀 `<APP_BASE_URL>/verify/<type>?token=...`:
  - `student` — 学生邮箱验证
  - `teacher` — 教师审批
  - `reset-password` — 重置密码
  - 后端的 API 端点路径(`/api/auth/verify` 等)**不变**,只是邮件里前端落地页路径要按这个约定

## Postman

- 项目用 Postman **Spec Hub 文件夹格式**(YAML 拆文件,VCS 友好),目录 `postman/`,详细 schema 与命名规则见 [`postman/README.md`](postman/README.md)。
- **每新增一个 HTTP 接口,必须同步在 `postman/collections/MechHub Backend/<group>/<slug>/<Title>.request.yaml` 加一条**;不允许只加 Go handler 不加 Postman 文件。
- 新增/修改的占位变量同步进集合根 `.resources/definition.yaml` 的 `variables` 段以及 `environments/local.environment.yaml`。
- 不要使用单文件 `*.postman_collection.json` 格式。

## 流式接口

- **SSE**(`text/event-stream`):**所有**流式接口统一用 SSE,与业界主流(OpenAI / Anthropic / Vercel AI SDK)对齐。帧格式 `data: <json>\n\n`,**不用** `event:` 行,事件类型走 JSON 内的 `type` 字段。每 25s 一行 `: ping\n\n` 心跳,防代理超时。
- POST + SSE 响应体单端点(`/api/solochat/conversations/:id/messages/stream`)是当前的发消息模式。前端用 `fetch()` 读 response body stream,按 `\n\n` 切帧,去掉 `data: ` 前缀再 `JSON.parse`。**不使用浏览器原生 `EventSource`**(它只支持 GET 且不能带 body)。
- Go 端 helper:`internal/solochat/streamer.go::newSSE` + `c.Writer.Write` + `http.Flusher.Flush`。
- 必须 set `X-Accel-Buffering: no` 头,防止 nginx / Cloudflare 缓冲整个响应。

## LLM 对接(`internal/llm/`)

- LLM 实现走 ADK Go(`google.golang.org/adk` v1.2.0)+ Gemini 2.5 Flash 原生 SDK。**Go 自己跑 agent / tools / session**,没有 Python 服务,没有 LiteLLM 适配层
- 边界包 `internal/llm/`:
  - `runner.go::Bootstrap` —— 启动期建一次,绑定 Gemini 模型 + LlmAgent + tools + database session service
  - `sse.go::StreamChat` —— 单次 chat 转 8 种 SSE 帧(`message_start` / `reasoning_delta` / `text_delta` / `text_complete` / `tool_call_start` / `tool_result` / `error` / `message_done`)
  - `sessions.go::ListMessages` —— 读 ADK session,events 按 `invocation_id` 分组翻译给前端
  - `tools/{ocr,grader}.go` —— OCR / 批改工具,LLM 自主决定何时调
  - `prompts/` + `schemas/` —— 系统提示词与 grading JSON schema
- 切其它 LLM(qwen / Claude / OpenAI)需实现 `model.LLM` 接口(`Name()` + `GenerateContent(ctx, req, stream) iter.Seq2[*LLMResponse, error]`),~200-400 行
- Solochat 模块**只通过 `internal/llm.Service` 调 LLM**,不应该自己 import `google.golang.org/genai` 或 `google.golang.org/adk` —— 把 LLM 复杂度关在 `internal/llm/` 里

## Docker

- 父目录 `mechhub/docker-compose.yml` 起 2 个服务:`go-backend`(暴露 8080)+ `mysql:8`(持久化卷)。轮 7 起 `agent` 和 `mongo` 都退场了
- 容器间互访:`mysql:3306`;DSN 走 env `MYSQL_DSN`
- 各服务 `.env` 走 `env_file:` 引用,**不要 commit 真值**
- 本地开发不强制走 Docker,`go run .` 单进程即可;只要本机起一个 MySQL(brew / docker) + 配好 `MYSQL_DSN` / `GEMINI_API_KEY` / `DOCUMENTAI_*` 就行
- Document AI 鉴权:`GOOGLE_APPLICATION_CREDENTIALS` 指向 ADC JSON,或在容器里挂 service account
