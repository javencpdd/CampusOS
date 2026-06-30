# hello-wasm

`hello-wasm` 是 CampusOS v0.3-dev 的最小 Wasm 插件示例。

当前 Wasm Runtime 支持两种事件 ABI：

- `handle_event()`：无参数兼容 ABI，返回非 0 表示允许事件继续。
- `handle_event(i32 ptr, i32 len)`：payload ABI，Runtime 将 JSON 格式的 `EventMessage` 写入插件导出的 `memory`，再把 `ptr` 和 `len` 传给入口函数。

本示例仍使用无参数兼容 ABI：

- Runtime 启动时读取 `plugin.yaml` 中的 `config.module`。
- Runtime 实例化 `plugin.wasm`。
- 事件到达时调用导出的无参数函数 `handle_event`。
- `handle_event` 返回非 0 表示允许事件继续。

本示例的 `handle_event` 固定返回 `1`，用于验证：

- 插件可以被安装。
- `runtime: wasm` 可以启动。
- `SendEvent` 可以调用 Wasm 导出函数。
- Manager 可以把事件处理结果写入 `plugin_logs`。

## 文件说明

| 文件 | 说明 |
| --- | --- |
| `plugin.yaml` | 插件 manifest |
| `plugin.wasm` | 可直接加载的预构建 Wasm 模块 |
| `src/handle_event.wat` | `plugin.wasm` 的可读源文件 |
| `src/plugin.wasm.hex` | `plugin.wasm` 的十六进制表示 |

## 重新生成 plugin.wasm

当前仓库不要求开发机安装 TinyGo 或 wat2wasm。可以从 hex 文件重新生成：

```bash
xxd -r -p examples/plugins/hello-wasm/src/plugin.wasm.hex examples/plugins/hello-wasm/plugin.wasm
```

生成后可用 Go 测试验证：

```bash
go test ./internal/plugin/wasm -run TestHelloWasmExampleLifecycleAndLogging -count=1 -v
```
