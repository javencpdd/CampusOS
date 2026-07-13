# Manifest 与配置

## plugin.yaml

每个插件目录必须在根部包含 `plugin.yaml`。

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 插件唯一名称。 |
| `version` | 是 | 建议使用语义化版本。 |
| `runtime` | 是 | `builtin`、`wasm` 或 `grpc`。 |
| `scope` | 建议 | `system` 或 `user`。第三方默认使用 `user`。 |
| `lifecycle` | 否 | 后端 `restart/plugin-restart/hot` 与前端 `hot`；缺省值按 Runtime 推导。 |
| `ui` | 否 | `campusos.ui/v1` Route、Navigation、Slot、Surface 和 Action。 |
| `display_name` | 否 | 管理端显示名称。 |
| `description` | 否 | 插件用途。 |
| `events.subscribe` | 否 | 订阅事件列表。 |
| `permissions.api` | 否 | Host API 权限。 |
| `storage` | 建议 | `none`、`sqlite` 或插件约定的存储声明。 |
| `config` | 视 Runtime | 当前配置。 |
| `config_schema` | 否 | 管理端配置表单和后端归一化规则。 |

v2 外部插件还可声明 `api_version`、`host_api_version`、`type`、`managed_data`、`files`、`permissions.user` 和 `release`。受管数据示例和字段规则见 [插件中心、受管数据与签名](/plugins/market-managed-data)。工具可使用仓库中的 [`plugin-manifest-v2.schema.json`](../../docs/api/plugin-manifest-v2.schema.json) 进行结构预检；提交导入时仍必须通过 CampusOS 服务端的完整 Manifest 校验。

## Runtime 配置

Wasm：

```yaml
runtime: wasm
config:
  module: plugin.wasm
  entrypoint: handle_event
  event_timeout_ms: 1000
```

`module` 必须是插件目录内的相对路径，不能使用绝对路径或 `../`。

gRPC：

```yaml
runtime: grpc
config:
  command: ./plugin
  event_timeout_ms: 1000
```

Built-in：

```yaml
runtime: builtin
scope: system
config:
  enabled_feature: true
```

Built-in 的配置是否热更新由具体内置服务决定；插件整体启停由 `lifecycle.backend.activation_mode` 决定，不由 scope 单独决定。

## 生命周期与默认 UI

```yaml
lifecycle:
  backend:
    activation_mode: plugin-restart
  frontend:
    activation_mode: hot
```

新插件应提供默认可用 UI。第三方默认使用声明式 schema；复杂可信模块必须使用 Core 编译期白名单。完整格式见 [前端运行时与 Gateway](./frontend-runtime.md)。

## 权限

```yaml
permissions:
  api:
    - resource: thread
      actions: [read]
    - resource: log
      actions: [write]
```

权限由 `resource:action` 表达。Manifest 声明只是申请，不能代替后端检查。

## config_schema

```yaml
config:
  mode: compact
  enabled: true
  limit: 20

config_schema:
  fields:
    - key: mode
      label: "显示模式"
      type: select
      default: compact
      options:
        - label: "紧凑"
          value: compact
        - label: "完整"
          value: full
    - key: enabled
      label: "启用功能"
      type: boolean
      default: true
    - key: limit
      label: "数量上限"
      type: number
      default: 20
```

支持类型：

```text
string
text
number
boolean
select
json
```

规则：

- `key` 不能为空或重复。
- `select` 必须提供 `options`。
- 保存时按 schema 归一化类型。
- schema 未声明字段不会由 Admin 普通表单写入。
- 缺失字段优先保留当前值，再使用默认值。

## 配置更新接口

```http
GET /api/v1/plugins/:name
PUT /api/v1/plugins/:name/config
```

接口只对管理员开放。配置中不要保存明文生产密钥；敏感凭据应使用独立 Secret 管理。

## 版本变更

修改插件包内容后应更新 `version`。覆盖导入会比较版本和 checksum，但当前仍需要管理员判断变更是否兼容。

页面风格包使用独立的 `style.yaml`，其 `target`、CSS 根作用域、`effect` 和 `capabilities` 不属于普通 `plugin.yaml` Host API 权限。参见 [风格包、特效与 CampusStyleSDK](/plugins/style-packs)。
