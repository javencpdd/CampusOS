# API 设计 (API Design)

> 本文档定义 CampusOS 的 API 设计规范，包括 RESTful API 设计原则、统一响应体、认证体系、版本控制、错误处理与 OpenAPI 规范。

---

## 目录

- [设计原则](#设计原则)
- [URL 设计规范](#url-设计规范)
- [统一响应体](#统一响应体)
- [HTTP 方法语义](#http-方法语义)
- [分页与过滤](#分页与过滤)
- [认证与授权](#认证与授权)
- [版本控制](#版本控制)
- [错误处理](#错误处理)
- [限流策略](#限流策略)
- [OpenAPI 规范](#openapi-规范)
- [核心 API 列表](#核心-api-列表)

---

## 设计原则

### 1. API First

所有服务的接口定义**先于实现**：

- 使用 OpenAPI 3.0 (Swagger) 定义 RESTful API
- 使用 Protobuf 定义 gRPC 服务接口
- 接口定义即文档，即契约
- 前后端可以基于接口定义并行开发

### 2. RESTful 风格

遵循 REST 架构风格：

| 原则 | 描述 |
|:---|:---|
| **资源导向** | URL 表示资源，HTTP 方法表示操作 |
| **无状态** | 每个请求包含所有必要信息 |
| **统一接口** | 统一的 URL 格式、响应体、错误码 |
| **可缓存** | 合理使用 HTTP 缓存头 |
| **分层系统** | 客户端无需了解中间层 |

### 3. 一致性

- 统一的 URL 命名规范（kebab-case）
- 统一的响应体格式
- 统一的错误码体系
- 统一的认证方式

---

## URL 设计规范

### 基础格式

```
https://{host}/api/{version}/{resource}
https://{host}/api/{version}/{resource}/{id}
https://{host}/api/{version}/{resource}/{id}/{sub-resource}
```

### 命名规则

- 使用 **kebab-case**（连字符分隔）：`/api/v1/user-profiles`
- 资源使用**复数名词**：`/threads` 而非 `/thread`
- 子资源使用**嵌套路由**：`/threads/{id}/posts`
- 查询参数使用 **snake_case**：`?page_size=20&sort_by=created_at`

### URL 示例

```
# 身份认证
POST   /api/v1/auth/login                    # 登录
POST   /api/v1/auth/logout                   # 登出
POST   /api/v1/auth/register                 # 注册
POST   /api/v1/auth/refresh                  # 刷新 Token
GET    /api/v1/auth/me                       # 获取当前用户信息

# 用户管理
GET    /api/v1/users                         # 用户列表
GET    /api/v1/users/{id}                    # 用户详情
PUT    /api/v1/users/{id}                    # 更新用户
DELETE /api/v1/users/{id}                    # 删除用户
GET    /api/v1/users/{id}/threads            # 用户的帖子列表
GET    /api/v1/users/{id}/posts              # 用户的回复列表

# 帖子管理
GET    /api/v1/threads                       # 帖子列表
POST   /api/v1/threads                       # 创建帖子
GET    /api/v1/threads/{id}                  # 帖子详情
PUT    /api/v1/threads/{id}                  # 更新帖子
DELETE /api/v1/threads/{id}                  # 删除帖子
POST   /api/v1/threads/{id}/pin              # 置顶帖子
DELETE /api/v1/threads/{id}/pin              # 取消置顶
GET    /api/v1/threads/{id}/posts            # 帖子的回复列表

# 回复管理
POST   /api/v1/threads/{thread_id}/posts     # 创建回复
PUT    /api/v1/posts/{id}                    # 更新回复
DELETE /api/v1/posts/{id}                    # 删除回复

# 版块管理
GET    /api/v1/categories                    # 版块列表
POST   /api/v1/categories                    # 创建版块
GET    /api/v1/categories/{id}               # 版块详情
PUT    /api/v1/categories/{id}               # 更新版块
DELETE /api/v1/categories/{id}               # 删除版块
GET    /api/v1/categories/{id}/threads       # 版块的帖子列表

# 角色权限
GET    /api/v1/roles                         # 角色列表
POST   /api/v1/roles                         # 创建角色
PUT    /api/v1/roles/{id}                    # 更新角色
DELETE /api/v1/roles/{id}                    # 删除角色
POST   /api/v1/users/{id}/roles              # 给用户分配角色
DELETE /api/v1/users/{id}/roles/{role_id}    # 撤销用户角色

# 插件管理
GET    /api/v1/plugins                       # 插件列表
POST   /api/v1/plugins                       # 安装插件
GET    /api/v1/plugins/{id}                  # 插件详情
PUT    /api/v1/plugins/{id}/config           # 更新插件配置
POST   /api/v1/plugins/{id}/enable           # 启用插件
POST   /api/v1/plugins/{id}/disable          # 禁用插件
DELETE /api/v1/plugins/{id}                  # 卸载插件

# 文件上传
POST   /api/v1/files/upload                  # 上传文件
GET    /api/v1/files/{key}                   # 获取文件信息
DELETE /api/v1/files/{key}                   # 删除文件

# 系统配置
GET    /api/v1/configurations                # 获取配置列表
GET    /api/v1/configurations/{key}          # 获取配置项
PUT    /api/v1/configurations/{key}          # 更新配置项

# 审计日志
GET    /api/v1/audit-logs                    # 审计日志列表
GET    /api/v1/audit-logs/{id}               # 审计日志详情

# AI 网关
POST   /api/v1/ai/chat                       # AI 对话
GET    /api/v1/ai/models                     # 可用模型列表

# Webhook
GET    /api/v1/webhooks                      # Webhook 列表
POST   /api/v1/webhooks                      # 创建 Webhook
PUT    /api/v1/webhooks/{id}                 # 更新 Webhook
DELETE /api/v1/webhooks/{id}                 # 删除 Webhook
```

---

## 统一响应体

### 成功响应

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": "1234567890",
    "title": "欢迎来到 CampusOS",
    "content": "这是帖子内容..."
  }
}
```

### 列表响应（带分页）

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "items": [
      {
        "id": "1234567890",
        "title": "帖子标题"
      },
      {
        "id": "1234567891",
        "title": "另一个帖子"
      }
    ],
    "pagination": {
      "page": 1,
      "page_size": 20,
      "total": 150,
      "total_pages": 8
    }
  }
}
```

### 错误响应

```json
{
  "code": 40001,
  "msg": "invalid request parameters",
  "error": {
    "type": "validation_error",
    "details": [
      {
        "field": "title",
        "message": "title is required"
      },
      {
        "field": "content",
        "message": "content must be at least 10 characters"
      }
    ]
  }
}
```

### 响应结构定义

```go
// Response 统一响应体
type Response struct {
    Code    int         `json:"code"`              // 业务状态码，0 表示成功
    Msg     string      `json:"msg"`               // 状态消息
    Data    interface{} `json:"data,omitempty"`     // 响应数据
    Error   *ErrorInfo  `json:"error,omitempty"`    // 错误详情
}

// ListResponse 列表响应
type ListResponse struct {
    Items      interface{} `json:"items"`
    Pagination *Pagination `json:"pagination"`
}

// Pagination 分页信息
type Pagination struct {
    Page       int   `json:"page"`        // 当前页码
    PageSize   int   `json:"page_size"`   // 每页数量
    Total      int64 `json:"total"`       // 总记录数
    TotalPages int   `json:"total_pages"` // 总页数
}

// ErrorInfo 错误详情
type ErrorInfo struct {
    Type    string      `json:"type"`              // 错误类型
    Details interface{} `json:"details,omitempty"` // 详细错误信息
}

// 快捷构造函数
func Success(data interface{}) *Response {
    return &Response{Code: 0, Msg: "success", Data: data}
}

func Error(code int, msg string) *Response {
    return &Response{Code: code, Msg: msg}
}

func List(items interface{}, pagination *Pagination) *Response {
    return &Response{
        Code: 0,
        Msg:  "success",
        Data: &ListResponse{Items: items, Pagination: pagination},
    }
}
```

---

## HTTP 方法语义

| 方法 | 语义 | 幂等性 | 示例 |
|:---|:---|:---|:---|
| `GET` | 获取资源 | ✅ 幂等 | `GET /api/v1/threads/{id}` |
| `POST` | 创建资源 | ❌ 非幂等 | `POST /api/v1/threads` |
| `PUT` | 全量更新资源 | ✅ 幂等 | `PUT /api/v1/threads/{id}` |
| `PATCH` | 部分更新资源 | ✅ 幂等 | `PATCH /api/v1/threads/{id}` |
| `DELETE` | 删除资源 | ✅ 幂等 | `DELETE /api/v1/threads/{id}` |

### 方法与状态码映射

| 方法 | 成功状态码 | 说明 |
|:---|:---|:---|
| `GET` | `200 OK` | 成功获取资源 |
| `POST` | `201 Created` | 成功创建资源 |
| `PUT` | `200 OK` | 成功更新资源 |
| `PATCH` | `200 OK` | 成功部分更新资源 |
| `DELETE` | `204 No Content` | 成功删除资源 |

---

## 分页与过滤

### 分页参数

| 参数 | 类型 | 默认值 | 说明 |
|:---|:---|:---|:---|
| `page` | int | 1 | 页码（从 1 开始） |
| `page_size` | int | 20 | 每页数量（最大 100） |

```
GET /api/v1/threads?page=2&page_size=10
```

### 排序参数

| 参数 | 类型 | 说明 |
|:---|:---|:---|
| `sort_by` | string | 排序字段（如 `created_at`, `reply_count`） |
| `sort_order` | string | 排序方向：`asc`（升序）/ `desc`（降序） |

```
GET /api/v1/threads?sort_by=created_at&sort_order=desc
```

### 过滤参数

| 参数 | 类型 | 说明 |
|:---|:---|:---|
| `keyword` | string | 关键词搜索 |
| `category_id` | string | 版块过滤 |
| `author_id` | string | 作者过滤 |
| `status` | string | 状态过滤 |
| `created_after` | string | 创建时间起始（RFC 3339） |
| `created_before` | string | 创建时间结束（RFC 3339） |

```
GET /api/v1/threads?category_id=123&status=published&keyword=CampusOS
```

### 字段选择

```
GET /api/v1/threads/{id}?fields=id,title,content,author
```

---

## 认证与授权

### 第一方认证（JWT）

**登录流程：**

```
POST /api/v1/auth/login
Content-Type: application/json

{
  "email": "user@example.com",
  "password": "********"
}

# 响应
{
  "code": 0,
  "msg": "success",
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiIs...",
    "refresh_token": "rt_abc123def456...",
    "token_type": "Bearer",
    "expires_in": 7200
  }
}
```

**请求携带 Token：**

```
GET /api/v1/users/me
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

**刷新 Token：**

```
POST /api/v1/auth/refresh
Content-Type: application/json

{
  "refresh_token": "rt_abc123def456..."
}
```

### 第三方认证（API Key + HMAC）

**API Key 申请：**

第三方应用通过开发者中心申请 API Key，获得：

- `api_key`：应用标识
- `api_secret`：用于签名的密钥（仅显示一次）

**请求签名：**

```
# 签名算法
sign_string = HTTP_METHOD + "\n" +
              REQUEST_PATH + "\n" +
              SORTED_QUERY_STRING + "\n" +
              TIMESTAMP + "\n" +
              REQUEST_BODY_HASH

signature = HMAC-SHA256(api_secret, sign_string)

# 请求头
GET /api/v1/threads
X-API-Key: ak_abc123
X-Timestamp: 1700000000
X-Signature: base64(hmac_sha256_signature)
```

### 认证中间件

```go
// 认证中间件 - 支持 JWT 和 API Key 双模式
func Auth() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 尝试 JWT 认证
        token := extractBearerToken(c)
        if token != "" {
            claims, err := jwt.Verify(token)
            if err == nil {
                c.Set("user_id", claims.UserID)
                c.Set("auth_method", "jwt")
                c.Next()
                return
            }
        }
        
        // 尝试 API Key 认证
        apiKey := c.GetHeader("X-API-Key")
        if apiKey != "" {
            err := verifyHMACSignature(c)
            if err == nil {
                c.Set("api_key", apiKey)
                c.Set("auth_method", "api_key")
                c.Next()
                return
            }
        }
        
        c.JSON(http.StatusUnauthorized, Error(ErrCodeUnauthorized, "unauthorized"))
        c.Abort()
    }
}
```

---

## 版本控制

### URL 路径版本号

强制实行 URL 路径版本号隔离：

```
/api/v1/threads        # v1 版本
/api/v2/threads        # v2 版本（如果有破坏性变更）
```

### 版本生命周期

```
v1 (当前稳定版)
  │
  ├── v2 发布 → v1 进入维护模式（6个月）
  │              │
  │              ├── v2 稳定后 → v1 进入废弃警告（3个月）
  │              │                │
  │              │                └── 3个月后 → v1 下线
  │              │
  │              └── 继续接收安全补丁
  │
  └── 请求 v1 API 的响应头中包含:
      Deprecation: true
      Sunset: Sat, 01 Jul 2025 00:00:00 GMT
      Link: <https://api.campusos.io/docs/migration/v1-to-v2>; rel="successor-version"
```

### 向后兼容性规则

| 变更类型 | 兼容性 | 处理方式 |
|:---|:---|:---|
| 新增可选字段 | ✅ 兼容 | 在当前版本添加 |
| 新增 API 端点 | ✅ 兼容 | 在当前版本添加 |
| 修改字段类型 | ❌ 不兼容 | 新增版本 |
| 删除字段 | ❌ 不兼容 | 新增版本 |
| 修改响应结构 | ❌ 不兼容 | 新增版本 |
| 修改错误码 | ❌ 不兼容 | 新增版本 |

---

## 错误处理

### 错误码体系

**错误码格式：** `AABBB`

- `AA`：模块编码（2位）
- `BBB`：具体错误编码（3位）

### 模块编码

| 编码 | 模块 |
|:---|:---|
| `10` | 通用错误 |
| `20` | 认证/授权 |
| `30` | 用户模块 |
| `40` | 帖子/回复模块 |
| `50` | 版块模块 |
| `60` | 插件模块 |
| `70` | 文件模块 |
| `80` | 系统模块 |
| `90` | AI 模块 |

### 通用错误码

| 错误码 | HTTP 状态码 | 说明 |
|:---|:---|:---|
| `10001` | 400 | 请求参数错误 |
| `10002` | 400 | 请求体解析失败 |
| `10003` | 404 | 资源不存在 |
| `10004` | 409 | 资源冲突（如重复创建） |
| `10005` | 429 | 请求频率超限 |
| `10006` | 500 | 服务器内部错误 |
| `10007` | 503 | 服务不可用 |
| `10008` | 408 | 请求超时 |

### 认证/授权错误码

| 错误码 | HTTP 状态码 | 说明 |
|:---|:---|:---|
| `20001` | 401 | 未认证（缺少 Token） |
| `20002` | 401 | Token 无效或已过期 |
| `20003` | 401 | Refresh Token 无效或已过期 |
| `20004` | 403 | 权限不足 |
| `20005` | 401 | API Key 无效 |
| `20006` | 401 | 签名验证失败 |
| `20007` | 423 | 账号已被封禁 |

### 业务错误码

| 错误码 | HTTP 状态码 | 说明 |
|:---|:---|:---|
| `30001` | 409 | 用户名已存在 |
| `30002` | 409 | 邮箱已注册 |
| `30003` | 400 | 密码强度不足 |
| `30004` | 404 | 用户不存在 |
| `40001` | 400 | 帖子标题不能为空 |
| `40002` | 400 | 帖子内容过长 |
| `40003` | 404 | 帖子不存在 |
| `40004` | 403 | 无权编辑此帖子 |
| `40005` | 403 | 无权删除此帖子 |
| `40006` | 423 | 帖子已被锁定 |
| `50001` | 409 | 版块名称已存在 |
| `50002` | 404 | 版块不存在 |
| `50003` | 403 | 版块已关闭发帖 |
| `60001` | 400 | 插件 Manifest 格式错误 |
| `60002` | 409 | 插件已安装 |
| `60003` | 404 | 插件不存在 |
| `60004` | 400 | 插件权限不足 |

### 错误响应示例

```json
{
  "code": 40004,
  "msg": "you do not have permission to edit this thread",
  "error": {
    "type": "permission_denied",
    "details": {
      "required_permission": "thread:edit:own",
      "resource_owner": "user_123",
      "current_user": "user_456"
    }
  }
}
```

---

## 限流策略

### 限流规则

| 端点 | 限流策略 | 限制 |
|:---|:---|:---|
| `POST /auth/login` | IP 维度 | 5 次/分钟 |
| `POST /auth/register` | IP 维度 | 3 次/分钟 |
| `GET /*` | 用户/IP 维度 | 100 次/分钟 |
| `POST /threads` | 用户维度 | 10 次/分钟 |
| `POST /posts` | 用户维度 | 30 次/分钟 |
| `POST /files/upload` | 用户维度 | 20 次/分钟 |
| `POST /ai/chat` | 用户维度 | 10 次/分钟 |

### 限流响应头

```
HTTP/1.1 429 Too Many Requests
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1700000060
Retry-After: 60

{
  "code": 10005,
  "msg": "rate limit exceeded",
  "error": {
    "type": "rate_limit_exceeded",
    "details": {
      "limit": 100,
      "window": "1m",
      "retry_after": 60
    }
  }
}
```

---

## OpenAPI 规范

### 规范文件位置

```
api/
├── openapi/
│   ├── campusos.yaml          # 主规范文件
│   ├── paths/
│   │   ├── auth.yaml          # 认证 API 路径
│   │   ├── users.yaml         # 用户 API 路径
│   │   ├── threads.yaml       # 帖子 API 路径
│   │   ├── posts.yaml         # 回复 API 路径
│   │   ├── categories.yaml    # 版块 API 路径
│   │   └── plugins.yaml       # 插件 API 路径
│   ├── components/
│   │   ├── schemas/            # 数据模型定义
│   │   ├── parameters/         # 公共参数
│   │   ├── responses/          # 公共响应
│   │   └── securitySchemes/    # 认证方案
│   └── tags.yaml              # API 分组标签
```

### OpenAPI 片段示例

```yaml
# paths/threads.yaml
/api/v1/threads:
  get:
    tags: [Threads]
    summary: 获取帖子列表
    operationId: listThreads
    security:
      - bearerAuth: []
      - apiKeyAuth: []
      - {}
    parameters:
      - name: page
        in: query
        schema:
          type: integer
          default: 1
          minimum: 1
      - name: page_size
        in: query
        schema:
          type: integer
          default: 20
          minimum: 1
          maximum: 100
      - name: category_id
        in: query
        schema:
          type: string
      - name: keyword
        in: query
        schema:
          type: string
      - name: sort_by
        in: query
        schema:
          type: string
          enum: [created_at, updated_at, reply_count, view_count]
          default: created_at
      - name: sort_order
        in: query
        schema:
          type: string
          enum: [asc, desc]
          default: desc
    responses:
      '200':
        description: 成功
        content:
          application/json:
            schema:
              allOf:
                - $ref: '#/components/schemas/Response'
                - type: object
                  properties:
                    data:
                      type: object
                      properties:
                        items:
                          type: array
                          items:
                            $ref: '#/components/schemas/ThreadSummary'
                        pagination:
                          $ref: '#/components/schemas/Pagination'

  post:
    tags: [Threads]
    summary: 创建帖子
    operationId: createThread
    security:
      - bearerAuth: []
    requestBody:
      required: true
      content:
        application/json:
          schema:
            $ref: '#/components/schemas/CreateThreadRequest'
    responses:
      '201':
        description: 创建成功
        content:
          application/json:
            schema:
              allOf:
                - $ref: '#/components/schemas/Response'
                - type: object
                  properties:
                    data:
                      $ref: '#/components/schemas/Thread'
      '400':
        $ref: '#/components/responses/BadRequest'
      '401':
        $ref: '#/components/responses/Unauthorized'
      '403':
        $ref: '#/components/responses/Forbidden'
```

---

## 核心 API 列表

### 认证模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| POST | `/api/v1/auth/register` | 用户注册 | ❌ |
| POST | `/api/v1/auth/login` | 用户登录 | ❌ |
| POST | `/api/v1/auth/logout` | 用户登出 | ✅ |
| POST | `/api/v1/auth/refresh` | 刷新 Token | ❌ |
| GET | `/api/v1/auth/me` | 获取当前用户 | ✅ |

### 用户模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| GET | `/api/v1/users` | 用户列表 | ✅ |
| GET | `/api/v1/users/{id}` | 用户详情 | 可选 |
| PUT | `/api/v1/users/{id}` | 更新用户 | ✅ |
| DELETE | `/api/v1/users/{id}` | 删除用户 | ✅ (admin) |
| POST | `/api/v1/users/{id}/suspend` | 封禁用户 | ✅ (admin) |
| POST | `/api/v1/users/{id}/unsuspend` | 解封用户 | ✅ (admin) |

### 帖子模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| GET | `/api/v1/threads` | 帖子列表 | 可选 |
| POST | `/api/v1/threads` | 创建帖子 | ✅ |
| GET | `/api/v1/threads/{id}` | 帖子详情 | 可选 |
| PUT | `/api/v1/threads/{id}` | 更新帖子 | ✅ |
| DELETE | `/api/v1/threads/{id}` | 删除帖子 | ✅ |
| POST | `/api/v1/threads/{id}/pin` | 置顶帖子 | ✅ (mod+) |
| DELETE | `/api/v1/threads/{id}/pin` | 取消置顶 | ✅ (mod+) |

### 回复模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| GET | `/api/v1/threads/{id}/posts` | 帖子回复列表 | 可选 |
| POST | `/api/v1/threads/{id}/posts` | 创建回复 | ✅ |
| PUT | `/api/v1/posts/{id}` | 更新回复 | ✅ |
| DELETE | `/api/v1/posts/{id}` | 删除回复 | ✅ |

### 版块模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| GET | `/api/v1/categories` | 版块列表 | 可选 |
| POST | `/api/v1/categories` | 创建版块 | ✅ (admin) |
| GET | `/api/v1/categories/{id}` | 版块详情 | 可选 |
| PUT | `/api/v1/categories/{id}` | 更新版块 | ✅ (admin) |
| DELETE | `/api/v1/categories/{id}` | 删除版块 | ✅ (admin) |

### 插件模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| GET | `/api/v1/plugins` | 插件列表 | ✅ (admin) |
| POST | `/api/v1/plugins` | 安装插件 | ✅ (admin) |
| GET | `/api/v1/plugins/{id}` | 插件详情 | ✅ (admin) |
| PUT | `/api/v1/plugins/{id}/config` | 更新配置 | ✅ (admin) |
| POST | `/api/v1/plugins/{id}/enable` | 启用插件 | ✅ (admin) |
| POST | `/api/v1/plugins/{id}/disable` | 禁用插件 | ✅ (admin) |
| DELETE | `/api/v1/plugins/{id}` | 卸载插件 | ✅ (admin) |

### 角色权限模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| GET | `/api/v1/roles` | 角色列表 | ✅ (admin) |
| POST | `/api/v1/roles` | 创建角色 | ✅ (admin) |
| PUT | `/api/v1/roles/{id}` | 更新角色 | ✅ (admin) |
| DELETE | `/api/v1/roles/{id}` | 删除角色 | ✅ (admin) |
| POST | `/api/v1/users/{id}/roles` | 分配角色 | ✅ (admin) |
| DELETE | `/api/v1/users/{id}/roles/{rid}` | 撤销角色 | ✅ (admin) |

### 文件模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| POST | `/api/v1/files/upload` | 上传文件 | ✅ |
| GET | `/api/v1/files/{key}` | 获取文件 | 可选 |
| DELETE | `/api/v1/files/{key}` | 删除文件 | ✅ |

### AI 模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| POST | `/api/v1/ai/chat` | AI 对话 | ✅ |
| GET | `/api/v1/ai/models` | 模型列表 | ✅ |

### 系统模块

| 方法 | 端点 | 说明 | 认证 |
|:---|:---|:---|:---|
| GET | `/api/v1/configurations` | 配置列表 | ✅ (admin) |
| GET | `/api/v1/configurations/{key}` | 获取配置 | ✅ (admin) |
| PUT | `/api/v1/configurations/{key}` | 更新配置 | ✅ (admin) |
| GET | `/api/v1/audit-logs` | 审计日志 | ✅ (admin) |
| GET | `/api/v1/health` | 健康检查 | ❌ |