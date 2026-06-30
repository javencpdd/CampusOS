# CampusOS Go SDK

`sdk/go` 是 v0.3-dev 的最小 Go SDK 雏形，先固定插件常用的数据结构和 Host API HTTP 调用封装。

## 当前能力

| 能力 | 状态 |
| --- | --- |
| Event 类型 | 已提供 |
| Manifest 类型 | 已提供 |
| Host API client | 已提供 |
| `GetUser` / `GetThread` / `GetReply` / `QueryThreads` | 已封装 |
| `PublishEvent` / `SendNotification` | 已封装 |
| `GetConfig` | 已封装 |
| `SetConfig` / `CheckPermission` / `Log` | 已封装 |
| `StorageGet` / `StorageSet` / `StorageDelete` | 已封装 |
| Wasm 编译模板 | 后续任务 |
| 本地插件测试工具 | 后续任务 |

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
