# v0.11 可靠任务与 Webhook 安全运维

> 适用版本：`v0.11.0`  
> 面向：后台管理员、值班维护者、接收 Webhook 的服务开发者。

这份文档只解释已经实现的 v0.11 运行能力。它不会要求你直接修改 Outbox 或审计表。
日常操作从管理后台开始，数据库只用于经过批准的故障排查。

## 1. 先理解两个页面

| 页面 | 地址 | 用途 |
| --- | --- | --- |
| 可靠任务 | `http://localhost:3001/reliability` | 查看队列、Worker、可恢复操作、命令审计关联、兼容路径和保留预演。 |
| 集成中心 | `http://localhost:3001/integrations` | 配置 Webhook endpoint、查看 endpoint 的投递历史、执行一次安全测试。 |

“可靠任务”是受保护的后台页面。普通管理员也必须拥有实际 Permission Code，不能只因
页面菜单可见就获得重放或保留预演权限：

| Code | 用途 |
| --- | --- |
| `platform.reliability.read` | 查看队列、Worker、操作、命令关联和兼容遥测。 |
| `platform.reliability.replay` | 重放已进入 dead-letter 的事件。 |
| `platform.retention.preview` | 计算并保存保留策略 dry-run。 |

## 2. 日常查看顺序

1. 打开“可靠任务 -> 事件队列”，先看“待处理”“重试中”“失败队列”和“最早待处理”。
2. 如果待处理持续增加，再打开“Worker 与操作”，确认是否有最近心跳。
3. 点击事件行的列表图标查看每个消费者的投递尝试。不要从错误全文猜测业务状态。
4. 查看“可靠命令审计”，用命令、资源、request ID 和 event ID 串联授权或治理操作。
   此页不会显示请求体、Outbox payload 或 Secret。
5. 在“兼容与保留”查看旧路径调用量。调用量不为零时，不要删除旧表、旧目录或兼容
   接口。

每张列表底部都有独立分页。切换筛选条件会回到第一页；消费尝试只分页显示当前选择
事件的记录。页面查询默认受每个管理员每分钟 `120` 次的单进程限流保护。正常手工
操作不会触发；自动化客户端收到 `429` 时必须等待响应中的 `Retry-After`，不能立即
循环重试。

事件状态的含义：

| 状态 | 含义 | 处理 |
| --- | --- | --- |
| `pending` | 已提交，等待 Worker | 短暂出现正常；持续增长时检查 Worker 与数据库。 |
| `processing` | 已被一个 Worker 用 lease 领取 | 不要手工改成功；等待完成或 lease 过期。 |
| `retry` | 可恢复失败，等待下次可用时间 | 阅读尝试记录和接收方日志。 |
| `published` | 所有已注册消费者已确认 | 无需操作。 |
| `dead` | 达到最大次数或永久失败 | 修复原因后才考虑一次重放。 |

`attempts` 是事件 Claim 次数，不是消费者数。`7/8` 表示还可以进行第 8 次领取；`8/8`
不会被领取第 9 次。消费尝试中的 `skipped` 表示对应 Consumer Receipt 已存在，真实副作用
不会再执行；它不是错误。`system:outbox-finalize` 用于说明所有消费者结束后，主 Outbox
记录是否成功转为 published。

页面会直接标记三类异常：

- “尝试次数越界”：历史 attempts 大于 max，部署修复后会在下一次 Claim 维护阶段进入 dead。
- “处理租约已过期”：processing 的 lease 已失效，可由下一 Worker 在未耗尽时安全领取。
- “已记录消费者均完成，事件尚未最终化”：消费者已有成功/跳过证据，但 Complete 尚未成功。

详情只显示 lease、次数、时间和脱敏错误，不显示 payload、headers、幂等键、邮箱、验证码、
Token、Secret 或 operation details 原文。

## 3. 正确处理 dead-letter

重放不是“再试一次”按钮，而是可能再次触发外部副作用的高风险动作。按以下步骤操作：

1. 在事件队列筛选“失败队列”。
2. 打开该事件的消费尝试，确认失败来自哪一个 consumer，以及是否是 endpoint、
   DNS、签名、接收方 `4xx` 或限流问题。
3. 修复根因。例如修复接收服务、恢复 DNS、更新允许的公网地址，或在隔离环境中
   调整 endpoint 配置。
4. 确认接收方会按 `X-CampusOS-Event-ID` 幂等处理重复请求。
5. 点击重放，阅读确认文字。系统生成 `Idempotency-Key`，同一重放请求重复提交不会
   重置事件两次。
