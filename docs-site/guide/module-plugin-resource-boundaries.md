# 模块、插件与资源包的边界

> 更新时间：2026-10-26 07:03（Asia/Shanghai）

| 类型 | 当前位置 | 生命周期与权限边界 | 例子 |
| --- | --- | --- | --- |
| Core Module | `internal/modules/core/`，描述符在 `modules/core/` | 随 API 编译，拥有其身份、权限或存储事实 | 身份、社区、User Storage。 |
| Built-in Feature | `internal/modules/features/`，描述符在 `modules/features/` | 随宿主交付，可受控启停；不是可安装的外部包 | 文档、课表、外观、内置 AI/Knowledge。 |
| v5 External Plugin | `plugins/<示例源码目录>/plugin.json`；导入后包位于 `PLUGIN_DATA_DIR/v5/packages/` | 独立签名包、全局/User 安装、当前 Grant/Consent、配置与 Runtime 分别治理 | PDF Viewer、独立 SDK 作者包。 |
| Resource Package | `data/resources/{themes,homepage-packs,space-style-packs,...}/<id>/` | 使用 `resource.json` 与目录摘要；不携带插件业务 Runtime 或新增宿主权限 | 主题、主页/空间样式、Skill/Prompt 资源。 |
| 内置静态资源 | `data/resources/branding/default-logo.png` | `branding` 是独立静态组，只允许此普通 PNG；不作为 Resource Package 安装 | 默认站点 Logo。 |

## 外部插件与受管数据

当前包 wire 是 `campusos.plugin/v5`。源码、前端入口、Schema 和产物按 Manifest 交付；`plugins/` 只是作者源码目录，不会因目录存在就自动发现、安装或执行。包验签也不替代安装、管理员用途审批、需要时的本人同意或实际运行隔离。

导入、安装、Grant/Consent、Operation 及配置事实保存在宿主 PostgreSQL 表中；v5 文件使用私有 `PLUGIN_DATA_DIR/v5/` 命名空间。插件不能修改 `web/src/modules`、`admin/src/modules`、宿主源码/依赖/构建物或核心 Schema，不能自行创建平台 migration，也不能把任意 SQLite、宿主文件路径或数据库连接当成已授权能力。具体调用仅使用当前已发布的受控 Host/SDK 合同。

用户“添加插件”产生的是本域安装、配置和同意事实，不会把个人空间目录的写权限交给 iframe。全局与各 User 数据由宿主按 `system` / `user-<id>` 隔离；当前配置不采用旧 `data/personal-space/<id>/plugins/<key>/config/user.json` 文件布局。目录和备份集合见[数据目录](/reference/data-layout)。

## 外观资源与 Logo

资源包仍须通过类型、入口路径、校验摘要及禁止 Runtime 标记的检查。`branding` 不走这些资源包入口：当前仅允许 `default-logo.png`，拒绝额外文件、子目录和符号链接；Logo 大小与尺寸受现有图像边界限制。

管理员上传后的站点 Logo 位于宿主管理的 `data/config/branding/`，与内置默认资源分开。该目录由外观服务生成和更新，调用方不能指定任意本机路径；它属于普通本地配置备份范围。

## 明确保留的历史兼容范围

`plugins/.installed/`、`.staging/`、旧 YAML/v4 源码布局及旧个人空间插件配置仅保留为历史记录、兼容读取或演练材料，不是当前 v5 的安装和执行入口。旧 `data/plugins/` 内容也不会自动获得 v5 信任。`data/plugin_data/` 的历史子目录与当前 **`v5/` 受管命名空间**必须区分，不能将整个目录笼统当成废弃数据或忽略备份。

当前入口见[v5 Manifest 与包校验](/plugins/manifest)和[PDF Viewer 教程](/plugins/pdf-viewer-tutorial)。旧协议说明不扩展当前 Runtime、私有资源或恢复授权。
