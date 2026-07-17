# 初始管理员安全

v0.12 开始，CampusOS 将历史固定管理员凭据视为需要处理的兼容风险，而不是生产部署的
登录说明。服务不会在日志输出管理员密码、Bootstrap Secret 或密码哈希。

## 开发环境

本地开发使用明确的环境标记：

```dotenv
CAMPUSOS_ENV=development
AUTH_PASSWORD_HASH_ENABLED=true
AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN=true
```

这个兼容入口只用于本地开发和已有 smoke 工作流。不要将它用于共享环境。

## 生产环境

生产启动前，在部署 Secret 管理系统中设置：

```dotenv
CAMPUSOS_ENV=production
JWT_SECRET=<随机且非开发默认值>
AUTH_BOOTSTRAP_ADMIN_SECRET=<一次性高强度 Secret，至少 16 个字符>
AUTH_PASSWORD_HASH_ENABLED=true
AUTH_ALLOW_DEVELOPMENT_DEFAULT_ADMIN=false
```

生产环境会拒绝缺少 Bootstrap Secret、沿用开发 JWT Secret、关闭密码哈希或启用开发兼容
管理员的配置。

## 启动时会发生什么

1. 管理员不存在时，服务用 Bootstrap Secret 创建管理员。
2. 发现历史默认凭据时，服务将其轮换为 Bootstrap Secret。
3. 管理员已经使用自己的密码时，服务保留该密码，不会覆盖。

管理员角色和默认板块的初始化仍是幂等操作。若管理员邮箱与非 `admin` 用户冲突，服务会
停止并要求人工处理，而不会覆盖账号。

后续 v0.12 阶段会增加邮箱验证、Session 轮换、密码重置和受控恢复。详情可见仓库中的
`docs/help/系统设计相关/v12部署与初始管理员安全.md`。