6. 刷新队列和尝试记录，确认最终进入 `published` 或再次留下清晰的 `dead` 证据。

只能重放 `dead` 事件。`pending`、`processing`、`retry` 和 `published` 返回冲突是
正常安全限制。不要通过 SQL 把状态改成 `pending`，这会绕过 command audit、lease 和
幂等记录。

对于旧版本产生的 `97/8`、`103/8` 一类事件，先部署修复并让 Worker 完成一次 Claim 周期，
确认事件自动进入 dead 且 attempts 原值保留。随后按事件逐条核对 Receipt 和 Attempt，再使用
后台 Replay。Replay 不删除 Receipt，已成功的邮件和 EventBus 消费者会 skipped，修复后的
finalize 会把事件转为 published。完整安全查询见
[v0.12 可靠事件异常诊断与安全恢复](v12可靠事件异常诊断与安全恢复.md)。

## 4. 配置 Webhook

1. 打开“集成中心 -> Webhook”。
2. 填写容易识别的名称和 HTTPS 地址，选择订阅事件。
3. 按接收服务容量设置最大并发与每分钟速率；默认值适合低风险内部集成，不能当作
   集群级流量保护。
4. 保存后执行一次“测试”。测试也受 SSRF、防重定向、DNS、超时和响应大小限制，
   但它是直接探测，不会自动重试或进入 Outbox。
5. 在接收服务实现 v1 签名校验、timestamp 时窗和 event ID 去重后，再启用长期投递。
6. 从“投递记录”和“可靠任务”共同确认实际的成功、重试与 dead-letter。

生产环境保持：

```dotenv
WEBHOOK_ALLOWED_HOSTS=hooks.example.edu,events.example.edu
WEBHOOK_ALLOW_PRIVATE_NETWORK=false
```

不要为方便访问内网服务而在生产设为 `true`。需要内网集成时，应通过受控 egress、
专用中继或隔离网络设计解决。

## 5. 保留策略预演

v0.11 没有后台物理删除按钮。“兼容与保留”页只能：

1. 选择一个支持的目标，例如 Outbox、授权审计、Webhook 投递或插件日志；
2. 选择截止时间；
3. 点击搜索图标计算候选数量；
4. 点击保存图标记录 dry-run。

`CanDelete=false` 是预期结果。正式清理需要后续版本单独提供分批、上限、审批、
备份、恢复和对账方案；不要把 dry-run 记录理解为系统已经删除数据。

## 6. 故障排查与恢复

```bash
make reliability-check
make outbox-check
make failure-injection-check
make database-check
./scripts/test-v12-reliability-worker-convergence-migration.sh
```

`make database-check` 已包含 `scripts/test-v11-reliability-migration.sh`。该脚本在隔离
数据库执行 `000001-000026 -> 000027 up -> 000027 down -> 000027 up`，验证可靠性表、
权限、Webhook 列和历史 endpoint 数据的保留。它不是生产数据库回滚命令。

| 现象 | 优先检查 | 不要做 |
| --- | --- | --- |
| queue 持续积压 | Worker 心跳、migration `000027`、数据库连接、应用日志 | 不要清空 Outbox。 |
| 单事件持续 retry | attempt 记录、接收方状态、`Retry-After`、endpoint 限制 | 不要反复点击测试替代重放。 |
| 消费者 skipped 但 processing 反复出现 | finalize Attempt、lease generation、部署版本和状态转换日志 | 不要删除 Receipt 或直接写 published。 |
| 被拒绝的 URL | 域名、DNS 解析、redirect、`WEBHOOK_ALLOWED_HOSTS` | 不要全局打开私网访问。 |
| 插件/风格包操作 failed | 操作记录、staging、版本 snapshot 和磁盘权限 | 不要直接覆盖正式目录。 |
| 需回退部署 | 先停止新 producer，等待/冻结 lease，保留 Outbox 和审计 | 不要将 `processing` 直接写成成功。 |

如果 Admin 页面本身能打开但提示“可靠任务加载失败”，先执行：

```bash
curl -fsS http://localhost:8080/api/v1/health
tail -n 120 .campusos/logs/api.log
```

前端开发服务器可在 API 启动失败时继续提供静态页面，因此“页面能打开”不代表后端已
完成模块装配。正常启动日志必须包含 `CampusOS API 监听`，可靠任务请求应返回 `200`。

详见 [可靠命令与数据所有权](../../architecture/v11可靠命令事件与数据所有权.md) 和
[Webhook 安全模型](../../architecture/v11Webhook可靠投递与安全模型.md)。
