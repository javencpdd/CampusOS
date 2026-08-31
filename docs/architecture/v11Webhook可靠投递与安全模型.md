# v0.11 Webhook 可靠投递与安全模型

> 基线：`v0.11.0`  
> Owner：Webhook Built-in Feature；可靠任务由 Reliability Core 执行。

## 1. 投递模型

```text
已提交领域事件
  -> platform_outbox
  -> webhook.fanout consumer
  -> webhook.delivery child event（endpoint + event 幂等键）
  -> HTTP sender
  -> webhook_deliveries 结果 / retry / dead-letter
```

Webhook 是**至少一次**投递。发送成功但 CampusOS 在写完成状态前崩溃时，接收方可能
收到同一事件多次；接收方必须将 `X-CampusOS-Event-ID` 与 endpoint 身份一起作为
幂等键。CampusOS 不承诺 HTTP exactly-once。

兼容期内，旧 EventBus 订阅仍会调用同一个 `DeliverEvent` 入口。子事件使用相同
delivery key，因此重复 fan-out 不会生成第二个独立投递记录。

## 2. 出站安全边界

默认策略拒绝危险目标，且不是只在创建 endpoint 时检查：

- 仅允许 `http` 和 `https`，拒绝 URL userinfo、fragment 和无 host 地址；
- 默认拒绝 loopback、私网、link-local、multicast、未指定地址和云元数据地址；
- 每次请求和每次 redirect 都重新校验 URL；最多 3 次 redirect；
- 拨号时再次 DNS 解析，并实际连接到已验证 IP，减少 DNS rebinding 风险；
- HTTP transport 不继承代理环境变量，避免代理绕过受控 resolver；
- endpoint 可选 `WEBHOOK_ALLOWED_HOSTS` allowlist；
- 每次响应头最多 32 KiB、响应体最多 64 KiB，超时受 endpoint 配置控制（最大 30 秒）。

`WEBHOOK_ALLOW_PRIVATE_NETWORK=true` 仅适用于隔离开发或本地 mock。它不是生产
开关，也不能替代 allowlist、网络隔离或接收方验证。

## 3. 签名与接收方验证

v1 请求包含：

```text
Content-Type: application/json
User-Agent: CampusOS-Webhook/0.11
X-CampusOS-Signature-Version: v1
X-CampusOS-Timestamp: <unix seconds>
X-CampusOS-Event-ID: <event id>
X-CampusOS-Signature: v1=<HMAC-SHA256(timestamp + "." + raw_body)>
```

接收方应：

1. 读取原始请求体，不先改写 JSON；
2. 校验签名版本为 `v1`；
3. 用 endpoint 专属 Secret 对 `timestamp + "." + raw_body` 做常量时间 HMAC 比较；
4. 拒绝过期 timestamp，并用 `X-CampusOS-Event-ID` 去重；
5. 仅在安全持久化或可重试处理后返回 2xx。

Secret 从 endpoint 响应、可靠任务页、Outbox、审计和日志中排除。不要把 Secret 写进
URL、截图、错误页或 Resource Package。

## 4. 重试、限流与失败

| 结果 | CampusOS 行为 |
| --- | --- |
| `2xx` | 保存成功投递并确认 child event。 |
| 网络错误、`408`、`429`、`5xx` | 受最大尝试次数约束地 retry；优先采用 `Retry-After`，最大 24 小时。 |
| 其他 `4xx`、无效 payload、SSRF 拒绝 | 作为永久失败进入 dead-letter。 |
| 超过重试次数 | child event dead-letter；投递记录写 `failed` 与 dead-letter 时间。 |
| endpoint 被停用/删除或事件不再匹配 | 不再发送；已存在历史记录保持可查。 |

endpoint 可配置 `max_concurrent`（1-16）和 `rate_limit_per_minute`（1-600）。v0.11 的
限流器是单进程防护层；多实例全局配额需要共享 limiter，属于后续版本，不能把当前
数值理解为集群级强制限额。

## 5. 管理与例外

- 创建、停用、启用和查看 endpoint 使用既有 Webhook Permission Code；可靠任务查看
  使用 `platform.reliability.read`，重放使用独立的
  `platform.reliability.replay`。
- 管理端“测试”是管理员明确触发的一次直接探测，仍经过同一 URL、DNS、拨号、
  redirect、超时和响应大小检查；它不是持久任务、不会出现在 Outbox，也不提供崩溃后
  自动恢复语义。
- dead-letter 重放只对 Reliability queue 中 `dead` 事件开放，要求二次确认和
  `Idempotency-Key`，并写入 command audit。不要用重复点击“测试”代替故障恢复。

详细运维步骤见 [v0.11 可靠任务与 Webhook 安全运维](../help/系统设计相关/v11可靠任务与Webhook安全运维.md)。
