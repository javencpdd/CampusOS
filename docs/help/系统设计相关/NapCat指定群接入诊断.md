# NapCat 指定群接入诊断

> 更新时间：2026-10-28 02:42（Asia/Shanghai）
> 状态：操作员诊断工具及9项离线定向通过；本机947743079群就绪、1条测试文本发送与精确读回通过，见v1.2.172-dev。本工具不是已发布的CampusOS v5插件，尚未验收独立事件流。

当前CampusOS的Message Local仅作本地模拟，v5 Container仍禁止直接联网且没有消息扩展点。不能通过本工具的独立诊断推断QQ插件已经安装、账号已经绑定，或已提供群内知识/AI问答。

## 先完成现有机器人登录

打开现有NapCat WebUI（本机默认`http://127.0.0.1:6099/webui`），使用自己的现有账号完成QQ登录。管理服务运行不等于QQ已登录；`CheckLoginStatus`须返回`isLogin: true`，随后OneBot才能工作。

无需向Agent或其他人提供密码、WebUI令牌、QQ验证码或二维码内容。测试脚本只在进程内读取操作员指定的本地`webui.json`并申请自己的短期WebUI凭据，结束时注销自己的凭据；不把原令牌写入源码、命令行或报告。

## 只读检查

在仓库根目录运行，按本机实际位置调整配置目录：

```bash
python3 scripts/check-napcat-group.py \
  --config-dir "$HOME/qqbot/napcat/napcat/config" \
  --group 947743079
```

默认不发送消息。检查QQ登录、在线状态、当前机器人身份、指定群信息及机器人本人的群成员关系；不会读取群历史或枚举其他群成员。

## 显式发送一条测试文本并读回

只在已经授权测试的群使用`--send-test`：

```bash
python3 scripts/check-napcat-group.py \
  --config-dir "$HOME/qqbot/napcat/napcat/config" \
  --group 947743079 \
  --send-test \
  --output .cache/onebot-group-test/new-result.json
```

输出文件必须原先不存在，避免覆盖原始收据或在误重跑时再次发消息。只有QQ在线、群ID及机器人群成员关系全部核对后，才发送一条带`[CampusOS 接入测试]`及随机标记的纯文本，再按返回的消息ID读取并验证正文、发送者及消息类型。没有@全体、私聊、禁言、加好友或管理操作；发送结果未知或读回失败时不自动重发。

该路径复用本机NapCat已认证的调试API，不修改OneBot持久连接、监听地址、令牌或其他应用的反向WebSocket连接，不重启QQ/NapCat。它证明的目标是“本群发送与精确读回”；独立事件流、人类发指令、QQ到站内身份授权、CampusOS查询/问答和可安装v5插件仍需后续实现及验收。

| 退出码 | 含义 |
| --- | --- |
| 0 | 只读就绪或本群发送/读回通过，按报告中的status区分 |
| 2 | QQ尚未登录或需要操作员认证；没有发送测试消息 |
| 1 | 接口、群、消息、凭据清理或配置不变性检查失败；不得盲目重发 |

报告只保存结果、目标群、机器人ID末四位和需要时的消息ID，不保存令牌、群名、昵称、群消息正文或QR登录内容。退出前比对`webui.json`及所有`onebot11_*.json`的字节摘要，不自动覆盖并发操作员修改。

## 验证工具本身

```bash
python3 -m unittest scripts/test_napcat_group.py -v
```

离线测试使用合成群和机器人ID，不访问真实QQ。真实目标结果单独记录在对应进度和本机`.cache/`，不能把模拟测试当成机器人在线或消息已送达。
