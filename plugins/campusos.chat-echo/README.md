# v5 无 UI Echo Provider 示例

本包只有WASI产物及`ai.chat/v1`声明，消费能力为空，不读取宿主数据、网络或Secret。它演示Go SDK DTO与可复现打包，不调用模型，不代表05/06 Runner或03c Dispatcher已接入。程序仅回显调用者提供的公开/无上下文文本。

从仓库根目录执行`bash scripts/build-v5-examples.sh`构建，然后使用`campusosctl plugin pack`、`sign`和`verify`。私钥与信任配置由操作者显式提供，示例不携带生产密钥。生产安装/启用须由03b及后续阶段提供，包验证不会执行插件。
