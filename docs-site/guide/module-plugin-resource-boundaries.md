# 模块、插件与资源包的边界

> 更新时间：2026-09-23

| 类型 | 位置 | 生命周期 | 例子 |
| --- | --- | --- | --- |
| Core Module | `internal/modules/core/` | 随 API 编译，始终受核心治理 | 身份、社区、用户存储。 |
| Built-in Feature | `internal/modules/features/` | 随主程序交付，不可作为包安装 | 富文本、个人文档、课表、外观。 |
| v4 External Plugin | `plugins/<key>/` → `.installed/` | 独立校验、安装、发布、启停和授权 | `campusos.pdf-viewer`。 |
| Resource Package | `data/resources/` | 无业务 Runtime 的资源导入/应用 | 主题、主页风格、Skill/Prompt 资源。 |

外部插件不能把页面组件塞回 `web/src/modules` 或 `admin/src/modules`。核心应用只提供插件中心、管理入口、宿主 Surface 和 Bridge；具体用户/Admin 页面、Manifest、Schema 和构建产物均应随插件目录交付。

`runtime: builtin`、`data/plugins` 和 `data/plugin_data` 仅服务历史兼容，不能作为新插件入口。插件也不能自行创建平台数据库迁移；需要持久数据时使用已有平台通用能力，或使用受插件目录约束的 SQLite/配置文件。

用户点击“添加”已发布插件后，宿主创建用户级配置目录，不把个人空间写权限交给 iframe。详见[数据目录](/reference/data-layout)和[PDF Viewer 教程](/plugins/pdf-viewer-tutorial)。
