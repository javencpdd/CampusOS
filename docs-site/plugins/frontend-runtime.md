# 隔离 UI、Bridge 与 Gateway

> 更新时间：2026-10-18（Asia/Shanghai）；当前 v5 UI 合同为 `campusos.ui/v3` / `campusos.bridge/v1`。

插件的用户/管理 UI 是包内不可变产物，不进入宿主组件注册表。宿主按当前身份、包信任、安装代次、Grant/Consent 和配置证明签发最多 10 分钟的独立子域租约；资产访问每次重新验证。静态文件也不缓存，不能通过旧 URL 延续已撤销权限。

原生配置要求独立 UI 监听、`{isolation}` 子域 Origin 与精确父 Origin；Docker 开发默认 `http://<随机租约>.localhost:3003`，Node 网关只代理 API 内部资源监听。LAN/生产须配置专用通配 DNS/TLS，不能与 Web/Admin 共用 Origin。变量及 `rebuild` 规则见[配置与端口](/deployment/configuration#v5-插件与隔离界面)。

iframe sandbox 限定脚本及独立同源能力。资产 CSP 禁止网络连接、表单、子框架、对象和 base；父页 CSP 限制 iframe 导航。宿主核对准确 `event.source`、Origin、key/version、surface、audience，随机 challenge 后建立单次 MessagePort。导航、身份/上下文变化或期限到达会关闭旧 Port；迟到的响应不能流入新 iframe。

消息最多 64 KiB，唯一请求最多 4096、并发最多 32、请求最多 30 秒，PDF Range 最多 1 MiB。当前仅实现空参数 `config.read`（普通值的精确 JSON，不含 Secret）、当前资源的 `resource.describe` / `resource.readRange`。SDK 中的保留方法名不产生授权；没有编译宿主适配器的方法明确拒绝。配置写入和新界面打开由宿主表单/用户动作控制。

PDF 新标签页由宿主同源页面进行一次性内存交接并用真实 `/auth/me` 验证；URL、Cookie、持久存储和插件消息均不携带 JWT。完整握手、请求边界和资源复核见UI/Bridge 合同（仓库 `docs/api/v1.2插件UI宿主与Bridge合同.md`）。
