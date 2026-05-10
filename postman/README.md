# Postman Collection (Spec Hub 格式)

本目录是 Postman 的 **VCS / Spec Hub 文件夹格式**,Postman 桌面端可以直接同步。新增/修改接口必须按此格式提交。

## 目录约定

```
postman/
├── collections/
│   └── <Collection Name>/
│       ├── .resources/definition.yaml          # 集合根定义 + 集合变量
│       └── <group>/                             # 一级分组(如 auth、user)
│           ├── .resources/definition.yaml      # 分组定义,含 order
│           └── <slug>/                          # 单个请求,文件夹名=URL 末段
│               └── <Title>.request.yaml        # 请求文件,文件名=请求展示名
└── environments/
    └── <name>.environment.yaml                 # 环境变量
```

**命名规则**:
- 集合 / 分组 / 请求文件夹用 **kebab-case 或与路径对齐**(`forgot-password`、`change-password`)。
- 请求文件名是**人类可读标题** + `.request.yaml`(如 `Log in with email and password.request.yaml`)。这个标题就是 Postman 里看到的请求名,要求祈使句、首字母大写。

## 文件 schema

### 集合根 `<Collection Name>/.resources/definition.yaml`

```yaml
$kind: collection
description: >-
  多行说明。介绍这个集合是给谁用的、认证方式、跨请求依赖。
variables:
  baseUrl: http://localhost:8080
  email: test@example.com
  # 任何跨请求复用的占位变量都写这里,环境文件会覆盖同名 key
```

### 分组 `<group>/.resources/definition.yaml`

```yaml
$kind: collection
description: >-
  本分组的范围说明。如:是否需要登录、相关业务概念。
order: 1000   # 越小越靠前。建议公共接口 1000、需登录 2000、管理 3000
```

### 单个请求 `<slug>/<Title>.request.yaml`

```yaml
$kind: http-request
name: Log in with email and password
description: >-
  非显而易见的副作用要写,例如"成功后所有 session 失效",或"返回 200 不代表邮箱存在"。
url: "{{baseUrl}}/api/auth/login"
method: POST
headers:
  Content-Type: application/json
  Accept: application/json
body:
  type: json
  content: |-
    {
      "email": "{{email}}",
      "password": "{{password}}"
    }
order: 3000
```

要点:
- `url` 必须用 `{{baseUrl}}` 起头,不写死域名。
- query 参数直接拼在 url 里:`{{baseUrl}}/api/auth/verify?token={{verifyToken}}`。
- `body.content` 用 YAML 多行 `|-` 块,内部就是 JSON 字符串。占位变量用 `{{name}}`。
- GET 请求**不要**写 `body` 段。
- `order` 在分组内排序,建议用千位数留间隔(1000、2000…),便于后续插入。

### 环境 `environments/<name>.environment.yaml`

```yaml
# 顶部注释说明该环境的特殊点(如认证方式、需要的外部服务)
name: local
values:
  - key: baseUrl
    value: 'http://localhost:8080'
    enabled: true
  - key: email
    value: test@example.com
    enabled: true
```

`name` 字段就是 Postman 右上角下拉里看到的环境名。`values` 里的 key 会覆盖集合变量同名 key。

## 添加新接口的步骤

1. 假设要加 `POST /api/order/create`,且属于一个新分组 `order`:
2. 建 `collections/MechHub Backend/order/.resources/definition.yaml`,写 `$kind: collection` + `order:`。
3. 建 `collections/MechHub Backend/order/create/Create a new order.request.yaml`,按上面模板填。
4. 如果接口要用新的占位变量(比如 `orderId`),在 **集合根** `.resources/definition.yaml` 的 `variables` 段加默认值;需要环境维度区分的也加进 `local.environment.yaml`。
5. 如果接口需要登录,把它放在已经有 `cookie` 上下文的分组里(目前 `user/`),或单独建 `order` 分组并在描述里注明 "Requires session"。

## 不要做的事

- **不要**在 `url` 里写死 `http://localhost:8080`,一定走 `{{baseUrl}}`。
- **不要**把请求文件命名成 `request.yaml`(没标题),Postman 会显示为空白名。
- **不要**把敏感值(token、API key、生产密码)落进环境文件提交;占位即可,实际运行时在 Postman 里手填。
- **不要**手动维护单文件的 `*.postman_collection.json`,这种格式和 Spec Hub 不兼容,会冲突。
