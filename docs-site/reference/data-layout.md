# 数据目录与持久化边界

> 更新时间：2026-09-23

```text
plugins/<key>/                                  # v4 开发源码
plugins/.installed/<key>/<version>/             # 已校验、不可变的运行发布包
plugins/.staging/                               # 短暂预检目录，不执行
data/personal-space/<decimal-user-id>/          # 用户文件和插件用户配置
  plugins/<key>/config/user.json                # 受宿主 CAS/配额约束
data/resources/                                 # 无 Runtime 的资源包
data/plugins/、data/plugin_data/                # v1-v3 历史兼容目录
```

## 用户空间

头像、正文图片、个人文档和文章附件均通过 User Storage Core 落盘并记录元数据/归属。默认个人配额为 50 MB，管理员可按用户覆盖。个人空间文件只能由所有者读取；文章附件仅在对应文章仍对当前用户开放时读取。

文章附件在发布时形成受管理的附件绑定/快照，因此会同时出现在文章附件列表和“我的文档”的文章附件入口；它不是对其他用户个人空间的文件浏览权。

## 插件配置

宿主在用户添加插件后才创建：

```text
data/personal-space/<user-id>/plugins/<key>/
├── config/user.json
├── config/meta.json
├── .pending/
└── .snapshots/
```

这些目录由宿主计算并维护，插件 iframe 不能传入路径、枚举其他用户目录或直接读写文件。默认用户级插件配置总预算为 2 MiB，单插件配置上限为 256 KiB。

## 备份

恢复完整实例必须同时备份 PostgreSQL 与 `data/personal-space/`；只备份其一都会破坏元数据与文件的一致性。`plugins/.installed/` 是部署发布物，开发源码和运行数据也应按发布策略分别保留。

更多细节见[插件市场、目录与用户数据](/plugins/market-managed-data)。
