# Go SDK、CLI 与本地测试

## 10 分钟闭环

```bash
go run ./cmd/campusosctl plugin init my-plugin --runtime grpc
go run ./cmd/campusosctl plugin test my-plugin
go run ./cmd/campusosctl plugin build my-plugin
go run ./cmd/campusosctl plugin verify my-plugin --json
go run ./cmd/campusosctl plugin pack my-plugin
```

`plugin dev` 会依次执行 test、build 和 verify。CI 建议使用 `--json` 读取稳定结果，而不是解析人类输出。

## Runtime 差异

| Runtime | build/test 行为 | 生命周期 |
| --- | --- | --- |
| gRPC process | `go test ./...`、构建 `plugin` 可执行文件 | 默认 `plugin-restart` |
| Wasm | 检查 `plugin.wasm` 魔数和运行包 | 默认 `hot`，候选实例验证后交换 |

当前 `grpc` 名称是兼容标识，进程事件传输尚未成为稳定 protobuf 契约。不要据此假设任意标准 gRPC 插件都能直接运行。

## Mock Host

Go SDK 的 `NewHarness` 可注入配置、KV、用户、帖子、权限允许/拒绝、Host API 失败、日志、通知和事件，不需要启动完整服务器。HTTP 403 可通过 `errors.Is(err, campusos.ErrPermissionDenied)` 判断。

External Plugin 可运行模板位于 `examples/plugins/grpc-example` 和 `examples/plugins/wasm-example`。`examples/modules/builtin-feature-example` 只演示编译期模块描述符，不支持 `plugin install`。

资源目录使用独立命令：

```bash
go run ./cmd/campusosctl resource adopt data/resources/themes/my-theme --type theme
go run ./cmd/campusosctl resource inspect data/resources/themes/my-theme
```
