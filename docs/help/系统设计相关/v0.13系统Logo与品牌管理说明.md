# v0.13 系统 Logo 与品牌管理说明

> 当前基线：`v0.13.0`  
> 更新时间：2026-08-03  
> 适用范围：Web 顶部品牌区、Admin“外观与风格包”、Appearance Built-in Feature

## 当前行为

系统默认 Logo 位于 `data/resources/branding/default-logo.png`。当前文件为透明背景横版 PNG，分辨率
`720 × 223`，约 `103 KB`；它由原始图片裁剪透明边界并压缩后纳入仓库。用户前台顶部通过
`GET /api/v1/home/logo` 加载，不再写死文字 Logo；资源不可用时仍显示 `CampusOS` 文本回退。

管理员进入 Admin `/appearance` 的“系统 Logo”操作台后可以预览、选择并替换 Logo，也可恢复仓库默认图。

| 方法 | 路径 | 权限 | 作用 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/home/logo` | 公开 | 返回当前 Logo 二进制，带 ETag 和短期缓存。 |
| `GET` | `/api/v1/home/config` | 公开 | `logo` 字段返回 URL、尺寸、大小、是否自定义和上传上限。 |
| `POST` | `/api/v1/home/logo` | 管理员 + `appearance.homepage.configure` | 上传并替换 Logo。 |
| `DELETE` | `/api/v1/home/logo` | 管理员 + `appearance.homepage.configure` | 删除当前自定义文件并恢复默认 Logo。 |

## 上传限制和保存位置

- 支持有效 PNG、JPEG，单文件最大 2 MB。
- 服务端会移除 JPEG 元数据、重新压缩 PNG/JPEG，并把最长边限制为 1024 px。
- 自定义文件写入 `data/config/branding/`，使用版本化文件名；配置成功后才清理上一个受管文件。
- `data/config/` 是本机可变数据并被 `.gitignore` 忽略，不能把管理员上传内容误提交到 Git。
- 备份和迁移实例时应把 `data/config/branding/` 与应用数据库/配置一起纳入备份；仓库默认 Logo 可由源码恢复。

格式、大小、资源缺失和目录不可写都会返回中文可操作错误，其中大小错误同时返回 `max_bytes` 和
`accepted_types`。客户端预检只改善体验，服务端校验始终是最终边界。

## 生效与 Docker

上传或恢复会更新 Appearance Homepage 配置版本，响应 URL 带版本查询参数，Web 刷新后即使用新资源。它是
运行时数据操作，不需要重启或重建 Docker。只有修改 `data/resources/branding/default-logo.png`、Go/Vue 源码、
Dockerfile 或挂载合同后，才按 Docker 开发指南等待热更新或执行相应 `up/rebuild`。

## 回归检查

1. 未上传时 Web 顶部和 Admin 预览都显示默认透明 Logo。
2. 上传 PNG/JPEG 后响应为自定义状态，刷新 Web 后显示新 Logo。
3. 超过 2 MB 或伪造格式时得到包含具体上限/格式的中文错误。
4. 恢复默认后自定义文件删除，URL 回到 `?v=default`。
5. 桌面和窄屏顶部均保持比例，不拉伸、不溢出导航。
