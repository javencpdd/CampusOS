# v11 可靠任务与 Webhook 合同

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

## 1. 只读接口

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
返回 `409`；没有认证 actor 返回 `401`。客户端不应把 `409` 当成可无条件重试。成功
响应同样只返回事件元数据，不返回被重放事件的 payload、headers 或幂等键。

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
