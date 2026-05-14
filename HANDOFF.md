# 交接说明 — 给下一个 AI

> 这份文档假设你刚被拉进来,什么都不知道。读完它你应该能立刻接着干活。

## 这是个什么项目

`mechhub-back` 是一个 Go 后端,现已完成:
- **用户系统**:注册 / 邮箱激活 / role 双轨(student / teacher,后者由 admin 审批) / 登录 / 登出 / 找回密码 / 改密 / 资料 / 头像 / Google OAuth
- **Solochat 模块**:通用 agent 对话,前端可见 thinking + 工具调用 + 工具结果。**调用 `mechhub-agent` Python ADK 服务做实际 LLM 工作**。批改不再是独立功能,是 agent 自主调用 `grade_submission` 工具的一个特例
- **Docker 化**:父目录 `mechhub/docker-compose.yml` 起 go-backend + agent + mongo 三服务

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
POST   /api/solochat/conversations/:id/messages/stream      发消息,NDJSON 流式(10 种事件,见下)
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

Message 持久化为 `Parts []MessagePart`,part type 可选:`text` / `thinking` / `tool_use` / `tool_result`。`GET /messages` 返回这些 parts,前端按数组顺序逐块渲染。

### 关键技术决策

| 决策 | 实现 |
|---|---|
| 认证 | Session ID + HttpOnly Cookie,session 存 mongo,TTL 索引自动过期。**不用 JWT** |
| 数据库 | MongoDB(`v2` 驱动),`bson.ObjectID` 作 ID |
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

**state CSRF 防护**:用 HttpOnly cookie `oauth_state` 而不是 mongo TTL 记录,简单且免一次查库。callback 第一时间清空。

**Open Redirect 防护**:**通过"不接受用户输入"消除攻击面**。`/auth/google` 不接 query 参数,登录后一律跳 `GOOGLE_DEFAULT_RETURN_URL`。如果未来要支持 deep link("登录前点的链接登录完跳回去")再加白名单参数。不要轻易开 `?return=` 的口子。

**头像镜像**:`mirrorGoogleAvatar` 用 `http.Client` GET picture URL → 检测 content-type → 走 OSS 上传同一个 `avatars/<uid>/<rand>.<ext>` 命名规则。**失败时 swallow error,不阻断登录**——头像没了用户可以登录后自己改。

**Google 头像 URL 是公开的**(`lh3.googleusercontent.com/...`),GET 不需要鉴权。

**错误处理**:OAuth 失败时不返回 4xx/5xx JSON,而是 redirect 回 return URL,带 `?oauth_error=...` 参数,前端读 query 显示错误提示。这是 OAuth 流程的常规做法。

**Google Console 必须匹配的设置**:
- "Authorized redirect URIs" 必须包含 `GOOGLE_REDIRECT_URL` 的值(开发是 `http://localhost:8080/api/auth/google/callback`)
- scopes 默认 `openid email profile`,后端代码里硬编码

### 4. Solochat:通用 agent chat(轮 4 重构)

**职责划分**:
- **Go 后端**:权限、会话、消息持久化(`Message.Parts []MessagePart`)、流式协议翻译、附件存储编排
- **Python agent** (`mechhub-agent`, FastAPI + Google ADK):LLM 推理 + 工具调用,通过 `POST /chat`(multipart + SSE)对外。当前模型 `openai/qwen3.6-max-preview`,通过 LiteLlm 的 `extra_body={"enable_thinking": True}` 让 qwen 输出思考过程
- Go ↔ agent 用 HTTP,Docker Compose 内部 `http://agent:8001` 直连

**为什么不在 Go 里跑 ADK**:Google ADK 只有 Python SDK。

**关键代码位置**:
- `internal/agent/{model,client}.go`:HTTP 客户端 + SSE 解析,`Event` 字段覆盖所有 8 种类型
- `internal/solochat/`:6 个文件(model/repo/service/handler/route/streamer)。**`grading.go` 和 `events_hub.go` 已删除**
- `internal/solochat/service.go::consumeAgentStream`:核心翻译循环,把 agent SSE 事件 1:1 写成 NDJSON 给前端,同时累积到 `Message.Parts` 落库
- `mechhub-agent/server/sse.py::stream_chat`:Python 端统一生成器,把 ADK 事件流 + tool callback 队列合并发出
- `mechhub-agent/mechhub_agent/callbacks.py`:`before/after_tool_callback` 通过 `contextvars.ContextVar` 拿到本次请求的 `asyncio.Queue`,把 `tool_call_start` / `tool_result` 入队
- `mechhub-agent/mechhub_agent/agent.py::_build_model`:控制 `enable_thinking` 开关

