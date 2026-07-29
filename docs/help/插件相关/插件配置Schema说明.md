# CampusOS 插件配置 Schema 说明

> 初始日期：2026-07-07
> 当前基线：`v0.13.0`；本文只说明仍兼容的 `config_schema`。新插件还应遵守 Manifest v2、
> Host API v2 和当前插件教程。

## 1. 设计目标

插件可以在 `plugin.yaml` 中声明 `config_schema`，用于描述哪些配置项可以被后台、CLI 或测试工具展示和编辑。v0.5.10 起，Admin 插件管理页已经可以按该 schema 渲染配置表单并写回插件配置。

`config_schema` 不直接替代 `config`，两者关系如下：

| 字段 | 作用 |
| --- | --- |
| `config` | 插件当前运行配置值 |
| `config_schema` | 配置表单和校验提示的结构描述 |

## 2. Manifest 示例

```yaml
config:
  module: "plugin.wasm"
  entrypoint: "handle_event"
  event_timeout_ms: 1000

config_schema:
  fields:
    - key: "entrypoint"
      label: "Entrypoint"
      type: "string"
      description: "导出的 Wasm 事件处理函数名"
      required: true
      default: "handle_event"
    - key: "event_timeout_ms"
      label: "Event timeout"
      type: "number"
      description: "单次事件处理超时时间，单位毫秒"
      required: true
      default: 1000
```

## 3. 字段说明

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `key` | 是 | 对应 `config` 中的配置 key，必须唯一 |
| `label` | 否 | 面向用户展示的名称 |
| `type` | 否 | 配置项类型，未填时默认为 `string` |
| `description` | 否 | 配置项说明 |
| `required` | 否 | 是否必填 |
| `default` | 否 | 默认值 |
| `options` | `select` 类型必填 | 可选项列表 |

当前支持的字段类型：

```text
string
text
number
boolean
select
json
```

## 4. Select 示例

```yaml
config_schema:
  fields:
    - key: "layout"
      label: "Layout"
      type: "select"
      default: "grid"
      options:
        - label: "Grid"
          value: "grid"
        - label: "List"
          value: "list"
```

`select` 类型必须提供 `options`，否则 manifest 校验会失败。

## 5. CLI 与 Admin 行为

`campusosctl plugin init` 会为新插件生成最小 `config_schema`：

```bash
go run ./cmd/campusosctl plugin init my-plugin --runtime wasm
go run ./cmd/campusosctl plugin init my-grpc-plugin --runtime grpc
```

Wasm 插件模板默认暴露 `entrypoint` 和 `event_timeout_ms`；gRPC（受管进程兼容名称）模板默认暴露 `command` 和 `event_timeout_ms`。CLI 不再生成 `runtime: builtin`；编译期能力应使用 `campusos.module/v1` 描述符并随主程序构建。

`campusosctl plugin inspect` 会输出 `config_schema`：

```bash
go run ./cmd/campusosctl plugin inspect data/plugins/hello-wasm
```

`campusosctl plugin pack` 会在打包前解析并校验 manifest：

```bash
go run ./cmd/campusosctl plugin pack data/plugins/hello-wasm
```

当前校验规则：

| 规则 | 结果 |
| --- | --- |
| `key` 为空 | 拒绝 |
| `key` 重复 | 拒绝 |
| `type` 不在支持列表内 | 拒绝 |
| `select` 没有 `options` | 拒绝 |

当前 CLI 校验只负责 schema 结构合法性，还不负责把 `default` 与 `type` 做强一致校验，也不会直接修改插件运行配置。

Admin 插件管理页会在插件详情接口中读取：

```text
GET /api/v1/plugins/:name
```

响应中的 `config` 和 `config_schema` 会用于渲染配置弹窗。保存时调用：

```text
PUT /api/v1/plugins/:name/config
```

后端会按 `config_schema.fields` 对提交值做基础类型归一化：

| 类型 | 归一化行为 |
| --- | --- |
| `boolean` | 接收布尔值或可解析布尔字符串 |
| `number` | 接收数值或可解析数字字符串 |
| `select` | 必须匹配 `options` 中的值 |
| `string` / `text` | 转为字符串 |
| `json` | 当前原样保存，后续可增加结构校验 |

如果插件声明了 `config_schema`，未声明在 schema 中的字段不会被写入最终配置。缺失字段会优先沿用当前 `config`，再回退到 field `default`。

## 6. 后续方向

| 方向 | 说明 |
| --- | --- |
| Host API 配置写入联动 | 管理端配置 API 已可写入；后续可继续统一 Host API `SetConfig` 的审计和权限策略 |
| 类型校验增强 | 后续可校验 `default` 与 `type` 是否匹配 |
| 风格插件复用 | 个人主页风格插件可使用 schema 描述颜色、布局、字体等可配置项 |
| 自定义 HTML 安全模型 | 首页和个人主页当前只开放后端检测通过的受限 HTML 子集；任意 JS、未审查 HTML、CSP 强隔离和审核流仍是后续增强方向 |
