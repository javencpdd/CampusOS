# 开发一个 QQ 文档助手适配插件

> 更新时间：2026-10-28（Asia/Shanghai）
> 示例：`campusos-qqbot` 0.1.0；本次目标为 Linux 上已有 NapCat 的 QQ 机器人。

本教程对应 `integrations/napcat-campusos/` 的实际代码：独立运行的 OneBot 11 适配插件，在指定 QQ 群查询随包公开项目文档，返回来源链接。运行不依赖 CampusOS API、数据库或模型，链接是否可打开取决于官网部署。

## 1. 先验证宿主合同

CampusOS v5 插件没有已发布的 IM 消息入口；不能为了 QQ 放开其网络隔离。本机 NapCat 构建的原生加载器又有硬编码官方 ID 白名单，会拒绝自定义原生插件。不要改加载器，也不要冒用官方 ID。

因此本例是**独立 OneBot 适配插件**，使用现有 QQ 登录，通过仅回环监听的正向 WebSocket 接入。它不是 CampusOS v5 包，也不是 NapCat 原生包，不能导入二者的插件管理页。源码放在 `integrations/`，`plugins/` 继续保留给需要通过 v5 Manifest 检查的平台插件。

选择标准协议便于以后适配其他实现，但本次没有实测 SnowLuma，不宣称已经兼容。原生插件接口与 OneBot 协议也不能混为一谈。

## 2. 定义最小闭环

| 指令 | 行为 |
| --- | --- |
| `/campus`、`/campus 帮助` | 展示指令和公开数据范围 |
| `/campus ping` | 确认适配插件与目录就绪；不代表平台 API 或模型在线 |
| `/campus 官网` | 返回官网和本教程入口 |
| `/campus 搜索 插件` | 搜索随包目录，最多返回三条标题、简介和来源 |

支持指令前 @ 当前机器人。私聊、其他群、自身消息、普通聊天、图片和转发不触发。关键词按空格拆分，全部词均命中才返回；标题命中优先。它是项目文档导航，不是 AI 或校园业务问答。

## 3. 分离纯逻辑与副作用

```text
integrations/napcat-campusos/
├── core.mjs              # 配置、群事件解析、关键词检索及回复
├── bot.mjs               # 去重、限流及发送状态
├── receipts.mjs          # 持久回执
├── onebot.mjs            # WS、echo 关联、断线和动作超时
├── runner.mjs            # 私有配置、单实例锁、状态、正常退出
├── catalog.json          # 随包公开文档目录
├── tools/catalog.mjs     # 源文档白名单和摘要一致性检查
├── tools/pack.py         # 仅运行文件打包
└── tests/                # 合成事件、协议帧和故障注入
```

运行要求为 Node.js 20+；Node 20 加 `--experimental-websocket` 使用其内置客户端，无需 npm 运行依赖。开发检查另需 Python 3 和本仓库。当前目标环境为 Linux，尚未验证独立运行器在 Windows 上的权限/服务行为。

OneBot `/` 连接同时接收事件与动作结果，发送的动作只有：

```js
{
  action: 'send_group_msg',
  params: {
    group_id: verifiedGroup,
    message: [{ type: 'text', data: { text: replyText } }]
  },
  echo: randomCorrelationId
}
```

群号来自入站事件与配置白名单交集。回复使用结构化纯文本，不解析 CQ 码，不支持私聊、任意媒体、@ 全体或用户指定发送目标。响应必须匹配当前 `echo`、`status=ok`、`retcode=0` 和有效消息 ID 才确认完成。

## 4. 默认关闭与本机连接

`config.example.json` 默认业务关闭、群白名单为空、Token 为空，不能直接投入运行。实际配置放在仓库外，例如 `~/.config/campusos-qqbot/config.json`，权限必须为 0600：

```json
{
  "bot": {
    "enabled": true,
    "allowedGroups": "你的测试群号",
    "siteUrl": "http://192.168.1.10:3002/",
    "prefix": "/campus"
  },
  "connection": {
    "url": "ws://127.0.0.1:6700/",
    "accessToken": "填写你自己生成且与NapCat一致的随机Token"
  }
}
```

上面是结构示例，群号和 Token 占位符不会通过验证。生成至少 32 字节随机值，在 NapCat WS 配置与本地配置中填写同一值，不发到群里或提交仓库。这里只接受显式端口的 `127.0.0.1`/`::1` WS 地址，拒绝外部主机、URL 凭据、查询参数及片段。官网则使用群成员可访问的 HTTP(S) 地址，不能填 `localhost`；IP 变化后须更新配置并重启本适配服务。

**OneBot Token 拥有机器人的接口权限，并非群范围受限凭据**。本插件的群白名单只是应用层约束；本机配置与进程仍属于可信运维边界。Token 仅在文件和内存中使用，客户端按协议在握手时传入，不输出 URL 或原始提供方异常。WebUI 登录 Token 与 OneBot Token 是两个用途，不需要将 WebUI Token 交给插件。

## 5. 只索引明确公开的文章

`tools/catalog.mjs` 明确列出八篇 `docs-site/` 文档，记录源 SHA-256，不扫描配置、进度证据或用户文件。新增文档时修改白名单与标题/简介，然后：

