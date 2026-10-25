# 受管 AI 模型

> 更新时间：2026-10-25（Asia/Shanghai）

V12-14a 提供内置文本模型网关。部署者配置远程 OpenAI 兼容 HTTPS 模型和本机 OpenAI 兼容守护进程；独立管理员使用当前 MFA 会话切换两个固定 Profile。14a提供宿主内部能力和管理 API；14b当前用户[有据问答](./cited-rag.md)已接公开/私人页面，14c插件模型消费及贡献接入见[插件AI与公开知识能力](./plugin-ai-knowledge.md)。14b专用内部Port分别接入[公开集合本机引用问答](./public-local-rag.md)和[单个私人来源的本机问答与逐次同意](./private-local-rag.md)；私人索引同意不替代本次模型同意。

## 配置与选择

保持 `AI_ENABLED=false` 直到两种 Profile 的模型、端点、凭据、独立加密主密钥、配额和超时全部配置好。远程配置使用 `AI_BASE_URL/AI_MODEL/AI_API_KEY`，本机使用 `AI_LOCAL_BASE_URL/AI_LOCAL_MODEL/AI_LOCAL_API_KEY`。`AI_ACTIVE_KEY_ID` 选择 `AI_ENCRYPTION_KEYS` 中的 `ID:64hex` 主密钥，独立于 JWT/MFA/插件密钥保管，不进入应用备份。

`AI_DEFAULT_PROFILE` 仅首次初始化选 `remote-chat` 或 `local-chat`；重启不会覆盖已保存的选择。`AI_BUDGET_REQUESTS` 是每 Profile 的累计请求单位额度，`AI_MAX_OUTPUT_TOKENS` 限制单请求输出，`AI_MAX_CONCURRENT` 限制持久预留数。超时限定100ms至60s。不支持工具或流式回答。

本机端点必须使用 literal loopback IP 加端口，如 `http://127.0.0.1:11434/v1`。先由运维运行模型服务；网关不安装或启动模型。在 Docker 中 loopback 是容器自身，须按实际网络部署，不能把任意局域网主机当成本机例外。远程必须 HTTPS，DNS 混入私网地址会拒绝，不走代理、不跟随重定向、不自动重试或 fallback。

管理 API 实际路径是 `/api/v1/ai/profiles`，使用独立 Admin JWT。读取最新 Profile/Binding 修订后，以当前近期 MFA 会话调用 `/api/v1/ai/bindings/chat` 或 `/api/v1/ai/profiles/:profile_id/state`。版本用十进制字符串；冲突后重新读取，浏览器写操作还须管理域 CSRF。接口不能修改 URL、模型或明文凭据。管理元数据不包含端点、SecretRef 或提示词。

## 预算与取消

一次请求先预留1，未分发失败释放0；提交分发后的成功、取消、超时和不确定结果均保守结算1。这里不是 token 定价或财务账单。必需审计结算失败时不展示答案，租约工作器稍后只补结算，不重新请求模型。重启、切换 Profile、恢复都不清零预算。

当前会话撤销、Feature desired 停用、Profile 停用、绑定变化都会取消在途请求并丢弃迟到答案。取消不保证提供方尚未工作，所以未知用量不应当作免费。旧 request ID 不能拿来盲重试。

## 备份和恢复

数据库备份保留模型密文、预算及执行事件；部署加密主密钥单独保管。丢钥或同 ID 错钥会关闭执行，不能用重启“修复”为可用。轮换采用新 key ID 并保留恢复旧备份所需的旧密钥。

官方整库恢复停用 Profile、推进绑定版本并废止旧会话，授权盘点包含模型与预算；本地复核后仍暂停 AI 出站，不自动复活原授权。未决执行只结算，不重放。不要删除执行历史或预算强制回退迁移。

详细字段、安全边界、迁移与验收命令见仓库 `docs/api/v1.2受管AI模型与执行合同.md` 和 `scripts/v12-14a-managed-gateway-drill.py`。协议测试端验证调用链，不代表真实 LLM 质量。14b当前Linux完整验收见 `.156` 与 `v12-14b-exit-linux.json`；Windows目标延期；14c当前验收见新增接入教程与.158，整版Final仍待独立验收。

本轮14b的公开/私人页面与逐次同意操作见[有据问答](./cited-rag.md)；当前完整验收状态以仓库 `.156` 记录为准。原公开/私人本机 Port 指南保留其历史切片范围。
