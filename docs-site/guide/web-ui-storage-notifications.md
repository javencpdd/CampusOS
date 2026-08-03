# Web 交互、个人空间与站内通知

本页说明 v13 用户端和管理端的当前行为。接口字段以生成的 OpenAPI 为准。

## 社区列表与价格

- 选择 group 的“全部帖子”会聚合它的全部活动子 board；空 group 返回空列表。
- 选择 board 仍只读取该 board。
- 讨论、互助和二手详情统一显示所属板块与标签；板块名称可点击回到该板块的帖子列表。
- 二手价格输入允许两位小数，客户端把元转换为整数分；`12.34` 元对应 `price_minor=1234`。

PostgreSQL 聚合查询将展开后的板块字符串 ID 校验并转换为 `bigint[]`，与 `threads.category_id BIGINT`
同型比较。若旧进程日志出现 `operator does not exist: bigint = text`，说明仍在运行修复前的 API；Docker
开发模式会自动重编译 Go 源码，健康恢复后重新打开分组即可。

## 个人空间

每位用户默认有 50 MB 空间，头像、Community 内容图片、富文本图片和课表共享使用量。管理员可在 Admin
“用户管理 → 空间”中给单个用户设置 1 MB–100 GB 配额，授权立即生效，不需要重启容器。调低配额不会删除
已有文件，但在使用量重新低于配额前不能继续写入。

JPEG/PNG 会在服务端去元数据、重编码，并将超过 1920 像素的长边等比缩小；GIF/WebP 为保护动画和避免
重复有损编码而原样保存。配额按最终落盘大小计算。

个人主页设置展示最近三个头像源文件。选择历史头像不会改变它们的顺序；只有新上传才按 FIFO 删除最老文件。

空间状态包含 `max_avatar_bytes`，页面会显示服务端当前的单头像上限。默认允许 PNG、JPEG、GIF、WebP，单文件
最大 2 MB。文件过大、格式不支持和总空间不足时，前后端都使用中文说明具体上限和下一步，例如压缩/裁剪图片、
删除不用的文件或联系管理员提高配额；错误详情还包含机器可读的 `max_bytes` 与 `accepted_types`。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/spaces/me/storage` | 当前用户空间状态。 |
| `GET` | `/spaces/me/avatars` | 保留头像和当前选择。 |
| `POST` | `/spaces/me/avatar` | 上传新头像并执行 FIFO。 |
| `PUT` | `/spaces/me/avatar` | 选择已保留头像。 |
| `GET/PUT` | `/spaces/admin/users/:user_id/storage` | 管理员读取/授权用户配额。 |

## 系统 Logo

用户前台顶部 Logo 由 Appearance Feature 提供。管理员在 Admin“外观与风格包 → 系统 Logo”上传 PNG/JPEG
或恢复默认图；单文件最大 2 MB，服务端会压缩并把最长边限制为 1024 px。操作立即生效，不需要重启 Docker，
用户前台刷新后会读取带新版本参数的 `/home/logo`。

仓库默认图位于 `data/resources/branding/default-logo.png`；管理员上传的本地可变文件位于
`data/config/branding/`，部署备份时必须包含后者。详细维护合同见仓库 Help 的系统 Logo 与品牌管理说明。

## 后台用户邮箱

Admin“用户管理”通过 `GET /admin/users` 读取包含邮箱的管理投影。该接口需要管理员准入和
`identity.user.read` 权限；未绑定个人邮箱或仅有历史共享占位邮箱的用户显示“未绑定邮箱”。公开
`GET /users` 与 `GET /users/:id` 仍不返回邮箱，前台用户不能据此枚举其他用户的登录地址。

## 通知接收规则

管理员下架帖子时通知作者；其他用户回复主题时通知主题作者；回复评论时通知评论作者，并在接收者不同的情况下
通知主题作者。自我回复和相同接收者不会生成重复通知。通知跳转到帖子或新回复锚点。

这些通知是可靠写入的一部分：通知存储失败时，对应下架或回复命令回滚。用户端铃铛显示站内通知，它不是邮件、
浏览器 Push 或短信。

## 首次进入模块

Web 继续使用按路由分包，但 App Shell 会后台预加载核心页面，UI Runtime 也会预加载可信模块。相同 Manifest
不会重复删除/注册路由，并发同步会合并串行执行。因此第一次点击发帖、互助、二手或个人空间时不应再出现类似
整页刷新的白屏；后台仍可能看到正常的 JavaScript chunk 请求。

Docker 开发模式的实时平台日志和重建方法见 [Docker 跨平台开发](/deployment/docker-development)。数据目录、
配额和备份边界见 [数据目录](/reference/data-layout)。
