# 数据目录

## 总体结构

```text
data/
├── plugins/                       插件实现和 manifest
├── plugin_data/                   插件运行数据和源码风格包
├── personal-space/<user_id>/      用户个人文件
│   ├── file/
│   ├── img/
│   ├── excel/
│   ├── word/
│   └── pdf/
├── images/                        非用户归属的全局图片
├── config/                        本地配置预留
├── dist/                          本地发布产物预留
└── skills/                        本地 Skill 数据预留
```

## 插件代码与数据分离

`data/plugins/<plugin>/` 只保存插件实现：

- `plugin.yaml`
- 插件 README
- `plugin.wasm` 或 gRPC 可执行入口
- 插件运行需要的静态资源

`data/plugin_data/<plugin>/` 保存运行后产生或可编辑的数据：

- SQLite/KV 文件
- 缓存和生成文件
- 页面风格包源码目录
- 插件自己的可恢复状态

插件包导出不会自动包含 `plugin_data`。迁移插件时必须分别考虑代码包和运行数据。

## 用户个人空间

默认根目录为：

```text
data/personal-space/<user_id>/
```

常见子目录：

| 路径 | 内容 |
| --- | --- |
| `img/avatars/` | 头像源文件，默认保留最近 3 个。 |
| `img/richtext/` | 富文本文章图片。 |
| `file/schedule/` | 个人课表索引和学期 JSON。 |
| `excel/`、`word/`、`pdf/` | 按文件类型分类的上传文件。 |

默认个人空间配额为 10MB，由 `personal-space` 插件配置控制。头像、富文本图片和课表文件共同计入配额。

## 页面风格包

可编辑源码风格包放在：

```text
data/plugin_data/<plugin>/style-packs/<pack>/
```

当前三个目标目录：

```text
data/plugin_data/personal-space/style-packs/      个人主页所有者风格
data/plugin_data/homepage-customizer/style-packs/ 管理员统一首页风格
data/plugin_data/web-theme/style-packs/           管理员提供的完整用户前台主题
```

典型结构：

```text
style.yaml
README.md
preview.png
templates/
assets/
styles/
config.schema.json
```

风格包不应放入 `data/plugins`，因为它不是插件 Runtime 的实现代码。可选的 `effects/main.js` 仍属于风格包数据，但只能通过沙箱运行时执行；`effects/source.ts` 是不直接运行的开发源码。

## 备份边界

完整恢复至少需要同一时间点的：

1. PostgreSQL 数据。
2. `data/` 持久文件。
3. `.env` 或等价的安全配置备份。

只备份数据库会丢失用户头像、富文本图片和插件文件；只备份 `data/` 会丢失用户、帖子、授权和插件状态。
