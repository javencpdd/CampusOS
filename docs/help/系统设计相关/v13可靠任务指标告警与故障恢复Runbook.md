# v0.13 可靠任务指标、告警与故障恢复 Runbook

> 适用版本：CampusOS v0.13-dev。本文用于诊断 Outbox Worker、邮件投递、Challenge 和 Session
> 的运行异常。所有恢复操作都必须保留 Consumer Receipt、Attempt 和命令审计。

## 1. 先看哪里

1. 打开管理端 `http://localhost:3001/reliability`。
2. 先看“队列健康、积压年龄、失败趋势”，再点击“进入诊断”。
3. 在事件详情中核对状态、尝试次数、租约和 Consumer Attempt。
4. Prometheus 默认关闭；启用时只监听独立 loopback 地址，默认是 `127.0.0.1:9091/metrics`。

后台页面不会自动重放或删除事件。只有 dead 事件在人工确认幂等性和根因已修复后，才能执行单事件
重放。

## 2. 核心指标

| 指标 | 标签 | 用途 |
| --- | --- | --- |
| `campusos_reliability_operations_total` | `operation`、`result` | Claim、Complete、Retry、Dead、Finalize、Receipt 和 lease conflict |
| `campusos_reliability_queue_events` | `status` | 当前 pending、processing、published、retry、dead 数量 |
| `campusos_reliability_oldest_pending_age_seconds` | 无 | 最早 pending/retry 事件的积压年龄 |
| `campusos_reliability_consumer_duration_seconds` | `consumer`、`result` | 固定 Consumer 的处理耗时和结果 |
| `campusos_email_delivery_total` | `provider`、`result` | delivered、skipped、unavailable、invalid |
| `campusos_email_delivery_duration_seconds` | `provider`、`result` | 邮件 Provider 调用耗时 |
| `campusos_identity_challenges_total` | `operation`、`result` | Challenge 请求、校验、Ticket 消费和限流 |
| `campusos_identity_sessions_total` | `operation`、`result` | Session 签发、验证、Refresh、Reuse 检测 |

标签中不会写入邮箱、验证码、Ticket、Token、事件 ID、用户 ID、请求路径或 payload。指标时序数据不写回
CampusOS 数据库。

## 3. Prometheus 配置

环境配置：

```dotenv
OBSERVABILITY_PROMETHEUS_ENABLED=true
OBSERVABILITY_PROMETHEUS_ADDR=127.0.0.1:9091
OBSERVABILITY_PROMETHEUS_PATH=/metrics
```

参考抓取配置位于 `deploy/prometheus/prometheus.yml`，recording/alert rules 位于
`deploy/prometheus/campusos-v13.rules.yml`。容器访问宿主 loopback 时应使用受控网络代理或端口转发，
不能把 exporter 直接发布到公网。

## 4. Worker 停止演练

预期现象：

- pending/retry 数量大于 0；
- `campusos_reliability_oldest_pending_age_seconds` 持续增长；
- Claim 成功速率归零；
- `CampusOSReliabilityWorkerStopped` 或 `CampusOSReliabilityBacklogOld` 触发。

恢复步骤：

1. 确认数据库可用、migration 已完成，且没有第二个旧版本 Worker 持有有效 lease。
2. 恢复当前版本 Worker，观察 Worker 心跳和 Claim 指标。
3. 等待队列收敛，pending/retry 应下降，published 应上升，积压年龄最终回到 0。
4. 抽查已有 Receipt 的事件：已完成 Consumer 必须显示 skipped，不能再次执行外部副作用。

## 5. SMTP 临时失败演练

在测试环境使用会先失败再恢复的 Sender，或短暂阻断测试 SMTP。不要在生产邮箱上故意发送大量验证码。

预期现象：

- `campusos_email_delivery_total{result="unavailable"}` 增长；
- Outbox 进入 retry，不暴露收件人和验证码；
- 连续失败达到规则阈值后 `CampusOSEmailDeliveryDegraded` 触发。

恢复 SMTP 后，下一次有效重试应记录 delivered，Consumer Receipt 只产生一次，事件最终进入 published。
已经过期、失效或消费的 Challenge 会安全 skipped，不补发旧验证码。

## 6. Dead-letter 增长演练

仅在测试环境投递由测试 Consumer 明确返回 Permanent 的事件。预期
`campusos_reliability_operations_total{operation="dead_letter",result="success"}` 增长，后台失败队列可见，
并触发 `CampusOSReliabilityDeadLetterGrowth`。

先修复根因并核对 Consumer 幂等性，再使用后台单事件重放。禁止批量改表、删除 Receipt、重置所有
attempts 或把 processing 直接改成 published。

## 7. 自动化故障证据

```bash
make v13-reliability-observability-check
```

该门禁覆盖 Worker 积压与恢复、Retry 收敛、Dead-letter 增长、SMTP 暂时失败后恢复、Challenge/Ticket
以及 Refresh Token Reuse 指标。测试同时断言指标文本不包含邮箱、验证码、Ticket 或 Token。

## 8. 回滚

1. 可先设置 `OBSERVABILITY_PROMETHEUS_ENABLED=false` 并重启，业务内计数和可靠任务处理不受影响。
2. 可从 Prometheus 移除 v0.13 rules；这只撤销告警，不改变 Outbox 数据。
3. 不得通过回滚删除 Attempt、Receipt、审计或 dead 事件。
4. 如果需要回滚应用版本，先停止新 Worker，再等待 lease 到期并启动兼容版本；不得让不同状态机版本
   长期并行领取同一队列。
