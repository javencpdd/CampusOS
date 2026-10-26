# 备份、恢复与版本验收

> 更新时间：2026-10-21（Asia/Shanghai）；当前 v3 离线恢复合同。Windows 实机按用户要求延期；Linux 目标结果以 v1.2 最新进度和封存证据为准。

## 准备与备份

原生维护先显式构建当前运行版本的 CLI，之后备份/安装/恢复均使用预构建产物，不自动构建宿主：

```bash
go build -o bin/campusosctl ./cmd/campusosctl
# 停止 API 和所有其它写入者，保留 PostgreSQL。
make backup
./scripts/restore.sh verify <确切的v3备份.tar.gz>
./scripts/restore.sh extract <确切的v3备份.tar.gz> <新的私有检查目录>
```

v3 包含 PostgreSQL dump、migration、编译期模块描述符、External Plugin 包和数据、Built-in Feature 数据、Resource Package、对象字节与普通本地配置；数据库和完整 `PLUGIN_DATA_DIR/v5` 五棵受管目录属于同一备份集合。宿主和插件容器镜像由部署方独立保管并按固定 digest 准备，不在普通 SQL/文件备份中自动拉取或构建。脚本不提供跨数据库/文件的在线一致性快照。Docker 的 backup/restore 命令会停止 API/Web/Admin，使用非 root 维护容器操作数据；结束后保持停止，维护者明确完成后再启动。

根 `.env` 不进入普通备份。Secret/MFA 解密材料、JWT/挑战等部署认证配置由可信运维单独保管，不得放进 `data/config` 或插件普通数据树；脚本不自动识别任意文件里的秘密。缺少正确密钥时，数据库密文无法解密。默认从可信部署根 `.env` 读取连接配置；`CAMPUSOS_SKIP_DOTENV=true` 明确只使用进程环境。所有备份含私有数据，均须私密保管。

备份使用 0700 暂存、0600 归档、唯一名称及原子无覆盖发布；发布前由预构建 CLI 校验。dump 或校验失败不会发布半成品。外层只允许四个确切文件和三条确切 SHA 清单；两层均拒绝路径穿越、链接、特殊文件、重复/冲突项、额外 gzip 成员、非零追加载荷及超限解压。提取仅发布到新建或空私有目录，不继承归档 owner/执行权限。默认每层最多 16 GiB、250000 项，可通过 CLI 显式设置限额。

校验用于发现损坏和限制文件结构，不证明来源可信；SQL 恢复仍只接受可信运维保管的备份。当前入口只支持 v3 与**相同运行版本的完整 migration/checksum**。v1/v2 可能带 `.env`、旧身份域和旧 schema，当前入口明确拒绝；应在隔离旧版本环境中按前向迁移方案处理，不能仅改格式标记或重算摘要冒充当前备份。

## 离线恢复与授权复核

先保管原部署密钥，核对目标数据库、数据根与当前运行版本，停止所有 API/其它写入者。新建空数据库须先显式执行当前运行版本的迁移，建立相同 schema/checksum。原生机器需要 PostgreSQL 客户端 `psql`；Docker 维护镜像内已包含当前 CLI 与客户端：

```bash
# 使用本地可信环境配置 DATABASE_DSN、DB_NAME 等，不把连接口令写入命令行。
./scripts/restore.sh apply <v3备份.tar.gz> --confirm
bin/campusosctl recovery status
```

原生标准数据根为 `data`；可用 `CAMPUSOS_RECOVERY_DATA_ROOT` 明确目标根，`PLUGIN_DATA_DIR` 应指向该根内的 `plugin_data`。恢复不覆盖宿主源码、模块代码或 migration 文件，只核对当前运行版本并恢复受管数据。需要不同目录布局时，应先在隔离目标配置并验收，不把任意路径当默认受管根。

