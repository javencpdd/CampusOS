# 插件体系

## 先确认你要开发什么

| 目标 | 应使用的类型 |
| --- | --- |
| 修改身份、社区、权限、User Storage 等平台完整性能力 | Core Module |
| 增加随主程序发布、可启停的官方业务功能 | Built-in Feature |
| 独立安装、升级、卸载和迁移的扩展 | External Plugin |
| 主题、主页风格、个人主页风格、Skill/Prompt | Resource Package |

本章的“插件”专指 External Plugin。内置课表、个人空间、富文本和 Appearance
由 Feature Registry 管理，不在插件目录中。

## 外部插件目录

```text
data/plugins/<plugin-name>/       实现、plugin.yaml、Wasm/进程入口
data/plugin_data/<plugin-name>/  KV、快照和运行数据
```

插件包更新代码时不能覆盖用户数据。需要写用户文件时，必须声明能力并通过
Host API/User Storage，不得自行拼接 `data/personal-space` 绝对路径。

## Runtime

### Wasm

`runtime: wasm` 由 wazero 在受控环境中加载，适合小型事件逻辑。Wasm 只能
调用声明并获准的 Host API。

### 受管进程

`runtime: grpc` 是历史兼容名称。CampusOS 启停独立进程，并通过显式声明、
严格校验的 loopback HTTP Extension/Event 端点通信。它目前不是标准
protobuf gRPC 协议。

### 为什么没有 builtin

`runtime: builtin` 只保留旧文件解析能力，不能被 Plugin Manager、CLI 或导入
流程安装。Built-in 使用 `modules/*/module.yaml`，并随 CampusOS 重新编译。

## 权限和事件

```yaml
events:
  subscribe: [thread.created]
permissions:
  api:
    - resource: log
      actions: [write]
```

Manifest 是权限申请，不是授权结果。Host API 默认拒绝；用户数据能力还需要
用户 Grant。外部插件永远不能直接取得数据库连接、JWT 私钥、CampusOS 用户
Token、`AppContext` 或内部 Service。

## UI Runtime

外部插件可以声明 Route、Navigation、Surface 和 Action。业务 Action 通过：

```text
/api/v1/extensions/:plugin/*path
```

Core 注入可信调用者上下文，插件不能相信请求正文中的伪造用户 ID。运行清单
将外部贡献放在 `plugins[]`，内置功能贡献放在 `modules[]`。

## 示例

| 示例 | 位置 | 用途 |
| --- | --- | --- |
| `hello-wasm` | `data/plugins/hello-wasm` | 最小 Wasm 事件插件 |
| `grpc-example` | `examples/plugins/grpc-example` | 最小受管进程模板 |
| `campus-welcome` | `examples/plugins/campus-welcome` | UI Surface 与 Gateway |
| `v2-managed-example` | `examples/plugins/v2-managed-example` | Grant、受管记录和文件 |
| `schedule-helper` | `examples/plugins/schedule-helper` | 以课表场景讲解可移植外部插件 |
| Built-in descriptor | `examples/modules/builtin-feature-example` | 仅说明模块描述符，不可 `plugin install` |

下一步：[课表插件完整教程](/plugins/schedule-plugin-tutorial) 或
[编写第一个插件](/plugins/create-first-plugin)。
