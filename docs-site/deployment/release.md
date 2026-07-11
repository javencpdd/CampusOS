# 构建与发布

## 当前交付边界

CampusOS 当前具备开发环境、前端生产构建和后端二进制构建能力，但尚未形成经过完整验收的生产级容器镜像与高可用部署方案。以下流程适合测试环境和发布预检，公网部署还需要 TLS、密钥管理、备份、监控和安全加固。

## 构建后端

```bash
make build
```

产物默认位于：

```text
bin/campusos-server
```

运行前必须提供数据库、Redis、NATS 和必要环境变量。

## 构建前端

```bash
cd web && pnpm build
cd ../admin && pnpm build
cd ../docs-site && pnpm build
```

产物：

| 项目 | 默认目录 |
| --- | --- |
| Web | `web/dist/` |
| Admin | `admin/dist/` |
| Docs | `docs-site/.vitepress/dist/` |

这些目录可以交给 Nginx、Caddy 或对象存储静态托管。Web 和 Admin 的 `/api` 请求需要反向代理到 CampusOS API；Docs 是纯静态站点，不依赖 API。

## 发布前 migration

```bash
make migrate-status
make migrate-up
make migrate-status
```

执行 migration 前应备份 PostgreSQL 和 `data/`。不要直接修改已经在其他环境应用的历史 migration；新增变更使用新的递增版本。

## 反向代理边界

建议使用不同域名或明确路径：

```text
community.example.edu   -> web/dist
admin.example.edu       -> admin/dist
docs.example.edu        -> docs-site/.vitepress/dist
api.example.edu         -> campusos-server:8080
```

Admin 构建时将 `VITE_DOCS_URL` 指向公开文档地址。文档站独立后无需改动 API 或数据库。

## 发布检查清单

1. Go 全量测试通过。
2. Web、Admin、Docs 构建通过。
3. migration 状态与仓库一致。
4. 默认管理员密码已修改。
5. 数据库、`data/` 和配置备份已验证可恢复。
6. API 只通过 TLS 暴露，CORS 和可信代理配置明确。
7. 插件包来源、权限和 checksum 已人工复核。
8. Admin、平台日志和 pgAdmin 不向非管理员开放。

## 回滚原则

代码回滚不等于数据回滚。涉及 migration 或插件数据格式时，需要在发布前写明：

- 兼容的旧版本范围。
- 数据回填是否可逆。
- down migration 的风险。
- `data/` 文件是否需要同步恢复。

优先采用向前修复和兼容读取，避免在没有备份验证的情况下直接回滚结构性 migration。
