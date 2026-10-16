# CampusOS v5 插件源码与包

> 更新时间：2026-10-16（Asia/Shanghai）

当前源码使用完整`campusos.plugin/v5`的`plugin.json`，不再自动发现或适配旧`plugin.yaml`、v4 `.installed`目录及开发源开关。主CLI的pack/sign/verify只处理v5，安装与启用由03b及后续阶段提供。

- `campusos.pdf-viewer/`：两个UI audience的无后端包，已重建旧PDF示例的发布合同与构建产物。
- `campusos.chat-echo/`：无UI的Go/WASI包，使用Go SDK的`ai.chat/v1` DTO，不调用模型或宿主能力。

显式构建与声明检查：

```bash
bash scripts/build-v5-examples.sh
go run ./cmd/campusos-plugin-v5-check -root plugins
```

`make contracts-check`包含同一生产目录声明检查，不构建或执行插件。实际包只能包含`plugin.json`、`dist/`、`schemas/`及可选README/LICENSE；编译产物不进入Git。构建器只在开发者显式运行时执行，不由包元数据触发。

查看[包与签名操作说明](../docs/help/插件v5包与签名操作说明.md)及[阶段进度](../docs/进度/v1.2-dev/v1.2.114-dev.md)。历史v4类型/校验器留作旧数据和测试参考，生产入口已退役；不得把旧目录或CLI当成v5安装后门。
