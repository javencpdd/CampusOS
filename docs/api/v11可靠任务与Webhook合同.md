# v11 可靠任务与 Webhook 合同

> 基础地址：`http://localhost:8080/api/v1`  
> 版本：`v0.11.0-experimental`  
> 机器可读权威合同：[openapi-v0.6-current.yaml](openapi-v0.6-current.yaml)

所有接口沿用 CampusOS `{ code, msg, data, request_id }` 响应包络。可靠任务 API 只在
管理员权限通过后可用，列表上限由服务端限制，且不返回 Outbox payload、Webhook
Secret、Token 或 command audit 的 `details`。

## 1. 只读接口

| 方法 | 路径 | Permission Code | 说明 |
| --- | --- | --- | --- |
| `GET` | `/platform/reliability/summary` | `platform.reliability.read` | 返回 pending、processing、retry、published、dead 和最早待处理时间。 |
| `GET` | `/platform/reliability/events` | `platform.reliability.read` | 支持 `status`、`type`、`limit`；返回事件元数据和状态，不返回敏感 payload。 |
| `GET` | `/platform/reliability/attempts` | `platform.reliability.read` | 支持 `event_id`、`limit`；返回消费者尝试证据。 |
| `GET` | `/platform/reliability/workers` | `platform.reliability.read` | 返回 worker heartbeat。 |
| `GET` | `/platform/reliability/operations` | `platform.reliability.read` | 支持 `kind`、`limit`；返回可恢复文件/资源操作状态。 |
| `GET` | `/platform/reliability/command-audits` | `platform.reliability.read` | 返回 command、actor、资源、request/trace/event 关联；不返回 raw details。 |
| `GET` | `/platform/reliability/compatibility` | `platform.reliability.read` | 返回兼容路径使用计数。 |
| `GET` | `/platform/reliability/retention-preview` | `platform.retention.preview` | 支持 `target`、RFC3339 `before`；只计算候选数。 |
| `GET` | `/platform/reliability/retention-runs` | `platform.retention.preview` | 返回已经保存的 dry-run。 |

## 2. 高风险写接口

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
返回 `409`；没有认证 actor 返回 `401`。客户端不应把 `409` 当成可无条件重试。

## 3. Webhook 合同补充

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

Webhook 详细签名与安全策略见 [v11 Webhook 可靠投递与安全模型](../architecture/v11Webhook可靠投递与安全模型.md)。
