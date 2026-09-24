# 以 PDF Viewer 学习第一个 v4 外部插件

> 更新时间：2026-09-23（Asia/Shanghai）  
> 样例源码：[`plugins/campusos.pdf-viewer/`](https://github.com/javencpdd/CampusOS/tree/main/plugins/campusos.pdf-viewer)
> 前提：已按 [Docker 跨平台开发](/deployment/docker-development) 启动 CampusOS。

本教程不要求用户创建 API Key、Token 或数据库账户。PDF Viewer 是纯 UI 插件：它证明“插件源码自包含、界面隔离、
权限由宿主复核”这一条完整路径；它不是让插件获得用户文件路径或直接下载 URL 的示例。

## 1. 先看完整目录

```text
plugins/campusos.pdf-viewer/
├── plugin.yaml
├── config/
│   ├── system.defaults.json / system.schema.json
│   └── user.defaults.json   / user.schema.json
├── frontend/
│   ├── user/{index.html,main.ts}
│   ├── admin/{index.html,main.ts}
│   ├── shared/{bridge.ts,pdf-range-transport.ts,pdf-range.worker.ts}
│   ├── package.json
│   └── vite.config.ts
├── LICENSE
└── README.md
```

`frontend/` 包含插件自己的页面、依赖和构建配置；Web 与 Admin 主工程不再内嵌 PDF Viewer 的页面代码。
发布时，构建器只把 Manifest、README、许可证、配置定义和编译后的 UI 产物写进不可变 release；源码、`node_modules`、
临时文件和用户配置不会进入 release。

## 2. Manifest：声明，而不是授权

PDF Viewer 的 [`plugin.yaml`](https://github.com/javencpdd/CampusOS/blob/main/plugins/campusos.pdf-viewer/plugin.yaml) 包含四类关键事实：

```yaml
api_version: campusos.plugin/v4
key: campusos.pdf-viewer
version: 2.0.0-dev.2
compatibility:
  host: ">=1.1"
  ui: campusos.ui/v3
  bridge: campusos.bridge/v1
backend:
  runtime: none
```

它还声明了用户/管理员 UI 入口、`preview` Surface、三种 PDF 资源类型、所需 Capability，以及 system/user JSON
配置 schema。v4 校验器要求：

- 插件 key、Surface ID、route、入口路径都必须是安全的包内相对路径；不允许 URL、绝对路径、`..` 或符号链接。
- 能力只能从平台拥有的 Capability Catalog 中选择，当前均为 `self` scope；声明中必须包含用途。
- UI Surface 只能声明 `page`、`modal`、`drawer`、`fullscreen` 或 `new-tab` 展示方式，实际容器由宿主决定。
- `runtime` 不能包含任意命令行、数据库名称或环境变量。当前 PDF Viewer 使用 `none`。

执行源码合同检查：

```powershell
go run ./cmd/campusos-plugin-v4-check -root plugins
go run ./cmd/campusosctl plugin v4 validate plugins/campusos.pdf-viewer
```

## 3. 三层授权与三种资源入口

用户从图文文章附件、个人空间附件或“我的文档”点击“预览”时，宿主先检查业务访问权，再创建短期 Invocation：

```text
用户点击 PDF
  -> 宿主确认文章公开状态，或确认当前用户是个人文件/文档 owner
  -> Manifest 声明 ∩ 管理员 Grant ∩ 用户 Consent
  -> 当前 release、Surface、展示方式与调用上下文摘要
  -> 返回只绑定本次 Invocation 的隔离 iframe 地址
```

PDF Viewer 获取每段字节时，宿主还会再次检查资源状态、文章 ACL 或 owner、当前 release 及授权决策。插件从不接收
`storage_key`、磁盘路径、用户 JWT 或长期下载地址。普通下载是独立的宿主能力；预览插件不可用、撤权或过期时，
有权用户仍可从原入口下载文件。

三种入口与 Capability 的对应关系：

| 入口 | 资源类型 | 必需能力 |
| --- | --- | --- |
| 已开放图文文章的 PDF 附件 | `article_attachment` | `article_attachment.self.preview` |
| 当前用户的个人附件 | `personal_asset` | `personal_space_file.self.read` |
| 当前用户的个人文档 PDF | `personal_document` | `personal_space_file.self.read` |

## 4. 隔离 UI 与 Bridge

开发 Docker 栈把校验后的 release 静态文件通过 `http://<当前浏览器主机>:3003` 提供，而用户端和管理端仍在 3000/3001。
这个跨 Origin 设计避免插件页面与主站共享 DOM、Cookie 或脚本上下文。

`frontend/shared/bridge.ts` 做四件事：校验 `origin`、`source`、插件/版本/Surface/audience 和随机 challenge；建立
`MessagePort`；只接受冻结的 `campusos.bridge/v1` RPC；在窗口关闭时销毁 Port。当前 Bridge 仅允许：

```text
config.read / config.update
resource.describe / resource.readRange
backend.invoke / ui.requestSurface
records.read / records.write
```

PDF Viewer 的 `resource.readRange` 每次最多读取 1 MiB。PDF.js 通过自定义 Range Transport 分段读取，不把整份文件
base64 化，也不把受保护文件持久化到浏览器存储。插件不得使用 `fetch` 代理、任意 URL、SQL、文件路径或主站 Token 规避 Bridge。

## 5. 用户配置目录

用户在插件中心点击“添加到我的插件”时，若 release 声明 `configuration.user`，宿主会幂等创建：

```text
data/personal-space/<decimal-user-id>/plugins/campusos.pdf-viewer/
├── config/user.json       # 经 schema 校验的非敏感配置
├── config/meta.json       # release/schema/revision/hash，不能作为授权凭据
├── .pending/              # 主机恢复暂存，不在“我的文档”中显示
└── .snapshots/            # 有界配置快照
```

路径由宿主从已认证 user ID 与 Manifest key 计算，iframe 和用户输入不能指定它。配置更新使用 revision CAS、内容 hash 和
总预算检查；敏感 Secret 仍由宿主加密保存，不能写入 `user.json`。

## 6. 本地修改、构建与验收

修改 PDF Viewer 的 Manifest、前端依赖、Vite 配置或 Docker 插件 UI 脚本后，使用重建命令生成新的开发 release：

```powershell
.\scripts\docker-dev.ps1 rebuild
```

Linux、WSL2 或 Git Bash 使用：

```bash
./scripts/docker-dev.sh rebuild
```

仅改动 Go、Web/Admin 或普通文档时，正在运行的开发栈通常可热加载；但 v4 release 的前端产物、依赖和网关需要重建。
重新打开预览前应刷新浏览器，并在插件语义版本变更后让管理员和用户重新检查/确认当前版本的能力。

独立发布流程使用同一 v4 包校验链：

```bash
go run ./cmd/campusosctl plugin v4 stage plugins/campusos.pdf-viewer --out /tmp/pdf-release
go run ./cmd/campusosctl plugin v4 pack plugins/campusos.pdf-viewer --out /tmp/pdf-viewer.tar.gz
go run ./cmd/campusosctl plugin v4 sign /tmp/pdf-viewer.tar.gz --key-id <trusted-key-id> --key-file <base64-private-key-file> --out /tmp/pdf-viewer.signed.tar.gz
go run ./cmd/campusosctl plugin v4 install /tmp/pdf-viewer.signed.tar.gz --root plugins
```

最后人工验证：管理员检查当前 release 的 Grant；用户添加插件并同意所需能力；分别从文章、个人附件、个人文档打开 PDF；
随后撤销 Consent、停用插件或移除文章附件，确认下一次 Bridge 调用被拒绝，而有权下载路径仍按业务规则工作。

继续阅读：[v4 Manifest 与配置](/plugins/manifest)、[隔离 UI、Bridge 与 Gateway](/plugins/frontend-runtime)、
[可信市场与用户添加](/plugins/market-managed-data)。
