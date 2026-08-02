# 数据目录

## 当前结构

```text
modules/                              编译期模块描述符，不是运行数据
data/
├── plugins/<plugin>/                 External Plugin 实现和 plugin.yaml
├── plugin_data/<plugin>/             External Plugin 私有运行数据与版本快照
├── module_data/<feature>/            Built-in Feature 本地数据
├── resources/
│   ├── themes/
│   ├── homepage-packs/
│   ├── space-style-packs/
│   ├── skills/
│   ├── prompts/
│   ├── personas/
│   └── knowledge-metadata/
├── personal-space/<user_id>/         User Storage Core 管理的用户文件
├── images/                            无用户归属的全局图片
├── config/                            本地配置预留
├── dist/                              本地发布产物预留
└── skills/                            旧本地 Skill 导入预留
```

## External Plugin

`data/plugins/<plugin>/` 只保存实现和随代码部署的静态输入：

- `plugin.yaml`；
- `plugin.wasm`，或受管进程的可执行入口；
- README 和运行所需静态文件。

`data/plugin_data/<plugin>/` 只保存该外部插件的 KV、缓存、版本快照和可恢复
运行状态。插件代码包不会自动携带这些数据，备份和迁移必须分别处理。

内置课表、个人空间、富文本、Appearance 和 Moderation 不允许出现在这两个
目录中。

## Built-in Feature 数据

模块描述符在 `modules/`，实现代码在 `internal/modules/`。Built-in Feature
需要本地可变数据时使用 `data/module_data/<feature>/`。当前个人主页内置 JSON
风格位于：

```text
data/module_data/personal-space/styles/
```

功能停用不会删除这里的数据。

## 用户个人空间

所有用户文件通过 User Storage Core 写入：

```text
data/personal-space/<user_id>/
├── img/avatars/                       头像源文件，默认保留最近 3 个
├── img/richtext/                      富文本图片
├── file/schedule/                     学期课表索引和 JSON
├── plugins/<plugin>/                  获得用户授权的插件附件
├── excel/
├── word/
└── pdf/
```

默认配额为 50 MB；管理员可以在用户管理中写入 `user_storage_quotas`，按用户覆盖默认值且立即生效。
头像、Community 内容图片和富文本图片共享该配额。JPEG/PNG 会去元数据、重编码，并在长边超过 1920
像素时等比缩小，配额按优化后的文件大小计算；GIF/WebP 为保留动画或避免重复有损编码而原样保存。

头像目录默认按上传时间保留最近 3 个源文件。用户切换到历史头像只修改 `user_spaces.avatar`，不会修改
源文件时间或 FIFO 顺序；只有上传新头像才会删除最早的源文件。课表和富文本只依赖 User Storage Port，
不依赖个人主页功能是否启用。备份必须同时包含 PostgreSQL 和 `data/personal-space/`。

## Resource Package

主题和风格包统一保存在：

```text
data/resources/themes/<id>/
data/resources/homepage-packs/<id>/
data/resources/space-style-packs/<id>/
```

每个目录必须有 `resource.json`，并且通常包含 `style.yaml`、README、预览、
模板、图片、CSS、配置 schema 和可选受限特效。`resource.json` 声明稳定 ID、
类型、版本、兼容范围、入口、来源和 checksum。

```bash
go run ./cmd/campusosctl resource inspect data/resources/themes/campus-canvas
```

Resource Package 不能包含 `plugin.yaml`、Go/Cargo Runtime、migration 或启动
脚本。它没有业务进程生命周期，也不能取得数据库、JWT、用户 Token 或任意
文件系统权限。

## 从旧布局迁移

旧版把 Built-in 描述符和风格包放在 `data/plugins`、`data/plugin_data`。v10
提供可回滚迁移：

```bash
./scripts/migrate-v10-module-plugin-layout.sh check
./scripts/migrate-v10-module-plugin-layout.sh apply backups/v10-layout-before
./scripts/migrate-v10-module-plugin-layout.sh rollback backups/v10-layout-before
```

迁移不覆盖同名目标；每个移动写入状态文件；旧风格包迁入后必须生成并通过
Resource Manifest 校验。

## 备份边界

`scripts/backup.sh` 的 v2 格式同时包含：PostgreSQL、`modules/`、外部插件、
插件数据、模块数据、资源包和用户文件。恢复工具继续接受旧 v1 备份。

```bash
make backup
make restore-drill
```

只备份数据库会丢失文件；只备份 `data/` 会丢失账号、帖子、授权和状态。
