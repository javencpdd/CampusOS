# Plugin UI v2 开发与验收

> 适用基线：v1.1-dev  
> 更新时间：2026-09-22（Asia/Shanghai）
> 状态：本文是旧 `campusos.ui/v1/v2` 声明式合同的兼容说明。PDF Viewer 已迁至 v4 自包含包并使用 v3 隔离 iframe；当前实现与未完成项以[插件重构计划](../../项目计划书v1/项目计划v1.1/02-v1.1插件自包含与动态加载重构计划书.md)为准。

## 1. 解决什么问题

本文只解释仍受兼容代码支持的 v1/v2 声明式 Surface。它不是 PDF Viewer 的当前实现说明：
`builtin.pdf-viewer` 已被移除，当前只有自包含外部插件 `campusos.pdf-viewer`，使用 v4 包与 v3 隔离 iframe。
其实际加载、授权和运行边界见[插件平台与授权体系](../../architecture/模块设计/插件平台与授权体系.md)。

Plugin UI v2 把“插件想展示什么”与“浏览器怎样打开”分开。插件只能声明一个命名 `Surface` 并请求展示；
CampusOS 宿主根据当前设备、页面状态和安全策略选择 `modal`、`drawer`、`fullscreen` 或同源 `new-tab`。
这使 PDF 预览、受管数据详情等界面既能有弹窗和新标签页体验，也不会让插件取得任意 URL、`window.open()`、
主站路由控制权或用户 Token。

外部插件仍是声明式 schema，不会动态注入 JavaScript 到主站。`trusted-module` 只允许随 CampusOS 编译、
系统作用域且在可信白名单中的 Built-in Feature。

## 2. 最小 v2 示例

仓库中的 [v2-managed-example](../../../examples/plugins/v2-managed-example/plugin.yaml) 是可被测试直接解析的参考：

```yaml
ui:
  contract_version: campusos.ui/v2
  actions:
    - id: v2-managed-example.open-note
      kind: open-surface
      surface_id: v2-managed-example.note-preview
      presentation: modal
  surfaces:
    - id: v2-managed-example.note-preview
      version: v1
      type: record-preview
      layout_role: overlay
      renderer: schema
      schema: { component: stack }
      presentations: [modal, drawer, fullscreen, new-tab]
```

`open-surface` 不得包含 `method`、`path`、URL、HTML 或脚本。`surface_id` 必须指向同一 Manifest 已声明的
Surface，Action 所请求的展示方式必须在该 Surface 的 `presentations` 中。需要常规数据提交时仍使用受管
Extension Gateway Action，并由 Host API、Capability、Admin Grant 与 User Consent 共同判定。

## 3. 合同与生命周期

| 项目             | 规则                                                                                                       |
| ---------------- | ---------------------------------------------------------------------------------------------------------- |
| `campusos.ui/v1` | 保持已有 Route/Navigation/Slot/Surface/Action 兼容；未显式声明时按 v1 解析。                               |
| `campusos.ui/v2` | 每个 Surface 必须声明至少一个允许的 Presentation；宿主而非插件最终决定打开方式。                           |
| 新标签页         | 只能走同源路由和短期、服务端保存的 Invocation Context；不能把 JWT、对象路径或长期下载 URL 写入查询字符串。 |
| 停用、撤销、过期 | Surface 打开与内容读取均重新校验插件状态、文章/资产可见性与 Invocation；旧页面不构成继续访问授权。         |
| 崩溃/错误        | 宿主显示可理解的错误或下载降级，不把未受信任异常、请求体或私密上下文直接展示给用户。                       |

## 4. 开发者验收

```bash
go test ./internal/plugin -run 'TestManifestUIV2|TestV2ManagedExampleConformsToUIContract' -count=1
go run ./cmd/campusos-contracts --check
cd web && pnpm exec vue-tsc --noEmit && pnpm build
```

手工验证应覆盖 v1 已安装插件、v2 schema Surface、modal/drawer/fullscreen/new-tab、插件停用、授权撤销、
Invocation 过期和插件运行失败。浏览器矩阵、目标 Linux 环境和容量/恢复报告属于 Final 发布证据，不能用本地
单元测试替代。

## 5. PDF 文档预览：当前实现不使用 v2

`PDF 文档预览`当前唯一有效身份为外部、自包含的 `campusos.pdf-viewer`。它的页面、PDF.js Worker、Manifest 和配置定义
位于 `plugins/campusos.pdf-viewer/`，运行时只从校验后的 `.installed` release 读取。它只会显示在“外部插件运行管理”
及其面向用户的“用户目录与授权”投影中；不会作为“内置功能”或第二个 `builtin.pdf-viewer` 出现。

“用户目录与授权”只控制用户是否可见、请求和个人 Consent；导入/启停/管理员 Capability Grant 在“外部插件运行管理”中
完成。两个页面共享同一个当前安装快照，目录条目会在 API 启动同步时清除失效投影。

首次在线预览时，用户会被要求同意当前用途：文章附件使用 `article_attachment.self.preview`，个人空间“个人附件”
和“我的文档”的 owner-only PDF 使用 `personal_space_file.self.read`。同意可在“插件中心 → PDF 文档预览 → 查看并授权”撤销；管理员 Grant 被拒绝或
撤销、用户 Consent 被撤销、插件停用、文章下架或资产隔离后，创建和读取 Invocation 都会失败。用户无需生成密钥、
Token 或私钥。

普通用户的入口在图文文章详情页：登录后打开一篇**已发布**、且自己有访问权限的文章，文章附件卡片中的 PDF
会显示“预览”菜单，可选择弹窗、抽屉、全屏或新标签页。编辑页中的“上传附件”和“从我的附件添加”用于将 PDF、
音视频、Office 或压缩包作为附件绑定到文章；只有 PDF 在本版本提供在线预览，其他格式仍可安全下载。

个人空间的“个人附件”及“我的文档”中的 PDF 都会显示预览入口，但仅允许文件或文档 owner 使用。公开文章的有权读者可预览其附件 PDF，
并不因此取得作者个人空间或 Personal Documents 的下载权。个人文档预览只创建绑定当前用户和 document ID 的短期
Invocation；PDF.js 页面与 Worker 随插件 v4 release 发布到独立 Origin，浏览器可缓存插件代码，文件内容仍由认证 API
私有返回且不被持久缓存。

### v4 静态产物路径

隔离网关只服务 `/plugins/<key>/<version>-<digest>/...`，不会服务站点根目录 `/assets/...`。因此 v4 插件的 Vite 构建
必须使用相对 `base: './'`；打包器会将 `dist/user/`、`dist/admin/` 与共享的 `dist/assets/` 分别复制到 release 的
`ui/user/`、`ui/admin/`、`ui/assets/`。若入口 HTML 仍引用 `/assets/...` 或 release 缺少 `ui/assets/`，浏览器只能显示
“正在连接 CampusOS…”，因为 Bridge 客户端脚本尚未执行，并非授权被拒绝。

修改插件前端构建配置或 v4 打包器后需执行 Docker `rebuild`，普通宿主 Vue/Go 源码变更仍由现有热加载处理。