恢复先持久写入 `data/.campusos-recovery.json`，再替换受管文件、执行 SQL；文件和目录同步落盘后，事务内生成新的随机实例认证代次，废止 User/Admin 旧会话、刷新/恢复票据、旧机器凭据及委托，撤回插件 Grant/Consent、停止安装与待执行操作、暂停 Webhook/消息绑定和 AI 出站，并保存必需恢复证据。共享启动锁与独占维护锁禁止在存活 API 上执行恢复。失败保留隔离标记；相同归档可在修复原因后重试，不自动撤销隔离或清除历史。

此时 API 启动应明确拒绝。可信运维必须依据独立运维/撤权记录逐项检查 status 输出的当前账号、角色、权限和插件授权；先纠正不应保留的账号/绑定，再重新取得盘点摘要。不可直接把“备份里有”当成当前仍获批准。完成后使用**该次实际盘点结果**：

```bash
bin/campusosctl recovery review --data-root data \
  --epoch <当前epoch> --inventory-sha256 <已核对的当前盘点SHA256> \
  --operator <可信运维引用> --reason <复核记录引用，勿填秘密> --confirm-offline
```

陈旧指纹会拒绝。复核与证据同事务；数据库已提交而标记写入失败时，同一复核可修复标记，不重复认证轮换或审计。只有数据库和文件的代次、归档、复核摘要和隔离状态一致，服务才允许启动；数据库不可用时不会带着恢复标记回退为内存模式。恢复后的 JWT 签名派生绑定新代次，避免认证版本数字回到旧值时复活旧 Token。复核后的启动跳过 bootstrap 补种，保留运维已核对的角色移除结果。

复核允许重新启动基础应用，**不会自动重新批准插件或出站**。插件仍须由当前独立 Admin 重批用途/资源及用户重新同意；Webhook/消息绑定须按当前目标重新配置，AI 出站维持恢复隔离。部署主密钥从独立保管材料恢复，不从普通备份导出。

Docker 使用同一套协议；先显式构建/准备匹配版本的镜像，再执行 `docker-deploy.sh restore ... --confirm`（恢复命令不构建、不拉取），再在相同维护镜像/数据卷中执行 `recovery status` 与 `review`，最后显式 `up`。维护容器与 API 使用相同非 root UID，运行时根文件系统只读，只有受管数据卷与临时目录可写。PowerShell 入口保持同一停止/复核合同；语法或 Linux 结果不代表 Windows 实机已验收。

## Linux 完整演练与容量

```bash
make restore-drill
# 或指定必须尚不存在的独占证据目录：
python3 scripts/v12-08b-exit-drill.py --work .cache/v12-08b-exit/acceptance-<唯一名称>
```

演练只使用已缓存镜像、独占 PG16/桥接网络、两个空库与新数据根；端口仅发布到回环，不覆盖现有实例。实际执行备份、严格校验、原生恢复脚本和 Docker 维护脚本、隔离/陈旧复核拒绝、新代次重开、当前 Secret 正负例、对象对账、两用户及独立 Admin 的真实浏览器流程；比较源码、依赖、ELF/SPA、整个 schema 和镜像指纹。

容量配置在执行前单独冻结：一万帖子、200 文档、两用户、一签名插件、8 并发，每目标 600 次测量请求；p95、API 实际 ELF RSS 和恢复到复核后健康的时限均保留原始结果与目标硬件。固定阈值为 p95 ≤1000 ms、峰值 RSS ≤512 MiB、恢复至健康 ≤180 s、非预期响应/越权成功均为零。该恢复时限包含自动化可信测试运维的精确盘点复核，不能当作真实人工复核或生产 SLA。

## 插件回滚与整库恢复的区别

v5 检查点只保存本插件、本 owner 的配置和有限私有文件，不保存 Grant/Consent；仅支持同 schema preserve 和可验证回退，仍须当前授权、真实后端 ready 或 UI-only 激活及必需审计。整库恢复可能把权限数据带回旧时点，因此必须执行前述新认证代次、凭据废止和运维复核，不能套用插件检查点的授权语义。

`make release-check` 属于整版发布门禁；某个恢复或容量目标通过不代表尚未完成的 Knowledge/AI/RAG 或 v1.2 Final 已交付。
