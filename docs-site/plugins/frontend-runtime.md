# 隔离 UI、Bridge 与 Gateway

> 更新时间：2026-09-23（Asia/Shanghai）
> 合同：`campusos.ui/v3`、`campusos.bridge/v1`

v4 插件页面不再编入 `web/src/modules` 或 `admin/src/modules`。宿主负责登录态、路由外壳、弹层、焦点、中文错误状态、
权限和销毁；插件只在校验后的 release 中提供自己的静态页面。

## 加载路径

```text
用户在 3000 / 管理员在 3001 点击宿主入口
  -> 宿主验证资源 ACL、release、Grant、Consent 和 Surface
  -> API 创建短期 Invocation / UI 会话
  -> 宿主打开受控 modal、drawer、fullscreen、page 或 new-tab 外壳
  -> iframe 从 3003 的不可变 release Origin 加载插件入口
  -> 精确 origin + source + challenge 校验后建立 MessagePort
  -> 插件仅经 Bridge 请求已声明的方法
```

在 Docker 开发模式，`plugin-ui` 服务只从 `plugins/.installed/` 提供 `/plugins/<key>/<version>-<digest>/...`。
它拒绝源码目录、暂存目录、任意上级路径和非插件 URL；浏览器不能把 `api:8080` 这样的 Docker 内部服务名当成插件地址。
局域网预览时，3003 必须与 3000/3001 同样在可信网络范围开放。

## v3 Surface

Manifest 只声明 Surface，不能声明任意 URL 或修改宿主路由：

```yaml
ui:
  user:
    surfaces:
      - id: preview
        route: preview
        title: PDF 预览
        presentations: [modal, drawer, fullscreen, new-tab]
```

宿主检查 presentation 是否在白名单中，并在插件停用、升级、退出登录或授权撤销时清理 iframe、监听器与待处理请求。
用户通过点击触发新标签页；URL 不携带 JWT、磁盘路径、`storage_key` 或长期下载链接。

## Bridge 是受限 RPC，不是浏览器代理

Bridge 握手要求精确 `event.origin`、`event.source === iframe.contentWindow`、插件 key、不可变版本、Surface、audience 与随机
challenge 均匹配。握手后只使用转交的 `MessagePort`；iframe reload 后旧 Port 失效。

当前冻结方法：

| 方法 | 用途 | 不代表的权限 |
| --- | --- | --- |
| `config.read` / `config.update` | 读取或更新宿主拥有的插件配置 | 不读取任意用户目录或 Secret 明文。 |
| `resource.describe` / `resource.readRange` | 读取当前 Invocation 的受限资源元数据和字节范围 | 不接受文件路径、对象 ID 猜测或任意下载 URL。 |
| `records.read` / `records.write` | 访问平台已开放的 owner-scoped 通用记录 | 不创建插件专属平台表。 |
| `backend.invoke` | 调用已登记的插件后端能力 | 不提供任意 HTTP/SQL 代理。 |
| `ui.requestSurface` | 请求宿主打开已声明 Surface | 不可强制弹窗、跳转或直接 `window.open`。 |

单条 Bridge 消息上限 64 KiB；PDF `resource.readRange` 单段最多 1 MiB。所有消息即使通过前端校验，服务端仍会重新认证、
检查 release、三层授权和业务资源 ACL。

## 缓存与安全

- release URL 含版本和 digest，静态 JS、CSS 与 Worker 可以使用 immutable HTTP 缓存；浏览器仍可能驱逐缓存。
- 受保护 PDF 字节不进入 Service Worker、IndexedDB 或共享缓存；插件也不能注册 Service Worker。
- 网关和宿主保留 CSP、frame-ancestors、路径白名单、消息大小、超时与错误审计边界。
- iframe/CSP 不是 DRM。对插件的信任仍来自包校验、发布者审核、最小能力和每次服务端授权。

实际实现请结合 [PDF Viewer 入门教程](/plugins/pdf-viewer-tutorial) 阅读 `frontend/shared/bridge.ts` 与
`frontend/user/main.ts`。旧 `campusos.ui/v2` 合同见 [历史说明](/plugins/ui-v2)。
