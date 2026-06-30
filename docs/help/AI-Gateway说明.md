# CampusOS AI Gateway 说明

> 日期：2026-07-01
> 范围：v0.4-dev AI Gateway 最小内核

## 1. 当前定位

AI Gateway 用于把 CampusOS 内部的 AI 调用统一收口，避免插件、后台、MCP、Webhook 或机器人适配器直接绑定某一个模型厂商。

当前已完成的是后端最小内核：

| 能力 | 状态 |
| --- | --- |
| Provider 接口 | 已提供 |
| OpenAI-compatible Provider | 已提供 |
| ChatCompletion 文本调用 | 已提供 |
| Embedding 可选接口定义 | 已提供接口，暂未实现 provider |
| Provider 配置 | 已接入 `.env.example` 和 `pkg/config` |
| API Key 脱敏 | 已提供，错误信息不直接暴露密钥 |
| 调用日志 | 已提供内存 logger |
| 基础限流 | 已提供并发数和每分钟请求数限制 |
| 管理员状态接口 | 已提供 |
| 管理员日志接口 | 已提供内存日志查询 |
| 管理后台页面 | 未完成 |
| 数据库日志持久化 | 未完成 |
| Host API 暴露给插件 | 未完成 |

## 2. 环境变量

`.env.example` 中新增：

```bash
AI_ENABLED=false
AI_PROVIDER=openai-compatible
AI_BASE_URL=https://api.openai.com/v1
AI_MODEL=gpt-4o-mini
AI_API_KEY=
AI_TIMEOUT=30s
AI_MAX_REQUESTS_PER_MINUTE=60
AI_MAX_CONCURRENT=4
```

| 变量 | 说明 |
| --- | --- |
| `AI_ENABLED` | 是否启用 AI 能力入口 |
| `AI_PROVIDER` | Provider 类型，当前建议使用 `openai-compatible` |
| `AI_BASE_URL` | OpenAI-compatible API base URL，通常以 `/v1` 结尾 |
| `AI_MODEL` | 默认模型名 |
| `AI_API_KEY` | Provider API Key，不应提交到仓库 |
| `AI_TIMEOUT` | HTTP 调用超时时间 |
| `AI_MAX_REQUESTS_PER_MINUTE` | 每分钟最大请求数 |
| `AI_MAX_CONCURRENT` | 最大并发请求数 |

## 3. 后端使用示例

```go
provider, err := ai.NewOpenAICompatibleProvider(ai.OpenAICompatibleConfig{
    BaseURL: "https://api.example.com/v1",
    APIKey:  "replace-with-real-key",
    Model:   "campus-model",
})
if err != nil {
    return err
}

logger := ai.NewMemoryCallLogger()
limiter := ai.NewInMemoryLimiter(4, 60)
gateway, err := ai.NewGateway(provider, ai.GatewayConfig{
    DefaultModel:  "campus-model",
    DefaultSource: "admin",
}, ai.WithCallLogger(logger), ai.WithLimiter(limiter))
if err != nil {
    return err
}

resp, err := gateway.ChatCompletion(ctx, ai.ChatRequest{
    Messages: []ai.Message{
        {Role: "user", Content: "请总结这篇帖子"},
    },
    Source: "plugin:summary",
})
```

## 4. 管理接口

AI Gateway 已接入服务启动流程。管理员可通过 API 查看当前状态和内存调用日志。

| 接口 | 权限 | 说明 |
| --- | --- | --- |
| `GET /api/v1/ai/status` | `role:manage` | 查看 AI Gateway 是否启用、是否就绪、provider 和脱敏配置 |
| `GET /api/v1/ai/logs?limit=100` | `role:manage` | 查看最近内存调用日志 |

状态接口不会返回 `AI_API_KEY` 原文，只会返回：

```json
{
  "api_key_configured": true
}
```

当前日志仍是内存日志，服务重启后会丢失。后续需要落地数据库表。

## 5. 安全要求

| 要求 | 当前处理 |
| --- | --- |
| API Key 不进仓库 | `.env.example` 只保留空值 |
| API Key 不进错误输出 | Provider 错误会替换密钥为 `[REDACTED]` |
| 调用来源可追踪 | `ChatRequest.Source` 会进入调用日志 |
| 请求量可控 | `InMemoryLimiter` 支持并发和每分钟请求数限制 |
| 默认不开启 | `AI_ENABLED=false` |
| 管理接口权限 | `/api/v1/ai/status` 和 `/api/v1/ai/logs` 需要 `role:manage` |

## 6. 后续任务

| 优先级 | 任务 |
| --- | --- |
| P0 | 将调用日志持久化到数据库 |
| P1 | 管理后台 AI Provider 配置页 |
| P1 | 管理后台 AI 调用日志页 |
| P1 | Host API 或 SDK 暴露受控 AI 调用能力 |
| P2 | Embedding provider 实现 |

当前阶段不做 AI 内容审核插件。AI 审核需要等 Provider、日志、限流、人工复核和测试样本更稳定后再进入主线验收。
