# Host API v2 受管数据合同

`campusos.plugin/v2` 为外部插件提供两类受管数据能力：系统归属记录和经用户授权的个人数据。插件永远不会获得数据库连接、表名、CampusOS JWT、用户 Token 或任意宿主文件路径。

## Manifest 前提

```yaml
api_version: campusos.plugin/v2
host_api_version: v2
type: external
permissions:
  api:
    - resource: managed_data
      actions: [read, write, delete]
  user:
    - resource: plugin_search
      actions: [read]
      purpose: 允许 CampusOS 检索已声明的个人字段
      risk: medium
      revocable: true
```

`managed_data.collections` 中每个集合必须声明 `owner`、字段、可搜索字段、可过滤字段和可选限额。

## Host API 方法

| 方法 | 权限 | 适用范围 |
| --- | --- | --- |
| `RecordCreate` | `managed_data/write` | 已声明的 `owner: system` 集合 |
| `RecordGet`、`RecordList` | `managed_data/read` | 已声明的 `owner: system` 集合 |
| `RecordUpdate` | `managed_data/write` | 已声明的 `owner: system` 集合，必须带乐观锁 `version` |
| `RecordDelete` | `managed_data/delete` | 已声明的 `owner: system` 集合，必须带乐观锁 `version` |

请求中的插件名不可信。Host 根据受管 Runtime 身份和短期插件令牌确定插件命名空间。

## 用户归属数据

`owner: user` 集合只能由已登录用户访问：

```text
/api/v1/plugin-market/:plugin/records/:collection
/api/v1/plugin-market/:plugin/files
```

用户在插件中心看到 `permissions.user` 的用途、风险和可撤销标记后才会生成 Grant。Grant 绑定用户、插件版本和精确 `resource:action`；目录未发布、Grant 已撤销或插件版本变化时，普通读写立即被拒绝。用户仍可在插件下架后导出或删除自己已经保留的数据。

`GET /api/v1/plugin-market/search?plugin=<id>&collection=<name>&q=<keyword>` 提供首版声明式搜索。它同时要求 `managed_data:read` 与单独的 `plugin_search:read` 用户授权，只查询 Manifest 声明 `searchable` 字段的一个当前用户集合；不扫描 SQLite、任意文件、插件配置或其他用户/插件数据。

附件固定落在：

```text
data/personal-space/<user-id>/plugins/<plugin-name>/
```

后端同时检查 Manifest 声明、客户端 MIME、实际内容嗅探、后缀、单文件和总配额；声明为文本的二进制内容会被拒绝。API 仅返回文件 ID 和下载端点，不暴露物理路径。

## 用户与管理端治理 API

| 路由 | 作用 |
| --- | --- |
| `GET /api/v1/plugin-market/me/usage` | 返回当前用户按插件隔离的记录数、文件数、文件占用和搜索授权状态。 |
| `POST /api/v1/plugin-market/:name/request` | 提交本地插件 ID 的推荐/安装申请；不会从浏览器安装代码。 |
| `GET /api/v1/plugin-market/admin/audits` | 管理员查看目录、授权、数据与导入发布审计。 |

管理员成功导入 v2 外部插件后，宿主会立即同步 Catalog，并按实际包预检结果写入发布记录。`verified` 只能由该导入路径写入，管理表单不能伪造验签成功。

## 限制

- Host API v2 不接受用户 ID 参数，也不能访问 `owner: user` 集合。
- 字段、过滤和搜索必须先在 Manifest 声明；未声明字段不能作为查询条件。
- 更新和删除使用版本号，冲突返回 `409`。
- 用户 Grant 不是系统级 Host API 权限，也不扩大插件已有权限。

完整 JSON 权限清单由契约工具生成：`docs/api/plugin-permissions-v2.json`。
