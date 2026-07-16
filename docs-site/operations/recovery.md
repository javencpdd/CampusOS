# 备份、恢复与版本验收

## 单节点备份

```bash
make backup
./scripts/restore.sh verify backups/campusos-backup-*.tar.gz
```

备份格式 v2 包含 PostgreSQL dump、migration、编译期模块描述符、External Plugin 代码和数据、Built-in Feature 数据、Resource Package、个人空间、图片及本地配置，并为内部文件生成 SHA-256 清单。备份可能包含密钥，不得上传公共仓库。

## 隔离恢复演练

```bash
make restore-drill
```

演练创建临时数据库，恢复并读取用户、帖子、External Plugin 和 migration，再检查模块描述符、模块数据、资源包、插件与个人空间文件；结束后删除临时数据库，不覆盖当前实例。

生产恢复必须先停止 API 和写入流量，再由维护者执行：

```bash
./scripts/restore.sh apply <backup.tar.gz> --confirm
make migrate-up
make database-check
```

## 插件回滚

External Plugin 在覆盖更新前自动保存到 `data/plugin_data/<plugin>/version-snapshots/`。管理端“外部插件 -> 版本”可以查看经过 checksum 校验的快照并恢复。Core/Built-in Feature 随服务源码部署，只能通过代码版本回退；Resource Package 使用资源校验和 Appearance 的应用回滚。

## 版本门禁

`make release-check` 运行路由合约、文档链接、Go 测试、migration、数据库体检、schema contract、三类插件模板、Web lint/format/build、Admin 与文档站构建、TypeScript SDK、恢复演练和 Chrome 页面 smoke。开发服务未启动时可显式设置 `RUN_BROWSER_SMOKE=false`，但正式验收不能跳过浏览器 smoke。
