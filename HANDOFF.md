# 交接说明 — 给下一个 AI

> 这份文档假设你刚被拉进来,什么都不知道。读完它你应该能立刻接着干活。

## 这是个什么项目

`mechhub-back` 是一个**全新的 Go 后端**,刚起步。当前阶段只完成了**用户系统**(注册 / 邮箱激活 / 登录 / 登出 / 找回密码 / 修改密码 / 个人资料 / 头像)。后面还会加业务模块,具体业务用户还没说。

## 必读的两份文档

按顺序看:

1. **`CLAUDE.md`** — 项目宪法。技术栈、目录约定、代码风格(8 条)、配置约定、CORS、数据库、Postman 约束。**这是硬规则,不能违反**。
2. **`postman/README.md`** — Postman Spec Hub 文件夹格式的 schema 与命名规则。新增接口必须同步加 Postman 文件,这是 CLAUDE.md 里写明的硬约束。

读完这两份再读这份交接说明剩余部分。

## 当前已实现

### 已开放的 8 条接口(详见 `internal/user/route.go`)

```
POST /api/auth/register         邮箱+密码+name 注册,发验证邮件
GET  /api/auth/verify           ?token=xxx,激活账号
POST /api/auth/login            登录,set-cookie session_id
POST /api/auth/logout
POST /api/auth/forgot-password  发重置邮件
POST /api/auth/reset-password   用 token 设新密码,踢掉所有 session
GET  /api/user/me               (需登录) 返回 id/email/name/avatar_url/verified
POST /api/user/update-profile   (需登录) 改 name
POST /api/user/avatar           (需登录) multipart 上传头像 → 阿里云 OSS → 返回新 URL
POST /api/user/change-password  (需登录) 改密码,踢掉所有 session
```

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
- 邮箱已存在 + 未验证 → **不动 user 记录,只刷新 verify token + 重发邮件**

这是为了 cover 邮件失败 / 用户忘记是否注册过的场景。**不能允许覆盖密码**——会被攻击者用来劫持账号(详见之前的讨论:攻击者重新注册并设置自己的密码,等真用户点验证邮件后就拿到了带攻击者密码的已验证账号)。如果有人提议"为什么 register 不允许改密码",直接拒绝,这是有意为之。

### 2. 改密 / 重置密码后所有 session 失效

`ChangePassword` 和 `ResetPassword` 末尾都调 `sessions.DeleteByUser`。这是行业标准,不要去掉。前端要相应处理"改密后跳登录页"。

### 3. 头像存 `avatar_key` 不存 URL

mongo 里只存对象 key(如 `avatars/<uid>/<rand>.png`),返回给前端时由 `oss.PublicURL(key)` 拼 URL。**改 CDN 域名只改 `.env` 不动数据**。

### 4. SwapAvatarKey 是原子的

`internal/user/repo.go::SwapAvatarKey` 用 `FindOneAndUpdate` 一次性拿旧 key + 设新 key。流程是:**先上传新文件成功 → swap → 删旧文件(best-effort)**,任何一步失败都不会出现"DB 指向不存在 OSS 对象"的孤儿状态。

### 5. ForgotPassword 邮箱不存在也返回成功

防枚举。不要改。

### 6. Resend / Gmail 折叠对话的坑

用户之前以为"每次注册收到的 token 都一样" —— 实际是 Gmail 把同主题邮件折叠成 thread,默认显示最早一封。**后端确实每次生成不同 token**,这点已验证。如果用户再提这个问题,引导他看 Resend 控制台 Logs 或换非 Gmail 邮箱测试。

### 7. `OSS_PUBLIC_BASE_URL` 必须带 `https://`

`oss.go` 里直接拼接 `publicBase + "/" + key`,如果没带协议头会得到无效 URL。`.env.example` 有正确示例。

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
- 用户当前用 mongo 是 `mongodb://oft:oft@oftheloneliness.cn/...`(他自己服务器上的 mongo,不是本地)。
- Resend 的 `MAIL_FROM` 用 `MechHub <no-reply@mechhub.oftheloneliness.cn>`,域名已经在 Resend 验证过。
- OSS:`mechhub-avatar` bucket,`cn-hangzhou`。AccessKey 之前在对话里曝光过,我建议用户轮换并降权(只授 PutObject + DeleteObject)。**确认下他是否做了这件事**,如果没做,提醒一下。

## 项目当前状态

- `go build ./...` 通过。
- 用户已实测跑通完整流程:注册 → 验证 → 登录 → me → 修改 name → 上传头像 → 改密码 → 找回密码。
- 所有 Postman 请求都验证过(Spec Hub 格式,VCS 友好)。
- 没有任何已知 bug。

接下来等用户提新需求。Good luck.