```bash
node integrations/napcat-campusos/tools/catalog.mjs
node integrations/napcat-campusos/tools/catalog.mjs --check
```

源文件与目录不同步时 `--check` 失败。搜索来源必须来自目录内固定路由，用户关键词不会拼接进链接。更新文档后重新生成、测试、打包和替换插件；机器人不会自动抓取官网。

## 6. 去重、限流和不确定发送

流程为：校验五分钟以内的白名单群事件 → 查重复 → 检查限流 → 持久化 `pending` → 发送 → 保存 `sent` 或 `uncertain`。回执写在发送之前，因此进程崩溃或重启不会自动重发旧动作；不声称分布式“恰好一次”。

回执包含 bot/group/message 组合 SHA-256、时间及状态，不含正文或用户 ID，摘要也不是抗枚举匿名化。保留 24 小时、最多 10,000 条。每群至少间隔 1.5 秒，同群同用户至少间隔 5 秒，一次只允许一个发送在途；超限静默丢弃，不排队补发。

动作六秒无确认时拒绝，业务层八秒兜底；发送不确定、回执损坏、写入失败或满容量时停止回复。先确认 QQ 侧实际结果，再重启本适配插件接收**新**指令；不能删除回执重试旧消息。连接握手十秒超时，九十秒无帧关闭连接；NapCat 应保留三十秒心跳。断线每三秒重连，不清除业务故障、不重发动作。停用能阻止新派发，已经交给 NapCat 的发送无法撤回。

## 7. 分路径立即测试

```bash
node --test integrations/napcat-campusos/tests/core.test.mjs
node --test integrations/napcat-campusos/tests/delivery.test.mjs
node --test integrations/napcat-campusos/tests/onebot.test.mjs
make qqbot-check
```

测试覆盖默认关闭、范围/类型/时间拒绝、来源排序、去重、并发/限流、故障与超时、WS 响应关联、重连、单实例锁和可重现打包。合成协议帧不证明实际 QQ 入站；目标环境须单独验证连接、发送/读回以及真人群指令，不用自身消息冒充真人事件。

## 8. 打包、接入和部署

```bash
python3 integrations/napcat-campusos/tools/pack.py
```

生成 `.cache/qqbot-plugin/campusos-qqbot-0.1.0.zip`，拒绝覆盖已有文件。ZIP 只有十一份白名单运行文件及示例、README、MIT LICENSE，不携带实际配置、回执或测试数据。

1. 在 NapCat WebUI 的网络配置新增正向 WS 服务：名称自定、`host=127.0.0.1`、空闲端口如 `6700`、消息数组格式、关闭自身消息上报、关闭 debug、设置随机 Token，然后启用。保留原来的 AstrBot 反向连接，不覆盖配置列表。当前本机版本可以热应用该新增项，无需重启 QQ。
2. 将 ZIP 解压到独立目录，如 `~/qqbot/campusos-qqbot/`。不要放进 NapCat 原生插件目录。
3. 创建上节的私有配置并设置 `chmod 600`。官网应已启动且群成员可访问。
4. 前台运行：

```bash
node --experimental-websocket ~/qqbot/campusos-qqbot/runner.mjs \
  --config ~/.config/campusos-qqbot/config.json
```

5. 常驻运行可把随包 `campusos-qqbot.service.example` 复制为 `~/.config/systemd/user/campusos-qqbot.service`，核对 Node 路径、安装及配置路径，再执行 `systemctl --user daemon-reload`、`systemctl --user start campusos-qqbot`。示例没有自动重启或自动开机自启，按部署需要另行选择。
6. 群成员发送 `/campus ping`，等五秒，再发送 `/campus 搜索 插件`；检查真实回复、来源网页与其他群无响应。QQ 机器人自身消息按设计不触发。

## 9. 运维与后续扩展

默认数据目录 `~/.local/share/campusos-qqbot/`：`status.json` 返回连接、业务故障码和计数；`receipts.json` 保留去重；`runner.lock` 防止同数据目录多实例。状态中不包含 Token、群号、用户 ID 或消息正文。正常 SIGTERM/SIGINT 清理锁；异常退出后须先确认原实例已结束，再手动移除残留锁，不可同时启动第二个实例。

停用使用 `systemctl --user stop campusos-qqbot`。配置变更与升级都先停止本服务、保留配置/回执，再替换并启动。卸载时移除本服务及自己添加的 NapCat WS 项，其他连接保持。插件不提供 HTTP 管理/注入接口，也不对局域网开放 OneBot 端口。

若下一片接平台公开知识 API，先确定 Integration Principal、端点/来源 Scope、实时公开性和引用校验，不能读取平台数据库或借用管理员会话。私人知识、模型问答和业务写操作另外设计与验收。

协议依据：[OneBot 正向 WebSocket](https://github.com/botuniverse/onebot-11/blob/master/communication/ws.md)、[鉴权](https://github.com/botuniverse/onebot-11/blob/master/communication/authorization.md)。原生插件是另一条路线，可参考 [NapCat 官方生命周期](https://napneko.github.io/develop/plugin/structure)；本例按协议独立实现，没有复制其模板或 AstrBot 源码。
