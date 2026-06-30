# CampusOS PR 模板与 CI 自测说明

> 适用文件：`.github/PULL_REQUEST_TEMPLATE/pull_request_template.md`、`.github/workflows/ci_test.yml`
> 文档日期：2026-06-28

## 1. PR 模板位置

当前精简版 PR 模板放在：

```text
.github/PULL_REQUEST_TEMPLATE/pull_request_template.md
```

创建 Pull Request 时，GitHub 会把模板内容带入 PR 描述，方便提交者统一说明变更范围、验证结果、风险和关联事项。

后续如果需要多个模板，可以继续在 `.github/PULL_REQUEST_TEMPLATE/` 下新增文件，例如：

```text
feature.md
bugfix.md
docs.md
```

## 2. 是否还需要本地自测

已经有 `.github/workflows/ci_test.yml` 后，仍建议做本地自测，但不需要每次都完整重复 CI 的所有步骤。

推荐原则：

| 改动类型 | 本地建议验证 | 是否等待 GitHub CI |
| --- | --- | --- |
| Go 后端 | `go test ./...` 或相关包测试 | 是 |
| 数据库迁移 | `make migrate-up`、`make migrate-status` | 是 |
| Web 前端 | `cd web && pnpm build` | 是 |
| Admin 前端 | `cd admin && pnpm build` | 是 |
| 文档 | 检查路径、链接、格式 | 是 |
| CI/CD 配置 | 本地 YAML 解析或人工检查 | 是 |

实际工作中可以按影响范围选择：

- 小型文档改动：本地检查文档即可，等待 GitHub CI 最终确认。
- 后端代码改动：至少本地跑相关包测试；提交 PR 前建议跑 `go test ./...`。
- migration 改动：本地必须跑迁移验证，因为 CI 只能告诉失败，不能替你判断迁移是否符合预期。
- 前端改动：至少跑对应项目的 `pnpm build`。
- 发布或 workflow 改动：本地检查 YAML 和触发条件，合并前必须看 GitHub Actions 结果。

## 3. 为什么不能只依赖 GitHub CI

GitHub CI 是合并前的最终门禁，但本地自测仍有价值：

| 原因 | 说明 |
| --- | --- |
| 反馈更快 | 本地相关包测试通常比等待完整 CI 更快 |
| 定位更直接 | 本地失败可以立刻调试，不需要反复 push |
| 节省 runner | 减少无效 CI 运行 |
| 保护分支质量 | PR 打开时已经有基本可信度 |

因此建议流程是：

```text
本地按改动范围验证
        ↓
提交 commit
        ↓
打开 PR 并填写模板
        ↓
等待 GitHub Actions CI 全部通过
        ↓
Review / Merge
```

## 4. PR 模板填写建议

### 4.1 变更概要

用 1 到 3 条说明本次 PR 实际改了什么。

示例：

```text
- 新增 Host API 权限检查
- 补充权限拒绝和 storage namespace 隔离测试
```

### 4.2 验证结果

只勾选实际执行过的项目。不适用的项目写明原因。

示例：

```text
不适用的验证项：
- 未运行 web/admin build：本次没有修改前端代码
- 未运行 migrate：本次没有修改 migrations
```

### 4.3 风险与回滚

简要说明可能影响什么，以及如何撤回。

示例：

```text
风险：
- 未声明权限的插件调用 Host API 会被拒绝

回滚方式：
- revert 本 PR
```

## 5. 当前建议

`.github/workflows/ci_test.yml` 已经覆盖 PR 的主要自动验证。后续开发不需要每次手动完整复刻 CI，但提交 PR 前应至少完成和改动范围直接相关的本地验证。
