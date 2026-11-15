# RSS Publisher 2.1.0

> 最新更新时间：2026-11-15（Asia/Shanghai）

独立 UI-only v5 插件。订阅槽位、RSS 2.0/RSS 1.0/Atom 解析、列表和预览均由包内 JS 实现；宿主提供配置、公开读取、通用本机助手及编辑器交接标准接口。更换发布者和 key 不需要修改宿主，安装/卸载不改宿主源码、依赖锁文件、二进制或核心 Schema。

UI 使用 opaque 双重沙箱，通过本站地址加载，无需通配 DNS 或 Windows 直连启动器。默认服务器读取无需下载；可选本机读取使用独立的 CampusOS 通用助手，不能读取宿主 Cookie/JWT/DOM，也不能修改服务器代理。

## 构建与升级

```bash
node plugins/campusos.rss-publisher/frontend/build.mjs
node --test plugins/campusos.rss-publisher/frontend/model.test.mjs
go run ./cmd/campusos-plugin-v5-check -root plugins
```

构建只写本插件 dist 和清单摘要，identity.js 由清单生成。2.1.0 增加可选 web.local.read，需新签名包、管理员批准及本人用途同意；已有 1.x/2.0.0 包字节不可覆盖。原 feed_1 至 feed_6 配置保持，读取位置不写入订阅配置。

## 两种读取方式

| 方式 | 网络及代理由谁控制 | 使用步骤 |
| --- | --- | --- |
| CampusOS 服务器 | 管理员统一维护服务器出站策略 | 普通浏览器直接读取，无需助手 |
| 本机助手 | 用户自己的助手进程配置本机 HTTP/HTTPS/SOCKS5 代理 | 连接助手、输入单次配对码，再明确选择自己的电脑 |

选择本机后，订阅、图片预览及导入图片都经过同一助手；失联不会切回服务器。127.0.0.1 在助手中指用户电脑，在服务器配置中指 API 所在机器/容器。代理地址和口令只留在助手本地，服务端仅显示代理协议；没有系统/浏览器/Clash 配置修改。

1. 在插件点击“连接本机助手”，经宿主确认生成一次性配对码。
2. 下载与自己电脑匹配的通用助手，启动并输入本站地址、本机代理和配对码。Windows 可直接运行 exe；Linux/macOS 从终端启动。
3. 在插件“读取位置”中选择在线电脑，并确认切换，然后读取订阅。
4. 本人配置保存后保留选择与连接。登录凭据刷新轮换、退出、停用或撤权后旧连接失效；界面保留不可用选择，需重新配对或明确切回服务器。

程序由部署者独立执行 python3 scripts/build-web-companion.py 构建，平台只下载固定文件，不在插件安装时构建或运行。详见[通用助手合同](../../docs/api/v1.2本机公开资源读取助手合同.md)、[读取及编辑器合同](../../docs/api/v1.2插件通用网页读取与编辑器交接合同.md)与[用户说明](../../docs-site/plugins/rss-publisher.md)。