**事件协议**:见上文"已开放的接口"段的 NDJSON 事件表。Python SSE → Go NDJSON 是 1:1 透传 + 加 `user_input` / `conversation_title` 两个 Go-only 事件。

**附件流转**(轮 5 修过):前端 → multipart 上传到 Go(MIME 白名单 image/PDF/text/markdown,DOCX 拒收)→ Go 写 OSS → DB 存 key → 发消息时 Go 下载 OSS → multipart 转给 agent(字段 `files`)→ Python `build_user_content` 把字节作为 `Part.from_bytes(...)` 直接塞进 ADK Content,LLM 能"看见"图片 / PDF;文本 / Markdown 内容 inline 拼到 prompt。图片同时落盘一份保留给 `grade_submission` / `ocr_images_cached` 工具复用。**Go 是中间人,Python 不直接访问 OSS**(用户 OSS AK 不出 Go)。

**批改不是独立功能**:LLM 自主决定何时调 `grade_submission(image_paths)` 工具。工具会自动用 OCR 缓存(本次 session state),返回完整 `GradingOutput` JSON。前端从对应 `tool_result.output` 渲染分数 + 评语 + 步骤分析。**`/grading-tasks/*` 端点已经全部下线**。

**agent session 持久化**:agent 当前 in-memory session,重启丢。所以 Go 端每次都重传文件。后续可以把 OCR 缓存搬到 mongo,但暂时 OK。

### 5. 改密 / 重置密码后所有 session 失效

`ChangePassword` 和 `ResetPassword` 末尾都调 `sessions.DeleteByUser`。这是行业标准,不要去掉。前端要相应处理"改密后跳登录页"。

### 6. 头像存 `avatar_key` 不存 URL

mongo 里只存对象 key(如 `avatars/<uid>/<rand>.png`),返回给前端时由 `oss.PublicURL(key)` 拼 URL。**改 CDN 域名只改 `.env` 不动数据**。

### 7. SwapAvatarKey 是原子的

`internal/user/repo.go::SwapAvatarKey` 用 `FindOneAndUpdate` 一次性拿旧 key + 设新 key。流程是:**先上传新文件成功 → swap → 删旧文件(best-effort)**,任何一步失败都不会出现"DB 指向不存在 OSS 对象"的孤儿状态。

### 8. ForgotPassword 邮箱不存在也返回成功

防枚举。不要改。

### 9. Resend / Gmail 折叠对话的坑

用户之前以为"每次注册收到的 token 都一样" —— 实际是 Gmail 把同主题邮件折叠成 thread,默认显示最早一封。**后端确实每次生成不同 token**,这点已验证。如果用户再提这个问题,引导他看 Resend 控制台 Logs 或换非 Gmail 邮箱测试。

### 10. `OSS_PUBLIC_BASE_URL` 必须带 `https://`

`oss.go` 里直接拼接 `publicBase + "/" + key`,如果没带协议头会得到无效 URL。`.env.example` 有正确示例。

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
MONGO_URI=<生产 mongo>
CORS_ENABLED=false                ← 同域不需要 CORS
SESSION_COOKIE_SECURE=true        ← HTTPS 必开
SESSION_COOKIE_SAMESITE=lax        ← 同域,lax 够用
APP_BASE_URL=https://mechhub.oftheloneliness.cn
GOOGLE_REDIRECT_URL=https://mechhub.oftheloneliness.cn/api/auth/google/callback
GOOGLE_DEFAULT_RETURN_URL=https://mechhub.oftheloneliness.cn
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

- **速率限制**(register / forgot-password 防邮件轰炸):未做。设计选择没定(mongo 计数 vs Redis)。
- **信息泄漏收紧**(把"邮箱已注册"和"未注册"两种响应统一):未做。等接前端、看产品形态再说。
- **resend verification email 独立端点**:未做。当前用"重新 register"已经能 cover。
- **单元测试**:**完全没有**。`internal/user/service_test.go` 起 mongo container 跑 happy-path 是合理起点。
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
- 用户**没有** docker / mongosh 在 PATH。我之前清理 mongo 用的是写一次性 Go 程序到 `/tmp/<x>/main.go` → `go run` → 删除。这套有效,可以复用。
- 用户当前用 mongo 是 `mongodb://oft:oft@oftheloneliness.cn/...`(他自己服务器上的 mongo,不是本地)。期间临时切换过 localhost mongo,后又换回去。
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
