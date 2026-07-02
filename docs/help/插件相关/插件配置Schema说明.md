# CampusOS 插件配置 Schema 说明

> 日期：2026-07-01
> 范围：v0.4-dev 插件配置 schema 草案

## 1. 设计目标

插件可以在 `plugin.yaml` 中声明 `config_schema`，用于描述哪些配置项可以被后台、CLI 或测试工具展示和编辑。

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

## 5. CLI 行为

`campusosctl plugin init` 会为新插件生成最小 `config_schema`：

```bash
go run ./cmd/campusosctl plugin init my-plugin --runtime wasm
go run ./cmd/campusosctl plugin init my-grpc-plugin --runtime grpc
```

Wasm 插件模板默认暴露 `entrypoint` 和 `event_timeout_ms`；gRPC 插件模板默认暴露 `command` 和 `event_timeout_ms`。

`campusosctl plugin inspect` 会输出 `config_schema`：

```bash
go run ./cmd/campusosctl plugin inspect examples/plugins/hello-wasm
```

`campusosctl plugin pack` 会在打包前解析并校验 manifest：

```bash
go run ./cmd/campusosctl plugin pack examples/plugins/hello-wasm
```

当前校验规则：

| 规则 | 结果 |
| --- | --- |
| `key` 为空 | 拒绝 |
| `key` 重复 | 拒绝 |
| `type` 不在支持列表内 | 拒绝 |
| `select` 没有 `options` | 拒绝 |

当前 CLI 校验只负责 schema 结构合法性，还不负责把 `default` 与 `type` 做强一致校验，也不会直接修改插件运行配置。

## 6. 后续方向

| 方向 | 说明 |
| --- | --- |
| 后台表单渲染 | 插件详情页按 `config_schema` 自动渲染配置表单 |
| 配置写入 | 表单提交后调用 Host API `SetConfig` 或管理端插件配置 API |
| 类型校验增强 | 后续可校验 `default` 与 `type` 是否匹配 |
| 风格插件复用 | 个人主页风格插件可使用 schema 描述颜色、布局、字体等可配置项 |
