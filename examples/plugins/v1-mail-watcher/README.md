# 邮件观察参考插件

历史 v3 包夹具：可验证个人 Secret 的加密保存、掩码展示和撤销，不连接真实邮箱。旧 Host API 的明文 Secret 读取已在 V12-01b 关闭；本包尚未适配受管 Secret Broker，声明 `secret.self.read` 不会让插件读取原值，不能作为当前运行期 Secret 用例。
