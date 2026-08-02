# v13 WebUI 交互、通知与个人空间治理说明

> 当前基线：`v0.13.0`  
> 更新时间：2026-08-02  
> 适用范围：Web 用户端、Admin 用户管理、Community Core、User Storage Core、Docker 开发日志

## 1. 本轮行为基线

| 场景 | 当前合同 |
| --- | --- |
| 点击一个分组查看帖子 | 返回该活动分组下全部活动子板块的公开帖子；空分组返回空列表。 |
| 填写二手价格 | Web 允许两位小数，以“分”的整数写入 `price_minor`。 |
| 用户个人空间 | 默认 50 MB；管理员可在用户管理中为单个用户授权 1 MB–100 GB。 |
| 上传图片 | JPEG/PNG 自动重编码和缩放，配额按优化后的实际字节数计算。 |
| 帖子和评论事件 | 管理员下架、主题被回复、评论被回复都会生成站内通知，并避免自我/重复通知。 |
| 历史头像 | 主页设置显示最近保留的头像，可直接切换；切换不改变 FIFO 顺序。 |
| Docker 平台日志 | API、Web、Admin、Docs stdout 同时写入 `.campusos/logs/`，管理端可以实时 follow。 |
| 首次进入模块 | 核心路由和可信模块提前加载，重复 Runtime Manifest 不再拆卸并重建相同路由。 |

## 2. 分组帖子与二手价格

`GET /api/v1/threads?category_id=<group_id>` 会先读取分类事实。如果 ID 指向 group，Thread Service 将其
展开为所有活动子 board 的 ID 集合，再交给 Memory/PostgreSQL Repository 统一过滤。非空 `CategoryIDs`
不会进入无筛选公开列表缓存；空集合显式匹配零行，避免空分组错误显示全站帖子。

Web 桌面导航在 group 下增加“分组名 · 全部帖子”，移动导航也提供同一入口。单个 board 的行为不变。

二手 UI 使用 `precision=2`、`step=0.01`。提交前执行 `Math.round(priceYuan * 100)`，例如 `12.34` 元提交为
`1234` 分。后端和 PostgreSQL 继续只保存非负 BIGINT 分价，避免金额持久化引入浮点误差。

## 3. 50 MB 配额、管理员授权与图片优化

User Storage Core 默认值为 `50 * 1024 * 1024` 字节。新增表：

```text
user_storage_quotas(user_id, quota_bytes, updated_by, created_at, updated_at)
```

不存在用户记录时使用系统默认值；存在记录时使用单独授权。Admin“用户管理 → 空间”会读取使用量、当前配额、
剩余容量和授权类型，并调用：

```text
GET /api/v1/spaces/admin/users/:user_id/storage
PUT /api/v1/spaces/admin/users/:user_id/storage
```

更新立即在 API 进程内生效，不需要重启 Docker。把配额调到已用容量以下不会删除文件，只会拒绝后续写入。

图片优化规则：

- JPEG：去除元数据，以质量 82 重编码；
- PNG：使用最高无损压缩；
- JPEG/PNG 长边超过 1920 像素时等比缩小；
- GIF/WebP：保留原始字节，避免破坏动画或重复有损编码；
- 解码像素超过安全上限时拒绝处理；头像、Community 内容图片和富文本图片均在优化后检查单图限制和用户配额。

更改 `modules/features/personal-space/module.yaml` 的系统默认值属于 Feature 配置/代码变更，需要重新加载服务；
使用 Admin 为单个用户授权不需要重启。

## 4. 头像历史与 FIFO

主页设置加载自身资料、空间状态和 `GET /api/v1/spaces/me/avatars`，展示最多三个保留源文件及当前选中项。
选择历史头像调用：

```http
PUT /api/v1/spaces/me/avatar
Content-Type: application/json

{"file_name":"<retained-file-name>"}
```

后端验证文件名和用户目录归属后，只更新 `user_spaces.avatar`。它不重命名文件、不改 mtime、不改变上传顺序。
只有 `POST /api/v1/spaces/me/avatar` 新上传成功时，才按上传时间删除最老源文件，然后把新文件设为当前头像。

## 5. 站内通知

新增/补全的通知类型：

| 类型 | 接收者 | 跳转 |
| --- | --- | --- |
| `community.thread.taken_down` | 被管理员下架帖子的作者 | 帖子详情 |
| `community.thread.replied` | 被其他用户回复的主题作者 | 新回复锚点 |
| `community.post.replied` | 被其他用户回复的评论作者 | 新回复锚点 |

嵌套回复中，如果评论作者与主题作者不同，两者都收到对应通知；如果是同一人，只写一条评论回复通知。作者回复
自己主题、用户回复自己的评论都不会生成自我通知。帖子、回复计数、通知和 Outbox 使用可靠事务；要求写入通知
但通知仓库失败时，帖子回复或下架命令会回滚，不留下“内容成功但消息丢失”的半状态。

## 6. Docker 平台日志

问题原因不是管理页面不支持 Docker，而是平台日志服务只 follow `.campusos/logs/*.log`，旧 Docker 开发入口
只把输出写到容器 stdout。当前开发入口使用 `tee -a` 同时保留两条输出路径：

```text
容器 stdout/stderr -> docker compose logs
                   -> .campusos/logs/api|web|admin|docs.log -> Admin 平台日志 SSE
```

修改入口脚本后需要重建并重新创建容器：

```powershell
.\scripts\docker-dev.ps1 up
```

Linux 使用 `./scripts/docker-dev.sh up`。管理页显示的是固定四个开发日志源，不是集中式生产日志系统；数据库、
Redis、NATS 的容器日志仍通过 `docker-dev.* logs <service>` 查看。

## 7. 首次点击模块看起来“刷新”

Vue Router 页面采用动态 `import()`，第一次访问会下载该模块 chunk；登录又会触发一次用户作用域 UI Manifest
同步。旧实现即使 Manifest 完全相同也会删除并重新注册动态路由，因此首访加载和路由重建叠加时看起来像页面
被动刷新，但没有发现业务代码主动调用 `location.reload()`。

当前处理：

1. App Shell 挂载后预加载核心社区、互助、二手、个人空间和账号页面 chunk；
2. Manifest 到达后预加载其中声明的可信模块；
3. 同一 Manifest 指纹重复到达时直接复用 Registry Snapshot；
4. 并发同步串行合并，避免登录切换时重复拆装路由；
5. Runtime 路径与已有静态核心路径相同时不再注册重复路由。

仍保留代码分包，因此首次打开应用会在后台产生网络请求，但后续模块点击应保持 SPA 客户端导航，不发生整页
白屏或浏览器刷新。若仍出现真实刷新，在浏览器 Network 中检查 `document` 类型请求和 401；API 客户端仅在
访问令牌无法恢复时会跳转登录页。

## 8. 部署与回归检查

| 修改 | 是否重启/重建 |
| --- | --- |
| Admin 调整单用户配额 | 不需要；立即生效。 |
| 上传/切换头像 | 不需要。 |
| 普通 Web/Go 源码 | Docker 开发栈由 Vite HMR/API 轮询重建处理。 |
| `compose.dev.yml`、`deploy/docker/dev-*.sh` | 需要执行 `docker-dev.* up` 重建/重建容器。 |
| 新 migration `000042` | API 开发容器重建时自动执行；生产必须按迁移流程备份、up、检查。 |

建议回归：分组含多个 board/空 group、`0.01` 和 `12.34` 元、管理员扩/缩容、超大 JPEG/PNG、四次头像上传并
切换最老头像、三类通知的自我/重复接收者、Docker 平台日志 follow、登录后首次进入发帖和二手模块。
