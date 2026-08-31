# 迭代计划：PostgreSQL 持久化 + JWT 认证 + 版块/回复系统

> 本次迭代将 CampusOS 从 Demo 级别升级为可持久化存储、具备真正认证能力的系统。

---

## 迭代进度

### 已完成 ✅

| 步骤 | 文件 | 说明 |
|:---|:---|:---|
| Docker Compose | `docker-compose.yml` | PostgreSQL 16 + Redis 7 + NATS 2 |
| 数据库迁移（建表） | `migrations/000001_init_schema.up.sql` | 11 张表：users, accounts, sessions, categories, threads, posts, tags, likes, audit_logs, notifications, configurations |
| 数据库迁移（回滚） | `migrations/000001_init_schema.down.sql` | DROP 所有表 |

### 待完成 📋

#### 第一步：基础设施

| 文件 | 说明 |
|:---|:---|
| `pkg/database/database.go` | PostgreSQL 连接池（pgx） |
| `pkg/snowflake/snowflake.go` | 雪花 ID 生成器 |
| `pkg/auth/jwt.go` | JWT Token 签发/验证 |
| `pkg/auth/password.go` | bcrypt 密码哈希 |
| 更新 `pkg/config/config.go` | 添加 JWT Secret、Database DSN 配置 |

#### 第二步：PostgreSQL Repository 实现

| 文件 | 说明 |
|:---|:---|
| `internal/core/identity/repository/pg_user_repository.go` | PostgreSQL 版 UserRepository |
| `internal/core/identity/repository/pg_account_repository.go` | PostgreSQL 版 AccountRepository |
| `internal/community/repository/pg_thread_repository.go` | PostgreSQL 版 ThreadRepository |
| `internal/community/repository/pg_post_repository.go` | PostgreSQL 版 PostRepository（新增） |
| `internal/community/repository/pg_category_repository.go` | PostgreSQL 版 CategoryRepository（新增） |

#### 第三步：新增领域模型

| 文件 | 说明 |
|:---|:---|
| `internal/community/domain/post.go` | Post（回复）领域实体 |
| `internal/community/domain/category.go` | Category（版块）领域实体 |
| 更新 `internal/core/identity/domain/user.go` | 添加 Account 实体 |

#### 第四步：新增 Service 和 Handler

| 文件 | 说明 |
|:---|:---|
| `internal/community/service/post_service.go` | 回复服务 |
| `internal/community/service/category_service.go` | 版块服务 |
| `internal/community/handler/post_handler.go` | 回复处理器 |
| `internal/community/handler/category_handler.go` | 版块处理器 |
| 更新 `internal/core/identity/service/user_service.go` | 使用 bcrypt + JWT |
| 更新 `internal/core/identity/handler/user_handler.go` | JWT 认证中间件 |

#### 第五步：更新 Server 和中间件

| 文件 | 说明 |
|:---|:---|
| `pkg/middleware/auth.go` | JWT 认证中间件（新文件） |
| 更新 `internal/server/server.go` | 依赖注入 PostgreSQL Repository，注册新路由 |

#### 第六步：Makefile 和迁移工具

| 文件 | 说明 |
|:---|:---|
| `Makefile` | build / dev / migrate / test 等命令 |
| `cmd/migrate/main.go` | 数据库迁移入口 |

---

## 开发顺序

```
1. pkg/database/database.go      ← 连接数据库
2. pkg/snowflake/snowflake.go    ← ID 生成器
3. pkg/auth/jwt.go               ← JWT 工具
4. pkg/auth/password.go          ← 密码哈希
5. 更新 pkg/config/config.go     ← 配置更新
6. 新增领域模型（post.go, category.go）
7. 新增 PostgreSQL Repository
8. 更新 Service 层（JWT 认证）
9. 新增 Handler 和路由
10. 更新 server.go（依赖注入）
11. Makefile + 迁移工具
12. 编译运行验证
```

---

## 启动开发环境

```bash
# 启动 PostgreSQL + Redis + NATS
cd ~/bbs/bbs01/CampusOS
docker compose up -d

# 执行数据库迁移
PGPASSWORD=campusos_dev psql -h localhost -U campusos -d campusos -f migrations/000001_init_schema.up.sql

# 启动服务
go run ./cmd/server/main.go
```

## 验证测试

```bash
# 1. 健康检查
curl -s http://localhost:8080/api/v1/health | jq .

# 2. 注册（会写入 PostgreSQL）
curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"jack","nickname":"Jack","email":"jack@example.com","password":"123456"}' | jq .

# 3. 登录（返回真实 JWT Token）
curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"jack@example.com","password":"123456"}' | jq .

# 4. 创建版块
curl -s -X POST http://localhost:8080/api/v1/categories \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"name":"技术分享","slug":"tech","description":"技术讨论区"}' | jq .

# 5. 发帖（持久化到 PostgreSQL）
curl -s -X POST http://localhost:8080/api/v1/threads \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"title":"Hello CampusOS","content":"第一篇帖子","category_id":"<cat_id>"}' | jq .

# 6. 回复帖子
curl -s -X POST http://localhost:8080/api/v1/threads/<thread_id>/posts \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"content":"这是一条回复"}' | jq .