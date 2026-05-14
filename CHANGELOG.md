# 更改日志

记录每一轮 AI 协作 / PR 的实质性改动。新条目放在最上面。

格式约定:
- 标题用 `## <轮次> — <日期> — <主要执行者>`
- 改动按"破坏性 / 功能 / 修复 / 杂项"分组
- 破坏性变更必须标 ⚠️,并说明前端 / 调用方需要怎么改

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
