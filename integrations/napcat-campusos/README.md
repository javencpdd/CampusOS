# CampusOS QQ 文档助手 · OneBot 适配插件 0.1.0

> 更新时间：2026-10-28（Asia/Shanghai）

独立运行的 QQ 机器人适配插件，通过现有 NapCat 的 OneBot 11 正向 WebSocket 接收事件并回复；不是 NapCat 原生插件或 CampusOS v5 包，不能导入两者的插件管理页。本机 NapCat 构建只加载硬编码官方 ID 的原生插件，因此本例使用标准协议，不修改加载器或伪装官方 ID。其他 OneBot 实现尚未实测。

群内指令：`/campus 帮助`、`/campus ping`、`/campus 官网`、`/campus 搜索 插件`。支持指令前 @ 当前机器人。搜索随包八篇公开项目文档，最多返回三条来源；不保存群聊历史，不读取私人知识、不调用模型。`ping` 只检查适配插件，不代表平台 API 在线。

源码：`integrations/napcat-campusos/`。完整教程：仓库 `docs-site/plugins/qqbot-napcat-tutorial.md`，官网 `/plugins/qqbot-napcat-tutorial.html`。

运行需要 Linux、Node.js 20+（Node 20 加 `--experimental-websocket`）、已有 NapCat QQ 登录及一个仅回环监听、配置了随机 Token 的 OneBot 11 正向 WebSocket。运行时零额外 npm 依赖；开发检查还需要 Python 3 和仓库文档源。

在仓库根目录执行：

```bash
node integrations/napcat-campusos/tools/catalog.mjs
make qqbot-check
python3 integrations/napcat-campusos/tools/pack.py
```

ZIP 解压到独立目录，例如 `~/qqbot/campusos-qqbot/`，不要放进 NapCat 原生插件目录。复制 `config.example.json` 到 `~/.config/campusos-qqbot/config.json` 并设为 0600。默认关闭、空群白名单和空 Token；必须填写自己的群号、官网局域网地址、回环 WS URL 和与 NapCat 对应的随机 Token。然后：

```bash
node --experimental-websocket ~/qqbot/campusos-qqbot/runner.mjs \
  --config ~/.config/campusos-qqbot/config.json
```

可复制随包的 `campusos-qqbot.service.example` 为用户级 systemd 服务，执行 `systemctl --user daemon-reload` 和 `systemctl --user start campusos-qqbot`。示例不会自动启用开机自启，也不自动重启不确定发送。Token 在文件及内存中使用，不应放到命令行或提交仓库；它拥有 OneBot 的机器人操作权限，不是群范围受限凭据。本插件在应用层限制群范围，不能代替 NapCat 的凭据保护。

插件数据默认 `~/.local/share/campusos-qqbot/`：`status.json` 只有连接/故障状态和计数，`receipts.json` 只有 bot/group/message 组合摘要、时间与发送状态，`runner.lock` 防止同目录多实例。摘要不是抗枚举匿名化。24 小时内去重，上限 10,000 条；只接受五分钟以内的指令，每群间隔 1.5 秒、同群同用户间隔 5 秒，最多一个发送在途；超限静默丢弃。

回执损坏、容量耗尽、写入失败或发送结果不确定时停止回复，不自动重发；发送六秒无确认即不确定，外层八秒兜底。确认 QQ 侧结果后重启**本适配插件**以接收新指令，不能删除回执来重试旧消息。存储故障须先恢复。停用/升级用 `systemctl --user stop campusos-qqbot`，正常退出清除单实例锁；异常退出留下的锁须确认无存活实例后手动移除。已进入 NapCat 的发送不能被停止撤回。编辑配置后重启适配插件生效，正常情况下无需重启 QQ。

握手十秒超时，九十秒无帧关闭连接（NapCat 应保留三十秒心跳）。连接断开后每三秒重连；不会重发未确认动作，也不会自动清除业务故障。只新增一个本机 OneBot 监听，保留已有 AstrBot 等连接；没有新的免认证管理、消息注入或公网端口。卸载先停用并移除本适配服务，按需保留回执，再从 NapCat 删除本次增加的 WS 配置项。

许可见随包 CampusOS MIT LICENSE。按 OneBot 官方协议独立实现，没有复制 NapCat 插件模板或 AstrBot 源码。
