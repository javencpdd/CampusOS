# 容量基线与发布门禁

v13 使用 `campusos-baseline` 冻结静态清单，使用 `campusos-capacity` 在同一候选环境中比较只读延迟和受控运行时快照。

```bash
make v13-capacity-check
```

该命令检查容量工具、预算文件、结构化列表查询预算、核心可靠性测试和前端 bundle 预算。真实候选环境还需执行两次 loopback-only capture：一次升级前基线、一次升级后候选，再比较公开列表与固定管理员准入/MFA/指标读取的 p95、goroutine、堆内存、连接池等待和 Reliability 队列年龄。真实候选比较使用严格的 `v13-capacity-budget.json`；隔离演练使用独立预算，仅为 1-10ms loopback 测量增加 `8ms` 绝对抖动阈值，不能替代真实候选环境性能结论。

仓库还提供 `make v13-capacity-drill`：它在临时 PostgreSQL 数据库上构建同一个 API 二进制，
让 baseline 与 candidate 分别在全新的 `127.0.0.1` API 进程中执行相同预热和受限采样，然后删除临时
数据库、进程和授权文件。进程隔离避免第二轮继承第一轮堆分配、使结果取决于 GC 时机。该演练不使用现有
开发库的 dead-letter、Session 或用户数据，并默认纳入发布检查。默认每轮采集 40 个样本，可通过
`V13_CAPACITY_SAMPLES=20..50` 调整；上限受可靠任务查询真实的 `120 次/分钟/管理员` 限流约束，演练
不会为测试禁用或放宽该保护。

工具不接受命令行 Token，也不会调用登录、验证码或写 API。需要观测快照时，临时授权文件必须是 `0600`，并在采样后按本机安全流程移除。

完整步骤、预算和故障处理见仓库中的 [v13 容量基线与回归门禁](https://github.com/javencpdd/CampusOS/blob/main/docs/help/%E7%B3%BB%E7%BB%9F%E8%AE%BE%E8%AE%A1%E7%9B%B8%E5%85%B3/v13%E5%AE%B9%E9%87%8F%E5%9F%BA%E7%BA%BF%E4%B8%8E%E5%9B%9E%E5%BD%92%E9%97%A8%E7%A6%81.md)。
