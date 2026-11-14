# 隔离 UI、Bridge 与 Gateway

> 更新时间：2026-11-14（Asia/Shanghai）；当前 v5 UI 合同为 `campusos.ui/v3` / `campusos.bridge/v1`。

插件的用户/管理 UI 是包内不可变产物，不进入宿主组件注册表。宿主按当前身份、包信任、安装代次、Grant/Consent 和配置证明签发最多 10 分钟的独立子域租约；资产访问每次重新验证。静态文件也不缓存，不能通过旧 URL 延续已撤销权限。

原生配置要求独立 UI 监听、`{isolation}` 子域 Origin 与精确父 Origin；Docker 开发默认 `http://<随机租约>.localhost:3003`，Node 网关只代理 API 内部资源监听。LAN/生产须配置专用通配 DNS/TLS，不能与 Web/Admin 共用 Origin。变量及 `rebuild` 规则见[配置与端口](/deployment/configuration#v5-插件与隔离界面)。

iframe sandbox 限定脚本及独立同源能力。资产 CSP 禁止网络连接、表单、子框架、对象和 base；父页 CSP 限制 iframe 导航。宿主核对准确 `event.source`、Origin、key/version、surface、audience，随机 challenge 后建立单次 MessagePort。导航、身份/上下文变化或期限到达会关闭旧 Port；迟到的响应不能流入新 iframe。

用户端从开始加载即计时，10秒内未建立连接会显示加载失败和“重新加载插件界面”，并列出需要由当前电脑访问的隔离地址。重试重新建立受限连接；身份变更、伪造握手等安全阻断保持拒绝。若反复失败，检查浏览器代理、通配域名解析及插件服务端口；只连通3000端的网页或本机抓取助手，不代表3003端的隔离界面已经连通。

RSS本机助手连接后继续在网页中操作：选择或添加订阅 → 读取最新内容 → 预览图文 → 导入编辑器核对并手动发布。终端保持开启并等待抓取任务，不会另外弹出编辑窗口；“助手在线”和“订阅界面连接成功”是两个独立状态。

### Windows能打开IP地址，但打不开隔离域名

若3003端的IP健康地址可达，而嵌入该IP的 `sslip.io` / `nip.io` 地址打不开，域名解析或代理路径仍可能拦住插件。满足当前私有IPv4与隔离域名同IP条件时，加载失败区域会提供“下载 Windows 直连启动器”。

刷新网页后下载并双击 `.cmd` 文件，它会用已安装的Edge或Chrome打开独立的CampusOS窗口，为本站隔离域名指定正确IP并直接连接服务。普通浏览器及系统网络设置保持原样，插件仍使用原独立Origin和受限Bridge。首次在新窗口登录后，从插件入口重新打开RSS；关闭原助手进程，再用新窗口的配对码重新运行并配对助手，随后选择该电脑读取。以后可双击启动器打开同一窗口；开发机IP或域名改变时重新生成文件。

消息最多 64 KiB，唯一请求最多 4096、并发最多 32、请求最多 30 秒，PDF Range 最多 1 MiB。当前仅实现空参数 `config.read`（普通值的精确 JSON，不含 Secret）、当前资源的 `resource.describe` / `resource.readRange`。SDK 中的保留方法名不产生授权；没有编译宿主适配器的方法明确拒绝。配置写入和新界面打开由宿主表单/用户动作控制。

PDF 新标签页由宿主同源页面进行一次性内存交接并用真实 `/auth/me` 验证；URL、Cookie、持久存储和插件消息均不携带 JWT。完整握手、请求边界和资源复核见UI/Bridge 合同（仓库 `docs/api/v1.2插件UI宿主与Bridge合同.md`）。
