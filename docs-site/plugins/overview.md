# v4 自包含外部插件体系

> 更新时间：2026-09-23（Asia/Shanghai）
> 适用范围：`campusos.plugin/v4` 新插件开发。v1-v3 目录和文档仅保留兼容与历史参考，不能作为新插件模板。

## 先选对扩展类型

| 目标 | 正确类型 | 生命周期 |
| --- | --- | --- |
| 身份、权限、社区、User Storage 等平台完整性能力 | Core Module | 始终随主程序运行。 |
| 官方业务功能，例如课表、个人空间、富文本 | Built-in Feature | 随 CampusOS 编译，可配置/启停，不可市场安装。 |
| 可独立发布、安装、授权和卸载的业务扩展 | External Plugin | 使用 `campusos.plugin/v4`。 |
| 主题、主页样式、Prompt、Skill 等无业务 Runtime 的数据 | Resource Package | 校验、导入、应用，不执行插件代码。 |

本章的“插件”只指 External Plugin。第三方代码不能通过 `runtime: builtin` 获取进程内访问权，也不能直接读取平台数据库、
宿主目录、JWT、Cookie、Redis、NATS 或环境密钥。

## 当前 v4 文件与数据边界

```text
plugins/<plugin-key>/                         # 受版本控制的插件源码
  plugin.yaml
  config/{system,user}.{schema,defaults}.json
  frontend/{user,admin}/                      # 可选的隔离 UI 源码
  backend/ storage/sqlite/ tests/             # 按需使用

plugins/.installed/<plugin-key>/<version>-<digest>/  # 已校验的不可变 release，唯一可发现运行产物
plugins/.staging/                              # 宿主原子安装临时目录，不可执行
data/personal-space/<user-id>/plugins/<key>/config/  # 宿主创建的用户配置，不对 iframe 暴露
```

源码目录、暂存目录和用户配置目录不会被直接执行或作为静态网页根目录。开发模式会先把源码打包为 release，再走同一套
校验/安装路径；生产模式只扫描 `.installed/`。旧 `data/plugins/`、`data/plugin_data/` 属于 v1-v3 兼容运行时，
不能作为 v4 源码发现入口。

## 当前可运行样例：PDF Viewer

[`plugins/campusos.pdf-viewer/`](https://github.com/javencpdd/CampusOS/tree/main/plugins/campusos.pdf-viewer) 是当前唯一的 v4 外部插件：

- `runtime: none`，不运行插件后端进程；预览内容仍由宿主按权限读取。
- 用户端 PDF.js 页面和管理端设置页随 release 发布到独立 Origin（开发栈默认 `:3003`）。
- 图文文章附件、个人附件、个人文档三种 PDF 入口由宿主创建短期 Invocation；插件只通过 Bridge 请求受限的描述和 Range 字节。
- 静态 JS/Worker 可按 release digest 使用浏览器缓存；受保护 PDF 字节不写入 Service Worker、IndexedDB 或共享缓存。

请从 [PDF Viewer 入门教程](/plugins/pdf-viewer-tutorial) 开始，而不是从旧课表示例复制 Manifest。

## 新插件的通用流程

```text
编写源码和 plugin.yaml
  -> v4 静态校验、构建 UI 产物
  -> stage / pack / 可选签名
  -> 校验后原子安装到 .installed
  -> 管理员审查并授予声明能力、发布目录
  -> 用户“添加到我的插件”并生成配置目录
  -> 用户同意个人数据能力
  -> 宿主按资源 ACL 与三层授权创建受限 UI/Host 调用
```

“用户添加”不等于管理员授权，也不等于用户已经同意所有能力；升级后版本身份、声明用途或能力变化会使旧授权失效。
平台每次调用都重新检查插件版本、运行状态、管理员 Grant、用户 Consent 和业务资源 ACL。

## Runtime 的当前边界

v4 Manifest 可以声明 `none`、`wasm` 或 `container`。`none` 是当前 PDF Viewer 已有的可运行路径。Wasm/Container
需要受限 Runtime Adapter、资源限制、恢复与目标环境证据；在这些门禁未完成前，不应把其目录、Manifest 字段或计划设计表述为已交付的通用执行平台。

下一步：[v4 Manifest 与配置](/plugins/manifest)、[隔离 UI、Bridge 与 Gateway](/plugins/frontend-runtime)、
[三层授权与资源访问](/plugins/authorization-v3)。
