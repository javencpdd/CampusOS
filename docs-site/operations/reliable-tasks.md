# 可靠任务与 Webhook 运维

本页对应管理后台：

```text
http://localhost:3001/reliability
```

CampusOS v0.11 将高风险数据库命令的业务写入、必要审计和事件记录放到同一事务；
提交后的事件由持久 Worker 处理。这个页面用于排查积压、重试、失败队列和可恢复的
文件/资源操作，不用于直接改数据库状态。

## 先确认权限

| 权限 | 能做什么 |
| --- | --- |
| `platform.reliability.read` | 查看队列、Worker、操作、命令关联和兼容路径。 |
| `platform.reliability.replay` | 重放一个已经进入 dead-letter 的事件。 |
| `platform.retention.preview` | 运行和保存保留策略 dry-run。 |

页面可见不等于具备重放权限。权限由后台角色与 Permission Code 决定，重放和保留
预演不应授予普通值班账号。

## 日常查看

1. 打开“事件队列”，先看 `pending`、`retry`、`dead` 和最早待处理时间。
2. 打开“Worker 与操作”，确认 Worker 有近期心跳；再查看失败操作是否保留了恢复说明。
3. 点击事件的“消费尝试”图标，找出发生问题的消费者、worker 和错误类别。
4. 查看“可靠命令审计”关联。这里能看到命令、操作者、资源、request ID 和 event ID，
   但不会返回请求体、Outbox payload 或 Secret。
5. 在“兼容与保留”观察旧路径使用量。调用量存在时，不能删除兼容表、目录或接口。

所有列表都由服务端分页，并显示实际总数。事件筛选后会回到第一页，消费尝试只展示
当前事件。旧客户端的 `limit` 参数仍可用，新客户端应使用 `page` 和 `page_size`。
查询默认按管理员账号执行每分钟 `120` 次的单进程限流；收到 `429` 时按
`Retry-After` 等待。

运维接口只返回排障所需的元数据。事件 payload、headers、幂等键以及操作、兼容遥测
和命令审计的原始 details 不会返回到浏览器。

状态说明：

| 状态 | 说明 |
| --- | --- |
| `pending` | 已提交，等待 Worker。 |
| `processing` | 当前有有效 lease；等待完成或自然过期。 |
| `retry` | 临时失败，等待下次可用时间。 |
| `published` | 所有已注册消费者已确认。 |
| `dead` | 永久失败或超过最大尝试次数，等待人工修复和决定是否重放。 |

`attempts` 是事件被 Claim 的次数，不是消费者数量。`7/8` 还允许最后一次领取，`8/8`
不会继续到第 9 次；历史越界值会保留原次数并收敛到 `dead`。消费尝试中的 `skipped`
表示 Consumer Receipt 已存在，因此不会再次发送邮件、Webhook 或 EventBus 副作用。

`system:outbox-finalize` 是所有消费者结束后的系统阶段：`succeeded` 表示主事件已经
published，`retry`/`dead` 表示 Complete 失败后已安全转换状态，`failed` 表示状态转换本身
没有保存成功。此时应检查 lease owner/generation 和后端脱敏日志，不能直接修改数据库状态。

页面诊断栏会提示“尝试次数越界”和“处理租约已过期”；消费尝试详情还能提示“已记录
消费者均完成，事件尚未最终化”。浏览器只收到 lease、次数、时间和 allowlist 错误，不会收到
payload、headers、幂等键、邮箱、验证码、Token 或 Secret。

v0.13 起，页面顶部还会显示队列健康、积压年龄、近 1 小时失败趋势和近 24 小时失败数。
“进入诊断”只会打开 dead/retry 的只读筛选，不会自动重放或删除任务。可选 Prometheus exporter
默认关闭，开启时使用独立 loopback 监听；指标标签只包含固定操作、结果、Provider 和 Consumer ID。

## 重放失败队列

重放可能再次产生外部副作用。只在以下条件都满足时操作：

1. 已从消费尝试和接收服务日志确认根因。
2. 根因已修复。
3. 接收方按事件 ID 做幂等处理。
4. 操作者有独立的 replay 权限并完成二次确认。

CampusOS 只允许重放 `dead` 事件，并自动使用 `Idempotency-Key` 防止同一重放请求
重复重置队列。不要用 SQL 修改事件状态，也不要以反复点击 Webhook 测试代替重放。

修复旧的 `97/8` 或 `103/8` 事件时，先等待新 Worker 将其自动收敛为 dead，再逐条检查
Receipt 和 Attempt 后使用后台 Replay。Replay 会重置 attempts，但保留 Receipt；已有消费者
会 skipped，最终化成功后事件进入 published。禁止删除 Receipt 或批量重置 Outbox。

## Webhook 安全边界

Webhook 是至少一次投递。接收方需要验证 v1 HMAC 签名、timestamp 时窗，并按
`X-CampusOS-Event-ID` 去重。CampusOS 默认拒绝私网、loopback、link-local、危险
redirect 和 DNS rebinding；每个 endpoint 还能设置最大并发和每分钟速率。

“测试 endpoint”是管理员明确触发的一次安全探测，走同一 egress 校验，但不是持久
任务，不会自动重试或进入 Outbox。

生产环境保持：

```dotenv
WEBHOOK_ALLOWED_HOSTS=hooks.example.edu
WEBHOOK_ALLOW_PRIVATE_NETWORK=false
```

仓库内的 `docs/help/系统设计相关/v0.11可靠任务与Webhook安全运维.md`、
`docs/architecture/v0.11Webhook可靠投递与安全模型.md` 和
`docs/help/系统设计相关/v0.13可靠任务指标告警与故障恢复Runbook.md` 记录了面向维护者的完整恢复、
Prometheus 规则和故障演练；此文档站页面保留日常操作所需的公开说明。
