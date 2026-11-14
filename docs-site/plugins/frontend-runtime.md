# 隔离 UI、Bridge 与 Gateway

> 更新时间：2026-11-15（Asia/Shanghai）；当前 v5 UI 合同为 `campusos.ui/v3` / `campusos.bridge/v1`。

插件的用户/管理 UI 是包内不可变产物，不进入宿主组件注册表。宿主按当前身份、包信任、安装代次、Grant/Consent 和配置证明签发最多 10 分钟的受限 UI 租约；资产访问每次重新验证。静态文件也不缓存，不能通过旧 URL 延续已撤销权限。

## 后台的插件管理界面

“打开插件管理界面”用于在后台内加载插件自带、与宿主隔离运行的页面。入口取决于当前不可变版本的 `ui` 声明：

| 当前插件 | 提供的界面 | 使用入口 |
| --- | --- | --- |
| rss-publisher 2.0.0 | 仅用户界面 | 用户端插件中心完成本人安装、用途同意和启用后打开 |
| pdf-viewer 3.0.0-dev.4 | 用户界面和管理界面 | 各身份对应入口；管理界面须先完成全局安装、有效审批和启用 |
| chat-echo、knowledge-reader、ai-knowledge | 无独立界面 | 功能由宿主调用；普通配置和密钥引用由宿主配置页管理 |

后台对仅用户界面或无界面的版本显示用途说明，不显示管理界面按钮。声明了管理界面但缺少安装、审批、启用或后端就绪条件时，按钮禁用并提示具体原因。点击可用入口后，宿主仍会核对当前授权和网关配置；用户界面不能作为管理界面加载。

## 隔离与连接

RSS 2.0.0 使用声明式 `isolation: "opaque"`，只借用本站网络地址加载签名资源，执行身份仍为独立 opaque origin；iframe 与响应 CSP 同时禁止同源权限，不能访问宿主 Cookie、存储、DOM、网络或 Worker。它无需通配 DNS、额外 UI 端口、本机助手或 Windows 下载。以下独立子域模式继续供未声明 opaque 的既有插件使用。

原生配置要求独立 UI 监听、`{isolation}` 子域 Origin 与精确父 Origin；Docker 开发默认 `http://<随机租约>.localhost:3003`，Node 网关只代理 API 内部资源监听。LAN/生产须配置专用通配 DNS/TLS，不能与 Web/Admin 共用 Origin。变量及 `rebuild` 规则见[配置与端口](/deployment/configuration#v5-插件与隔离界面)。

iframe sandbox 限定脚本及独立同源能力。资产 CSP 禁止网络连接、表单、子框架、对象和 base；父页 CSP 限制 iframe 导航。宿主核对准确 `event.source`、Origin、key/version、surface、audience，随机 challenge 后建立单次 MessagePort。导航、身份/上下文变化或期限到达会关闭旧 Port；迟到的响应不能流入新 iframe。

用户端从开始加载即计时，10秒内未建立连接会显示加载失败和“重新加载插件界面”，并列出需要由当前电脑访问的隔离地址。重试重新建立受限连接；身份变更、伪造握手等安全阻断保持拒绝。若反复失败，检查浏览器代理、通配域名解析及插件服务端口；传统子域模式仅连通3000端不代表3003端也已连通。

RSS 2.0.0 在普通网页中完成：添加订阅 → 读取 → 预览 → 确认导入 → 编辑器手动发布。抓取由通用服务器 Broker 完成，RSS 解析在插件 JS 中完成；原生助手已退役。

### Windows能打开IP地址，但打不开隔离域名

若3003端的IP健康地址可达，而嵌入该IP的 `sslip.io` / `nip.io` 地址打不开，域名解析或代理路径仍可能拦住插件。满足当前私有IPv4与隔离域名同IP条件时，加载失败区域会提供“下载 Windows 直连启动器”。

刷新网页后下载并双击 `.cmd` 文件，它会用已安装的Edge或Chrome打开独立的CampusOS窗口，为本站隔离域名指定正确IP并直接连接服务。普通浏览器及系统网络设置保持原样，插件仍使用原独立Origin和受限Bridge。首次在新窗口登录后，从相应插件入口重新打开。RSS 2.0.0 的 opaque 模式不需要此启动器。以后可双击启动器打开同一窗口；开发机IP或域名改变时重新生成文件。

消息最多 64 KiB，唯一请求最多 4096、并发最多 32、请求最多 30 秒，PDF Range 最多 1 MiB。基础读取实现空参数 `config.read`（普通值的精确 JSON，不含 Secret）、当前资源的 `resource.describe` / `resource.readRange`。SDK 中的保留方法名不产生授权；没有编译宿主适配器的方法明确拒绝。配置写入和新界面打开由宿主表单/用户动作控制。

v1.2.203 起，[RSS 图文插件](/plugins/rss-publisher)仅使用通用 config.read/config.update、web.read 和 editor.article.import。宿主不包含 RSS 解析、字段约定或插件名判断；图文导入包含用户确认与最多 8 图转存，单次上限 180 秒，普通请求仍 30 秒。图片预览只使用 Broker 返回的 Blob，不放开 iframe 网络或表单策略。

PDF 新标签页由宿主同源页面进行一次性内存交接并用真实 `/auth/me` 验证；URL、Cookie、持久存储和插件消息均不携带 JWT。完整握手、请求边界和资源复核见UI/Bridge 合同（仓库 `docs/api/v1.2插件UI宿主与Bridge合同.md`）。
