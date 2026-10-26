# 数据目录与持久化边界

> 更新时间：2026-10-26 07:03（Asia/Shanghai）

以下是默认布局。实际 `PLUGIN_DATA_DIR`、资源和模块数据路径可由受信部署配置改变；用户和插件不能提交任意宿主路径。

```text
modules/{core,features}/<id>/module.yaml           # 编译期模块描述符，不是外部插件
plugins/<示例源码目录>/plugin.json                # v5 作者源码，不自动安装/执行
data/plugin_data/v5/                             # 默认 PLUGIN_DATA_DIR/v5
  packages/<digest-hex>.tar.gz                    # 完整包摘要绑定的不可变包字节
  prepared/<publisher>/<key>/<owner>/<generation>.json
  data/<publisher>/<key>/<owner>/                # 宿主受管本域私有文件
  checkpoints/<publisher>/<key>/<owner>/<operation-id>.json
  restored-data/<publisher>/<key>/<owner>/<generation>/
data/module_data/                                # 内置模块的运行数据
data/resources/                                  # Resource Package 与内置静态资源
  branding/default-logo.png                      # 精确静态文件，非资源包
data/config/branding/                            # 外观服务管理的上传 Logo
data/personal-space/<decimal-user-id>/
  file/objects/<key>                             # User Storage Core 的受管对象字节
```

`owner` 是宿主生成的 `system` 或 `user-<十进制User-ID>`。客户端不控制这些目录、包摘要或代次，也不能靠猜测路径访问其他 owner。

## 用户空间与对象

头像、正文图片、个人文档及文章附件的字节由 User Storage Core 管理，`storage_objects` 与业务引用记录共同确定归属和当前可见性。默认个人配额为 50 MiB，管理员可按用户覆盖；上传与使用仍受当前配额、对象状态和业务权限检查。

公开文章附件可按对应文章的当前可见性读取，不等于允许浏览作者的个人空间或读取其他私人对象。文档版本、附件绑定与原对象字节不能只备份其中一侧。目录中保留的旧文件布局不授予新的访问权限。

## v5 安装、配置与检查点

全局/User 安装、Grant/Consent、Operation 以及 `plugin_v5_configurations` 是 PostgreSQL 中的当前事实；配置通过有界 Schema、修订 CAS 和本域身份复核。Secret 仅以受控引用参与配置，不把解密材料写进普通配置或交给插件 UI。

`prepared/` 保存本域某代次的准备胶囊；`data/`、`checkpoints/`、`restored-data/` 由宿主维护。检查点只包含该安装版本、配置及有界私有普通文件，不保存 Grant、Consent、会话或可执行权限。回退仍要求当前授权和已验收的生命周期合同；这些文件不是一个任意私有文件 Host API，也不会因此挂载给 Runner。

插件 iframe 不获得文件系统路径、目录枚举或直接写权限。旧 `data/personal-space/<id>/plugins/<key>/config/user.json`、`.pending/`、`.snapshots/` 以及 v4 的 `plugins/.installed/`、`.staging/` 属于明确的历史兼容材料，不是当前 v5 配置或发布位置。`data/plugins/` 可能保留历史数据；`data/plugin_data/v5/` 则是当前受管数据，不能一并当作旧目录忽略。

## v3 备份集合与密钥保管

官方 `scripts/backup.sh` 使用预构建 CLI 校验 v3 归档。必须停止 API 和其他写入者，取得同一时点的数据库与文件集合；脚本不提供数据库/文件之间的在线一致性快照。

外层包含 `database.sql`、`files.tar.gz`、`metadata.txt`、`SHA256SUMS`。文件集按存在的受管输入归一化为：

```text
migrations/       modules/
data/plugins/     data/plugin_data/     data/module_data/     data/resources/
data/personal-space/    data/images/    data/config/    data/skills/
```

因此，数据库、整个 `PLUGIN_DATA_DIR/v5/`、对象字节、模块数据和普通本地配置都属于恢复集合；仅备份 PostgreSQL 与个人空间不足以恢复完整实例。`migrations/` 和模块描述符随包保留供核对，恢复不会覆盖当前宿主源码、模块代码、构建产物或迁移文件。当前入口只接受与运行版本匹配的完整迁移及校验和，不把旧 v1/v2 归档改名成 v3 使用。

根 `.env` 不进入普通备份。Secret/MFA 解密材料、JWT/挑战等部署认证材料、宿主/插件固定镜像制品须单独可信保管；脚本不会从 SQL/普通文件自动重建密钥，也不会识别任意文件里的秘密，不能把秘密塞入 `data/config`。

私人派生索引的 `KNOWLEDGE_PRIVATE_KEY_DIR` 必须位于应用数据及全部备份输入/输出目录之外，其中的独立密钥和持久撤回标记另行保管，不能随旧应用备份回灌。整库恢复会废止旧 User/Admin 会话、刷新/恢复票据、机器凭据与插件 Grant/Consent，隔离应用并暂停出站；私人派生选择不会因备份中的旧同意而自动恢复授权。运维复核后也不会自动重新批准插件或模型出站。

确切停止、严格校验、隔离和当前权限复核步骤见[备份与恢复](/operations/recovery)。备份通过只证明该次目标与范围，不表示已完成未验收的平台或生产 SLA。
