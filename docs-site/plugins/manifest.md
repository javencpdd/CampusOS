# v4 Manifest 与配置

> 更新时间：2026-09-23（Asia/Shanghai）

每个 v4 源插件在根目录放置 `plugin.yaml`。它是声明式、可校验的包入口；不能包含命令行、数据库名、宿主路径、
URL、凭据、Token 或可执行回调。

## 最小可用结构

```yaml
api_version: campusos.plugin/v4
key: example.readonly-viewer
version: 1.0.0
publisher:
  id: example
  name: Example Publisher
display_name: 示例只读查看器
description: 由宿主授权后显示当前用户可访问的内容。
compatibility:
  host: ">=1.1"
  ui: campusos.ui/v3
  bridge: campusos.bridge/v1
backend:
  runtime: none
artifacts:
  user_ui:
    source: frontend/user
    entry: ui/user/index.html
ui:
  user:
    surfaces:
      - id: preview
        route: preview
        title: 示例预览
        presentations: [modal, fullscreen]
capabilities: []
```

`key` 必须是小写命名空间标识，例如 `publisher.plugin-name`；`version` 必须是 SemVer；每个 UI audience 必须同时声明
一个 `artifacts` 入口和至少一个 Surface。`source` 仅存在于源码包，`entry` 是 release 内的编译后入口。

## 主要字段

| 字段 | 作用 | 宿主校验 |
| --- | --- | --- |
| `compatibility` | 声明 host、UI 和 Bridge 合同 | UI 必须是 `campusos.ui/v3`，Bridge 必须是 `campusos.bridge/v1`。 |
| `backend.runtime` | `none`、`wasm` 或 `container` | 不接受插件提供任意进程命令；当前生产化样例为 `none`。 |
| `artifacts` / `ui` | 用户端或管理端隔离页面与可展示 Surface | 路径、Surface ID、route、presentation 必须安全且唯一。 |
| `preview_providers` | 宿主可选择的预览提供者 | 不是文件权限；目前只允许三类 PDF 资源与 `application/pdf`。 |
| `capabilities` | 插件请求的最小能力、用途和 required 标记 | 只能使用平台 Catalog 已定义的 `self` capability。 |
| `configuration` | system/user JSON schema 与默认值 | 路径必须在包内，JSON 与受限 schema 必须有效。 |
| `data.user_config_max_bytes` | 单插件用户配置上限 | 范围为 0–256 KiB，且仍受用户总配置预算控制。 |

## 配置的两种归属

```yaml
configuration:
  system:
    schema: config/system.schema.json
    defaults: config/system.defaults.json
    version: v1
  user:
    schema: config/user.schema.json
    defaults: config/user.defaults.json
    version: v1
```

- **系统配置**由管理员通过宿主控制面维护；插件页面不获得管理员万能权限。
- **用户配置**只有用户点击添加已安装插件后才由宿主在个人空间创建；保存时验证 schema、revision 和 hash。
- **Secret**不属于 JSON 配置。它由宿主加密保存并只在适当的授权调用中使用，不能写入 Manifest 或前端 Bundle。

完整真实样例请阅读 [`campusos.pdf-viewer/plugin.yaml`](../../plugins/campusos.pdf-viewer/plugin.yaml)。更改源码后先执行：

```bash
go run ./cmd/campusosctl plugin v4 validate plugins/campusos.pdf-viewer
go run ./cmd/campusos-plugin-v4-check -root plugins
```

修改版本、能力用途、UI 合同或包内容时必须创建新的不可变 release。Manifest 声明不等于获权，后续调用仍须经过
[三层授权与资源访问](/plugins/authorization-v3)。
