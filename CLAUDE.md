# MechHub Backend — 项目规约

> 所有 AI 协作与人工 PR 必须遵守。这是项目宪法,优先级高于一般直觉。

## 技术栈

- 语言:Go (`go.mod` 已定基线版本)
- Web 框架:`gin-gonic/gin`
- 数据库:MongoDB (`go.mongodb.org/mongo-driver/v2`)
- 配置:`.env` + `joho/godotenv`,`internal/config` 集中加载
- 邮件:`resend-go/v3`
- 对象存储:阿里云 OSS (`aliyun/aliyun-oss-go-sdk`)
- OAuth:`golang.org/x/oauth2`(Google)
- 密码:`golang.org/x/crypto/bcrypt`
- 认证:Session ID + Cookie(服务端 session 存 MongoDB,带 TTL),**不用 JWT**
- AI agent 后端:Python FastAPI(项目 `mechhub-agent`,Google ADK),Go 通过 HTTP + SSE 调用,**Go 不自己接 LLM SDK**
- 部署:Docker Compose(父目录 `mechhub/docker-compose.yml`,三服务 go-backend + agent + mongo)

## 目录约定

```
mechhub-back/
├── main.go                       # 入口,只做依赖装配 + listen
├── Dockerfile                    # 多阶段构建,alpine 输出
├── .env / .env.example
└── internal/
    ├── config/                   # typed Config,启动时校验必填
    ├── db/                       # mongo client + 索引初始化
    ├── mail/                     # 邮件发送
    ├── middleware/               # cors / auth
    ├── session/                  # session 存储 (mongo)
    ├── storage/                  # OSS 客户端封装(Upload / Download / PublicURL)
    ├── oauth/                    # Google OAuth 客户端
    ├── agent/                    # Python ADK HTTP 客户端(POST /chat,SSE 解析)
    ├── response/                 # 统一响应 + 错误码常量
    ├── router/                   # 装配路由,不写具体路径
    └── <feature>/                # 功能模块,自带 route.go
        ├── route.go              # Mount(g, h, ...) 一眼可见全部路由
        ├── handler.go            # 参数解析 + 调 service + 返回
        ├── service.go            # 业务逻辑
        ├── repo.go               # 数据访问
        ├── model.go              # struct + DTO
        └── *.go                  # 其它本模块文件
```

当前 feature module:`user/`、`solochat/`(后者还多了 `grading.go` + `events_hub.go` + `streamer.go`,因为流式 + 异步任务编排)。

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
   - mongo client、配置、mail sender 在 `main.go` 初始化,`New(...)` 注入到各模块。
   - 禁止 `var DB *mongo.Client` 这种 package-level 全局。

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

- 启动时 `EnsureIndexes`:
  - `users.email` unique
  - `sessions.expires_at` TTL=0
  - `tokens.expires_at` TTL=0,`tokens.user_id+kind` 复合索引
  - solochat 7 个集合的 user_id / conversation_id / task_id 索引(详见 `internal/db/mongo.go`)
- 用 `bson.ObjectID`,不用 string ID。
- 集合命名:**用户系统** `users` / `sessions` / `tokens`;**Solochat** 加 `solochat_` 前缀(`solochat_conversations` / `solochat_messages` / `solochat_grading_tasks` / 等);**通用** `uploaded_files`(头像 + solochat 附件共用)。
- 加新模块时新建的集合应该加同样的模块名前缀,便于看名字就知道归属。

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

- **NDJSON**(`application/x-ndjson`):用于"一次请求 + 一次流式响应"的场景(如发消息)。一行一个完整 JSON,`type` discriminator。Go 端用 `streamer.go::newNDJSON` + `c.Writer.Write` + `http.Flusher.Flush`。
- **SSE**(`text/event-stream`):用于"长连接订阅",客户端可断开重连。帧格式 `event: <name>\ndata: <json>\n\n`,**必须**每 25s 一行 `: ping` 心跳,防代理超时。Go 端用 `streamer.go::newSSE`。
- 都要 set `X-Accel-Buffering: no` 头,防止 nginx 缓冲整个响应。
- **异步任务事件**(如批改进度):用 `events_hub.go` 的内存 pub/sub,goroutine 执行任务 → `hub.Publish` → 多个 SSE 订阅者 `hub.Subscribe` fan-out。**服务重启就丢**,所以启动时必须把 `status=processing` 的孤儿任务标 `failed`(参考 `solochat.RecoverPendingTasks`)。

## Python Agent 对接(`internal/agent/`)

- Python ADK agent 跑在 `mechhub-agent` 项目,通过 `POST /chat`(multipart + SSE)对外。
- Go 端唯一接入点:`internal/agent/client.go::Chat`,返回 `<-chan Event`。
- **Go 不直接调 LLM SDK**(Google Gemini / OpenAI 之类),全部转给 Python。Go 只做权限 / 持久化 / 流式协议翻译。
- 通信路径:本机开发 `http://localhost:8001`,Docker Compose `http://agent:8001`。`AGENT_BASE_URL` 走 env。
- Python agent 端的 SSE 帧只有 `data: <json>\n\n` 行(没有 `event:` 行),用 `data.type` 字段区分。

## Docker

- 父目录 `mechhub/docker-compose.yml` 起 3 个服务:`go-backend`(暴露 8080)、`agent`(只在 Compose 网络内)、`mongo`(持久化卷)。
- 容器间用服务名互访:`http://agent:8001`、`mongodb://mongo:27017`。
- 各服务 `.env` 走 `env_file:` 引用,**不要 commit 真值**。
- 本地开发不强制走 Docker,`go run .` + Python `uvicorn` 两个进程也行,只需把 `AGENT_BASE_URL` 改成 localhost。
