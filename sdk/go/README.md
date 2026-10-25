# CampusOS Go SDK

> 更新时间：2026-10-20（Asia/Shanghai）

当前 v1.2 作者接口提供完整 `V5Manifest`、类型化 consumes/provides、Dispatcher 与 Chat/Preview/Knowledge DTO，以及无 UI Chat Provider 的 Container HTTP 服务。公共 Go SDK 仅依赖标准库；作者构建可以复制本目录的生产 `.go` 文件为独立模块，不使用宿主 `go.mod`、`internal/` 或第三方依赖。

## Container Chat 作者接口

```go
func ServeV5ContainerChat(
    ctx context.Context,
    input io.ReadCloser,
    output io.Writer,
    handler V5ContainerChatHandler,
) error

type V5ContainerChatHandler func(
    context.Context,
    V5RuntimeConfiguration,
    V5ChatRequest,
) (V5ChatResponse, error)

type V5RuntimeConfiguration struct {
    System json.RawMessage
    User   json.RawMessage
}
```

`ServeV5ContainerChat` 在宿主独占的 stdin/stdout 上顺序处理 HTTP/1.1 `Configure`、`Health` 和 `Invoke`。它不监听端口、拨号、读取凭据环境变量或注册宿主路由。只支持 `campusos.container-pipe/v1`；Event、HostResult、gRPC 和 Host API 调用不在此接口内。

配置只含宿主已校验的普通 system/user JSON 值，既不含 Secret 引用，也不代表安装、owner 或授权。每次 handler 调用获得独立副本，不能通过修改它影响后续调用。未知、重复字段、额外 JSON、非对象配置及超过深度/字节限制的输入拒绝。

Chat 请求必须是完整 `V5ChatRequest`，`context_class=none`，`deadline_ms` 为 **1–300000 的剩余毫秒数**。宿主负责绝对截止时间和当前身份、实例、Grant/Consent；作者不得把 DTO 的 deadline 与 Unix 时间戳比较。SDK 对每次调用创建相对超时 context，handler 必须响应取消，超时后的返回值不发送。任意不合作 handler 的强制中止依赖宿主原生进程隔离与 kill，SDK 不创建无法收回的后台 handler。

请求 payload 至多 64 KiB、frame 至多 96 KiB、HTTP 头至多 8 KiB；请求 prompt 至多 12000 个 Unicode 字符。响应必须匹配 request/contribution/generation，token 计数非负、finish_reason 为 `completed` 或 `length`，输出同时满足 50000 字符及 64 KiB 限额。handler error、panic、过期或非法响应只产生通用失败 frame，不发送内部错误、配置或未验证正文。协议错误返回通用错误并关闭输入；context 取消会关闭输入，以中断阻塞读取。

参考 [独立 Echo 作者包](../../plugins/campusos.chat-echo/README.md)。从仓库根目录可显式导出独立作者构建：

```bash
bash scripts/build-v5-chat-echo.sh --out /tmp/campusos-chat-author-new
```

输出目录必须不存在。该操作仅构建作者静态程序与未发布包模板；Docker 镜像构建、真实不可变镜像摘要、打包签名、安装、审批、配置与启用均为后续显式步骤。不得把构建成功称为已运行插件。

构建需要已安装的 Go 1.25 或更新工具链，使用 `GOTOOLCHAIN=local`、离线 module 解析，不自动下载 Go。系统 `go` 较旧时可显式选择现有工具链的绝对路径：

```bash
CAMPUSOS_AUTHOR_GO=/path/to/go1.25/bin/go bash scripts/build-v5-chat-echo.sh --out /tmp/campusos-chat-author-other-new
```

## 旧客户端与测试工具

`HostClient`、`NewHarness` 和旧 DTO 仍保留给历史调用者/测试。它们使用早期 `/api/host/*`、Plugin Token、`config_schema` 和内存模拟，不是 v5 的凭据、运行入口或宿主能力授权，不应放入新 v5 Provider 的调用链。v5 作者不能通过这些接口访问数据库、任意资源、Secret 或旧自动发现机制。

包签名与验证使用当前 `campusosctl plugin pack|sign|verify`；安装/授权与生命周期独立。Manifest、DTO 或 SDK 构建本身不会授予执行权。详见[包与签名说明](../../docs/help/插件v5包与签名操作说明.md)与[Container 执行合同](../../docs/api/v1.2插件Container执行沙箱合同.md)。

## 当前作者验收边界

独立离线SDK/作者模块、真实CLI签名包及同一只读Server/SPA中的两owner typed调用、配置代次、重载/升级/撤权/卸载/GC已在V12-08a当前Linux范围验收，详见[.134](../../docs/进度/v1.2-dev/v1.2.134-dev.md)。合同消费者由测试程序编译接入实际managed Dispatcher并提供当前Identity resolver，标准生产服务没有任意Provider调用端点。SDK的DTO/服务协议不提供owner或资源授权；内建AI/RAG实际接入见下节14c，Windows验收延期。

## V12-11 受管公开知识调用

`KnowledgeSearch` / `KnowledgeSource` 使用 `V5HostTransport`，不接受平台Token或URL。`ServeV5ContainerChatWithHost` 可在既有独占管道同步yield只读Host，保持原始期限和32次上限；普通chat接口仍拒绝HostResult。两项能力必须在manifest声明 `collection` Scope并获得当前Grant，当前原来源ACL仍每次重验。详见[合同与可构建示例](../../docs/api/v1.2公开知识MCP与HostSDK合同.md)。这些接口不授予私人知识或内建AI/外部Provider接入。

## v1.2 AI / Knowledge 双向接入

更新时间：2026-10-25（Asia/Shanghai）。新增精确本机/远程Profile模型与公开引用问答 DTO/helper；模型调用要求当前User、管理员独立批准及用户同意，并保留预算与必需结算。来源贡献的公开归属由宿主逐项重核。Go SDK另提供双贡献点的有界Container管道；消费许可与贡献注册分别处理。详见[接入合同](../../docs/api/v1.2插件AI与Knowledge双向接入合同.md)及[独立作者包](../../plugins/campusos.ai-knowledge/README.md)。
