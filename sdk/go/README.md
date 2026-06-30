# CampusOS Go SDK

`sdk/go` 是 v0.3-dev 的最小 Go SDK 雏形，先固定插件常用的数据结构和 Host API HTTP 调用封装。

## 当前能力

| 能力 | 状态 |
| --- | --- |
| Event 类型 | 已提供 |
| Manifest 类型 | 已提供 |
| Manifest `config_schema` 类型 | 已提供 |
| Host API client | 已提供 |
| `GetUser` / `GetThread` / `GetReply` / `QueryThreads` | 已封装 |
| `PublishEvent` / `SendNotification` | 已封装 |
| `GetConfig` | 已封装 |
| `SetConfig` / `CheckPermission` / `Log` | 已封装 |
| `StorageGet` / `StorageSet` / `StorageDelete` | 已封装 |
| 本地插件测试 Harness | 已提供 |
| Wasm 编译模板 | 后续任务 |

## 示例

```go
client := campusos.NewHostClient("hello-wasm")

value, found, err := client.GetConfig(ctx, "entrypoint")
if err != nil {
    return err
}
if found {
    fmt.Println(value)
}
```

默认 Host API 地址是：

```text
http://127.0.0.1:18080
```

如需自定义地址：

```go
client := campusos.NewHostClientWithBaseURL("http://127.0.0.1:18080", "hello-wasm")
```

## 本地测试 Harness

插件逻辑单元测试可以使用 `NewHarness` 模拟 Host API，不需要启动完整 CampusOS 服务：

```go
harness := campusos.NewHarness("hello-wasm")
defer harness.Close()

harness.Config["entrypoint"] = "handle_event"
client := harness.Client()

value, found, err := client.GetConfig(ctx, "entrypoint")
```

Harness 当前支持配置、存储、事件发布、日志、用户/主题/回复查询和权限检查的最小模拟。Harness 使用内存 HTTP transport，不监听本地端口，适合在受限 CI 或沙箱环境中运行。

## 配置 Schema

Manifest 可声明 `config_schema`，供后台或 CLI 渲染插件配置表单：

```yaml
config_schema:
  fields:
    - key: "entrypoint"
      label: "Entrypoint"
      type: "string"
      required: true
      default: "handle_event"
```

SDK 中对应 `ConfigSchema`、`ConfigField`、`ConfigOption` 类型。当前字段类型建议使用：

```text
string, text, number, boolean, select, json
```
