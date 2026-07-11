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

### Built-in

`runtime: builtin` 不启动外部进程。真实业务实现编译在 CampusOS 后端中，插件目录提供 manifest、配置和说明。

适合随系统交付且需要调用内部领域服务的能力，例如个人空间、课表、富文本和版主管理。

Built-in 不是允许第三方直接把任意 Go 代码放入目录后动态执行。增加新的 Built-in 能力通常仍需修改并重新编译 CampusOS。

### Wasm

`runtime: wasm` 由 wazero 加载模块。插件通常包含：

```text
plugin.yaml
plugin.wasm
README.md
```

适合小型事件处理和需要更强隔离的扩展。Wasm 插件只能通过声明并获准的 Host API 访问主系统。

### gRPC

`runtime: grpc` 作为独立进程运行。适合复杂依赖、长任务或需要独立部署节奏的服务。

gRPC 插件必须说明启动命令、连接方式、超时和故障恢复要求。

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

## 系统级与用户级

| scope | 生效方式 | 说明 |
| --- | --- | --- |
| `system` | 启停目标状态在 API 重启后生效。 | 随 CampusOS 部署的核心能力。 |
| `user` | 管理员可以加载、停止、重载和覆盖更新。 | 独立扩展，支持热加载。 |

这里的 `user` 是生命周期分类，不表示普通用户可以自行安装插件。安装、配置和更新仍属于管理员操作。

## 示例插件

| 插件 | 用途 |
| --- | --- |
| `hello-wasm` | 最小可运行 Wasm 事件插件。 |
| `hello-plugin` | gRPC manifest 示例。 |
| `personal-space` | Built-in 个人主页和用户文件能力。 |
| `homepage-customizer` | Built-in 首页配置与风格包。 |
| `web-theme` | Built-in 完整用户前台系统主题目录、用户本地选择和沙箱特效能力。 |
| `controlled-richtext-article` | Built-in 富文本文章。 |
| `personal-schedule` | Built-in 个人课表。 |
| `category-moderation` | Built-in 版块版主管理。 |

下一步：[编写第一个插件](/plugins/create-first-plugin)。
