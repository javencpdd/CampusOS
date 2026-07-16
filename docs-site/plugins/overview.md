# 插件体系

## 插件是什么

CampusOS 插件是带有 `plugin.yaml` 的独立功能目录。Manager 通过 manifest 识别插件名称、版本、Runtime、事件、权限、存储和配置，再决定如何加载。

插件实现默认保存在：

```text
data/plugins/<plugin-name>/
```

运行数据保存在：

```text
data/plugin_data/<plugin-name>/
```

这两个目录不能混用。插件包可以更新代码，但不应覆盖用户数据。

## Runtime

### Built-in 兼容映射

`runtime: builtin` 不启动外部进程。真实业务实现编译在 CampusOS 后端中，插件目录提供 manifest、配置和说明。

个人空间、课表、富文本和外观等当前由 Built-in Feature Registry 管理；Moderation 的权限、作用域和审计属于始终启用的 Core。旧 `runtime: builtin` Manifest 只保留配置和兼容映射。

Built-in 不是允许第三方直接把任意 Go 代码放入目录后动态执行。增加新的 Built-in 能力通常仍需修改并重新编译 CampusOS。

### Wasm

`runtime: wasm` 由 wazero 加载模块。插件通常包含：

```text
plugin.yaml
plugin.wasm
README.md
```

适合小型事件处理和需要更强隔离的扩展。Wasm 插件只能通过声明并获准的 Host API 访问主系统。

### 受管进程（历史名称 `grpc`）

`runtime: grpc` 作为独立进程运行。CampusOS 当前通过受限 loopback HTTP 调用显式配置的 Extension/Event 端点，不提供稳定 protobuf gRPC 协议。

进程插件必须说明入口、loopback 端点、超时和故障恢复要求；不能据此假设任意标准 gRPC 服务可以直接导入。

## 事件与 Host API

插件可以订阅核心事件，例如：

```yaml
events:
  subscribe:
    - thread.created
    - post.created
```

插件调用 Host API 前，Manager 会根据 manifest 权限检查：

```yaml
permissions:
  api:
    - resource: log
      actions: [write]
```

权限默认拒绝。只声明当前功能需要的最小集合，不要使用“以后可能会用”为理由申请高风险权限。

## 管理级别与生效方式

| 字段 | 说明 |
| --- | --- |
| `scope: system/user` | 管理级别、安装权限和系统重要性。 |
| `lifecycle.backend.activation_mode` | `restart`、`plugin-restart` 或 `hot`。 |
| `lifecycle.frontend.activation_mode` | 当前统一为 `hot`。 |

这里的 `user` 不表示普通用户可以自行安装插件。安装、配置和更新仍属于管理员操作。受管进程可以只重启插件，Wasm 可以热替换；具体行为不能再从 scope 推断。

## 示例插件

| 插件 | 用途 |
| --- | --- |
| `hello-wasm` | 最小可运行 Wasm 事件插件。 |
| `grpc-example` | 兼容 `grpc` 名称的最小进程模板，不代表标准 gRPC。 |
| `v2-managed-example` | 可编译、可测试的受管数据、用户 Grant 与 loopback Extension 示例。 |
| `campus-welcome` | UI Runtime、声明式 Surface、Action 和 Extension Gateway 完整示例。 |
| `personal-space` | Built-in Feature 个人主页能力；用户文件由 User Storage Core 提供。 |
| `homepage-customizer` | Built-in 首页配置与风格包。 |
| `web-theme` | Built-in 完整用户前台系统主题目录、用户本地选择和沙箱特效能力。 |
| `controlled-richtext-article` | Built-in 富文本文章。 |
| `personal-schedule` | Built-in 个人课表。 |
| `category-moderation` | Legacy Manifest；权限、作用域和审计已归 Moderation Core。 |

下一步：[编写第一个插件](/plugins/create-first-plugin)。
