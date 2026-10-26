# v5 无 UI Echo Chat Provider

> 更新时间：2026-10-20（Asia/Shanghai）

此独立作者包只回显当前用户显式提交的文本，提供 `ai.chat/v1`，不消费宿主能力，不调用模型，不读取宿主数据、网络、Secret 或平台数据库。后端为 Container HTTP 独占 stdin/stdout，使用[公共 Go SDK](../../sdk/go/README.md)的 `ServeV5ContainerChat`，没有监听端口。旧 WASI stdin 示例已替换，因为它不符合当前无 WASI 的 Wasm JSON ABI。

## 显式作者构建

从仓库根目录运行，输出目录必须不存在：

```bash
bash scripts/build-v5-chat-echo.sh --out /tmp/campusos-chat-author-new
```

构建器离线导出独立 SDK/作者 Go module，只使用标准库及公共 SDK，生成 Linux/amd64 静态 `image/provider`、scratch 非 root Dockerfile、依赖与字节摘要记录，以及 `package-template/plugin.json`。不使用宿主 module 或 `internal/`，不拉取镜像、不安装插件、不重启宿主。包检查和安装不会自动运行构建器。

需要本机已安装 Go 1.25 或更新工具链；`GOTOOLCHAIN=local` 和离线解析禁止自动下载工具链。若系统 `go` 较旧，显式选择现有安装的绝对路径：

```bash
CAMPUSOS_AUTHOR_GO=/path/to/go1.25/bin/go bash scripts/build-v5-chat-echo.sh --out /tmp/campusos-chat-author-other-new
```

作者须随后显式构建镜像：

```bash
docker build --pull=false --network=none --tag localhost/campusos-chat-echo:author /tmp/campusos-chat-author-new/image
docker image inspect --format '{{json .RepoDigests}}' localhost/campusos-chat-echo:author
```

源码 `plugin.json` 与导出模板中的 64 零镜像摘要只是未发布占位符。必须先将导出模板替换为镜像实际可解析的本地 `localhost/campusos-chat-echo@sha256:...` RepoDigest，再打包签名；不能用 tag、image ID、猜测摘要或占位符作发布证据。若引擎尚无 RepoDigest，应先完成该引擎的镜像发布/解析，包验证不会凭空制造镜像身份。

使用已构建的 CLI：

```bash
campusosctl plugin validate /tmp/campusos-chat-author-new/package-template
campusosctl plugin pack /tmp/campusos-chat-author-new/package-template --out /tmp/chat-echo.campusos
campusosctl plugin sign /tmp/chat-echo.campusos --key-id <publisher-key-id> --key-file <private-key-file> --out /tmp/chat-echo.release.json
campusosctl plugin verify /tmp/chat-echo.campusos --release /tmp/chat-echo.release.json --trust <trust-file>
```

这些步骤不会安装或执行插件。私钥及当前发布者信任配置由操作者显式提供，示例不携带密钥。安装后还须独立批准当前版本资源、设置配置并启用；消费声明为空不免除当前安装和调用权限检查。

## 行为与边界

配置 `prefix` 是 1–128 字符的普通字符串，默认 `Echo: `，作用域为 system/user。全局实例读取 system 配置；用户实例若有 user.prefix 则优先使用，否则使用 system.prefix。两名用户可以设置不同前缀，由 typed Dispatcher 调用各自安装实例验证隔离，客户端不能用请求字段指定别人的 owner。

只接受 `context_class=none`。`deadline_ms` 是相对剩余毫秒数，SDK 根据它创建 context，不与 Unix 时间比较。请求和响应严格匹配 ID、贡献 ID、实例代次及有界 JSON；取消、handler 错误或异常不会释放未验证输出，也不会写出 prompt/配置日志。SDK 只处理 Configure/Health/Invoke，不提供任意回调、Host API、Event 或网络服务。

## 无 UI 安装、配置与升级

管理端导入签名包后单独安装，审阅并批准当前版本的 CPU、内存、并发、超时和存储上限，在通用插件配置页设置系统 `prefix`，再启用全局安装。用户分别在插件中心安装、在通用配置页保存自己的 `prefix` 并启用。没有插件自带 UI 也能使用宿主配置和操作诊断；`ready` 须来自实际隔离实例及合同检查，导入成功不等于可调用。

发布新版本时复制作者输出的 `package-template` 到新目录，修改副本的 `version`（联合验收使用 `1.1.1`），核对实际 RepoDigest 后重新 validate/pack/sign/verify。不要修改已经签名的 `1.1.0`，也不要将零摘要模板当成已发布包。只修改包元数据时可以继续引用同一不可变镜像；后端代码变化则须显式重建镜像并固定新 RepoDigest。目标版本需要独立审批，配置也绑定版本；全局与各用户分别升级，不能替其他用户同意或变更配置。准备失败保留原版本，切换成功撤下旧代次。卸载可选择保留数据或清理本人配置/检查点；包 GC 须等全部安装和回退引用解除后显式执行。

## 类型化消费与联合验收

消费方是编译进宿主的 Feature/合同适配器：使用宿主受管 Dispatcher 选择当前用户的 `ai.chat/v1/text` 贡献，提交匹配 ID、实例代次、相对期限的 `V5ChatRequest` 和有限调度信封，并传入 Identity 当前会话 resolver。请求不能自报 owner、Grant 或 Feature。宿主重新验证当前用户会话、Feature、安装/授权、配置与实例代次后才释放校验过的 `V5ChatResponse`。本包仅接受显式 prompt 和 `context_class=none`，不给调用方创建公开/私有资源授权。

可复现的 [08a 联合验收入口](../../scripts/v12-08a-ecosystem-drill.sh)先构建作者镜像、两个独立版本签名包、PDF 及宿主/SPA，随后在新的独占 PG 和 OS 只读挂载中执行真实生产 Server 组装、隔离执行器和浏览器。合同消费者只编译在测试 ELF，通过私密 Unix socket 接入同一 Server 的实际 Dispatcher；标准 `cmd/server` 没有该测试端点或虚构 AI Feature。验收覆盖两名用户、独立配置、重载、升级、当前撤权、本人清理/包 GC 和 PDF/Core 回归。

合同消费者证明 Provider 可调用、当前权限和实例隔离；内建 AI/RAG 的实际业务接入属于 V12-14c。阶段状态与封存证据以 [v1.2 主计划](../../docs/项目计划书v1/项目计划v1.2/00-v1.2版本优化计划书.md)为准，Windows 仍按用户决定延期。
