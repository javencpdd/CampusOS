# v0.11 可靠任务与 Webhook 合同

> 基础地址：`http://localhost:8080/api/v1`  
> 版本：`v0.11.0-experimental`  
> 机器可读权威合同：[openapi-v0.6-current.yaml](openapi-v0.6-current.yaml)

所有接口沿用 CampusOS `{ code, msg, data, request_id }` 响应包络。可靠任务 API 只在
管理员权限通过后可用。列表统一使用 `page`、`page_size`，其中 `page_size` 最大为
`100`；旧客户端仍可传 `limit`，服务端会把它视为第一页的页大小。列表响应结构为：

```json
{
  "items": [],
  "total": 0,
  "pagination": {
    "page": 1,
    "page_size": 20,
    "total": 0,
    "total_pages": 0
  }
}
```

`total` 顶层字段为兼容字段，当前与 `pagination.total` 一致。查询按认证用户限流；无
用户上下文时按客户端 IP 限流。默认窗口为每分钟 `120` 次，超过限制返回 `429` 和
`Retry-After`。当前限流器是单进程保护，不代表多实例共享配额。

事件、重放、操作、兼容遥测和命令审计都通过显式响应 DTO 输出。接口不返回 Outbox
`payload`、`headers`、`idempotency_key`，也不返回 Webhook Secret、Token、operation
`details`、compatibility `detail` 或 command audit `details`。

## 1. Worker 状态与证据语义

`attempts` 表示事件被 Worker 成功 Claim 的次数，不是消费者数量。领取前 Store 会先把
`attempts >= max_attempts` 的 pending、retry 或租约已过期 processing 原子收敛到 `dead`；
`7/8` 可以领取最后一次并变为 `8/8`，`8/8` 不能再领取第 9 次。历史 `103/8` 会保留
103 这一事实并进入 `dead`，不会被改写成 8。

每个消费者成功后会保存 Consumer Receipt。后续因 Complete 前崩溃而重新领取时，已有
Receipt 的消费者记录为 `skipped`，含义是“该消费者已经确认”，不是消费者失败，也不会
再次发送邮件、Webhook 或 EventBus 副作用。平台仍保持 at-least-once 合同，外部接收方必须
按事件 ID 保持幂等，不能把 Receipt 解释为全局 exactly-once 承诺。

所有消费者成功或 skipped 后，Worker 以 `system:outbox-finalize` Attempt 记录主事件最终化：

| 状态 | 含义 |
| --- | --- |
| `succeeded` | Complete 已在当前 lease owner/generation 下把事件改为 `published`。 |
| `retry` | Complete 失败，但当前 Worker 成功把事件改为 retry。 |
| `dead` | Complete 失败且已达上限或错误不可重试，事件成功进入 dead。 |
| `failed` | Retry/DeadLetter 等状态转换也失败；需要结合 lease 和结构化日志排查。 |

Complete、Retry、DeadLetter 始终保留 `status=processing + lease_owner + lease_generation`
fencing 条件。租约丢失时不会覆盖新 Worker；`ProcessOnce` 会返回安全错误，并写不含 payload、
邮箱、验证码、Token、Secret 或幂等键的结构化日志。

事件 DTO 可安全返回 `lease_owner`、`lease_until`、`lease_generation`、`available_at`、
`attempts`、`max_attempts`、`dead_lettered_at`、`attempts_overflow` 和 `lease_expired`。
`last_error` 与 Attempt `error` 经过 allowlist 脱敏，历史任意错误不会原样进入浏览器。

## 2. 只读接口

| 方法 | 路径 | Permission Code | 说明 |
| --- | --- | --- | --- |
| `GET` | `/platform/reliability/summary` | `platform.reliability.read` | 返回 pending、processing、retry、published、dead 和最早待处理时间。 |
| `GET` | `/platform/reliability/events` | `platform.reliability.read` | 支持 `status`、`type`、`page`、`page_size`；返回事件元数据和状态。 |
| `GET` | `/platform/reliability/attempts` | `platform.reliability.read` | 支持 `event_id`、`page`、`page_size`；返回消费者尝试证据。 |
| `GET` | `/platform/reliability/workers` | `platform.reliability.read` | 支持分页；返回 worker heartbeat。 |
| `GET` | `/platform/reliability/operations` | `platform.reliability.read` | 支持 `kind` 和分页；返回不含内部 details 的操作状态。 |
| `GET` | `/platform/reliability/command-audits` | `platform.reliability.read` | 支持分页；返回 command、actor、资源、request/trace/event 关联。 |
| `GET` | `/platform/reliability/compatibility` | `platform.reliability.read` | 支持分页；返回不含 raw detail 的兼容路径计数。 |
| `GET` | `/platform/reliability/retention-preview` | `platform.retention.preview` | 支持 `target`、RFC3339 `before`；只计算候选数。 |
| `GET` | `/platform/reliability/retention-runs` | `platform.retention.preview` | 支持分页；返回已经保存的 dry-run。 |

## 3. 高风险写接口

| 方法 | 路径 | Permission Code | 要求 |
| --- | --- | --- | --- |
| `POST` | `/platform/reliability/events/:id/replay` | `platform.reliability.replay` | 只允许 `dead` 事件；必须提供 `Idempotency-Key`；从认证上下文获得 actor；写 command audit。 |
| `POST` | `/platform/reliability/retention-runs/preview` | `platform.retention.preview` | 只保存 dry-run 结果，不删除数据。 |

重放请求示例：

```bash
curl -fsS -X POST \
  -H "Authorization: Bearer ${CAMPUSOS_TOKEN}" \
  -H "Idempotency-Key: $(uuidgen)" \
  http://localhost:8080/api/v1/platform/reliability/events/<dead-event-id>/replay
```

错误语义：找不到事件返回 `404`；事件不是 dead、缺少幂等键或相同幂等请求仍在处理中
返回 `409`；没有认证 actor 返回 `401`。客户端不应把 `409` 当成可无条件重试。成功
响应同样只返回事件元数据，不返回被重放事件的 payload、headers 或幂等键。

历史异常恢复必须先等待耗尽事件进入 dead，再由管理员逐条审查和 Replay。Replay 重置事件
attempts，但保留 Consumer Receipt，因此已确认消费者会 skipped。不得通过 SQL 删除 Receipt、
把 processing 直接改成 published，或批量重置 Outbox。操作步骤见
[v0.12 可靠事件异常诊断与安全恢复](../help/系统设计相关/v12可靠事件异常诊断与安全恢复.md)。

## 4. Webhook 合同补充

既有 `/webhooks/*` 接口保持兼容。endpoint 响应新增或可包含：

```json
{
  "max_concurrent": 2,
  "rate_limit_per_minute": 60
}
```

投递记录可包含 `outbox_event_id`、`delivery_key`、`next_attempt_at` 和
`dead_lettered_at`，用于与可靠任务页面关联。Secret 永远不在 endpoint 或 delivery
响应中返回。

Webhook 详细签名与安全策略见 [v0.11 Webhook 可靠投递与安全模型](../architecture/v11Webhook可靠投递与安全模型.md)。
