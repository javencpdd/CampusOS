# 编写第一个插件

本教程创建一个名为 `notice-board` 的 Wasm 插件目录，完成初始化、manifest 检查和打包。脚手架不会替你生成业务 Wasm 模块，实际事件逻辑可以参考 `data/plugins/hello-wasm`。

## 1. 创建脚手架

在 CampusOS 仓库根目录执行：

```bash
go run ./cmd/campusosctl plugin init notice-board \
  --runtime wasm \
  --dir data/plugins/notice-board
```

生成：

```text
data/plugins/notice-board/
├── plugin.yaml
└── README.md
```

插件名只能使用小写字母、数字、连字符和下划线。目录名建议与 `name` 一致。

## 2. 准备 Wasm 模块

把编译后的模块保存为：

```text
data/plugins/notice-board/plugin.wasm
```

学习当前 ABI 时，先阅读：

```text
data/plugins/hello-wasm/README.md
data/plugins/hello-wasm/src/handle_event.wat
```

当前示例支持无参数兼容入口 `handle_event()`，返回非零值表示允许事件继续。

## 3. 修改 manifest

最小示例：

```yaml
name: notice-board
display_name: "Notice Board"
version: "0.1.0"
description: "Records newly created threads."
runtime: wasm
scope: user

events:
  subscribe:
    - thread.created

permissions:
  api:
    - resource: log
      actions: [write]

storage:
  type: none

config:
  module: plugin.wasm
  entrypoint: handle_event
  event_timeout_ms: 1000

config_schema:
  fields:
    - key: event_timeout_ms
      label: "事件超时"
      type: number
      required: true
      default: 1000
```

## 4. 检查目录

```bash
go run ./cmd/campusosctl plugin inspect data/plugins/notice-board
```

检查输出至少包含：

- `name` 和 `version` 正确。
- `runtime` 为 `wasm`。
- 事件和权限与预期一致。
- `config.module` 指向真实存在的文件。

## 5. 运行测试

插件自己的测试应覆盖：

1. 合法事件可以解析。
2. 非法 payload 不会导致 Runtime 崩溃。
3. 未声明 Host API 权限时调用被拒绝。
4. 超时和错误能够写入插件日志。
5. 重复事件不会产生不可接受的副作用。

CampusOS 回归测试：

```bash
GOCACHE=/tmp/campusos-go-cache go test ./internal/plugin/... -count=1
```

## 6. 打包

```bash
go run ./cmd/campusosctl plugin pack data/plugins/notice-board
```

或指定输出：

```bash
go run ./cmd/campusosctl plugin pack \
  data/plugins/notice-board \
  --out /tmp/notice-board-0.1.0.campusos-plugin.tar.gz
```

## 7. 导入管理端

1. 登录 Admin。
2. 打开“插件管理”。
3. 选择导入插件包。
4. 查看预检结果、权限和版本变化。
5. 确认导入。
6. 加载插件并查看日志。

`scope: user` 插件可以热加载；不要把第三方插件改成 `scope: system` 来绕过导入限制。

## 8. 完成标准

- manifest 可以被 `inspect` 解析。
- 插件包可以通过预检并导入。
- 插件能加载、接收事件并产生可追踪日志。
- 权限保持最小集合。
- README 说明配置、数据位置、错误和卸载影响。
