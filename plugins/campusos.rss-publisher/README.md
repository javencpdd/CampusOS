# RSS Publisher 2.0.0

> 最新更新时间：2026-11-15（Asia/Shanghai）

独立 UI-only v5 插件。订阅槽位、RSS 2.0/RSS 1.0/Atom 解析、列表和预览均由包内 JS 实现；宿主只提供 config.read/config.update、web.read 和 editor.article.import 标准接口。更换发布者和 key 不需要修改宿主。

UI 声明 isolation:opaque，以本站只读资产接口加载、在双重沙箱中运行，不依赖通配 DNS、本机抓取助手或 Windows 启动器。插件无 Cookie/JWT/宿主 DOM/文件路径权限。公开读取由服务器 Broker 执行，禁止私网、凭据、重定向和超限文件；图片预览使用 Blob，导入由宿主确认并转存后进入原生编辑器，发布仍需本人点击。

在仓库根目录构建：

```bash
node plugins/campusos.rss-publisher/frontend/build.mjs
node --test plugins/campusos.rss-publisher/frontend/model.test.mjs
go run ./cmd/campusos-plugin-v5-check -root plugins
```

构建只写本插件 dist 和 plugin.json 的摘要，identity.js 由该清单生成。安装、启停、升级和卸载通过平台受管接口，不修改宿主源码、锁文件、二进制或核心 schema。

从 1.0.0 升级须发布新的不可变签名包，管理员批准新能力、本人同意当前用途。保留 feed_1 至 feed_6 配置结构；旧版本专用接口和助手已退役，不能覆盖旧版本字节。详见[用户说明](../../docs-site/plugins/rss-publisher.md)和[通用接口合同](../../docs/api/v1.2插件通用网页读取与编辑器交接合同.md)。

## 服务器与本机代理的权限

本版本的读取位置是 CampusOS 服务器，界面只读说明这一点。服务器代理地址、认证、是否启用以及活动线路全部由管理员统一配置。用户插件 UI 不提供服务器代理开关、目录查询或线路选择；普通配置也不保存 proxy_profile 等服务器策略字段。

管理员可在部署配置中注册 HTTP/HTTPS/SOCKS5 端点（CAMPUSOS_PLUGIN_WEB_PROXY_PROFILES），再用 CAMPUSOS_PLUGIN_WEB_PROXY_PROFILE 选定活动编号；空编号表示管理员选择直连。策略统一用于订阅、预览图片及导入图片。代理不可用时请求失败，不绕过管理员策略自动直连；代理地址和口令不传给插件。

127.0.0.1 指 API 所在机器或容器，Windows 电脑的回环地址不能代替服务器地址。本机助手则是在用户电脑发请求，其代理应由本人配置并只作用于该客户端；旧助手没有代理配置入口且已退役，2.0.0 只提供免下载的服务器读取。若未来恢复可选本机读取，必须采用独立客户端与标准能力，不能把用户代理写到服务器或恢复宿主 RSS 注入。

部署示例及地址说明见 [通用读取合同](../../docs/api/v1.2插件通用网页读取与编辑器交接合同.md)。普通网页 JS 不能设置任意 HTTP/SOCKS 隧道，也不能绕过 CORS；仅打开浏览器代理不代表其他客户端进程自动使用它。
