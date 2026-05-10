# MechHub Backend — 项目规约

> 所有 AI 协作与人工 PR 必须遵守。这是项目宪法,优先级高于一般直觉。

## 技术栈

- 语言:Go (`go.mod` 已定基线版本)
- Web 框架:`gin-gonic/gin`
- 数据库:MongoDB (`go.mongodb.org/mongo-driver/v2`)
- 配置:`.env` + `joho/godotenv`,`internal/config` 集中加载
- 邮件:`resend-go/v3`
- 密码:`golang.org/x/crypto/bcrypt`
- 认证:Session ID + Cookie(服务端 session 存 MongoDB,带 TTL),**不用 JWT**

## 目录约定

```
mechhub-back/
├── main.go                       # 入口,只做依赖装配 + listen
├── .env / .env.example
└── internal/
    ├── config/                   # typed Config,启动时校验必填
    ├── db/                       # mongo client + 索引初始化
    ├── mail/                     # 邮件发送
    ├── middleware/               # cors / auth
    ├── session/                  # session 存储 (mongo)
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

## 数据库

- 启动时 `EnsureIndexes`:
  - `users.email` unique
  - `sessions.expires_at` TTL=0
  - `tokens.expires_at` TTL=0,`tokens.user_id+kind` 复合索引
- 用 `bson.ObjectID`,不用 string ID。
- 集合名复数小写:`users`、`sessions`、`tokens`。
