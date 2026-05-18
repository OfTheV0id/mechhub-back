# 更改日志

记录每一轮 AI 协作 / PR 的实质性改动。新条目放在最上面。

格式约定:
- 标题用 `## <轮次> — <日期> — <主要执行者>`
- 改动按"破坏性 / 功能 / 修复 / 杂项"分组
- 破坏性变更必须标 ⚠️,并说明前端 / 调用方需要怎么改

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
