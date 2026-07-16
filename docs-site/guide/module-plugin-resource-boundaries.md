# 模块、插件与资源包怎么区分

CampusOS 是模块化单体。课表、个人空间、富文本和外观是随服务一起编译的内置
功能，不是从插件市场安装的第三方代码。

## 先记住四类能力

| 看到的类型 | 它是什么 | 在后台怎么管理 |
| --- | --- | --- |
| Core Module | 系统完整性必需，例如身份、社区、版主策略、User Storage | 始终启用，只能改允许的策略 |
| Built-in Feature | 随主程序编译，例如课表、个人空间、富文本、Appearance | 可启停，不可安装或卸载 |
| External Plugin | 独立 Wasm 或受管进程扩展 | 可检查、安装、更新、启停和卸载 |
| Resource Package | 主题、主页风格、个人主页风格、Skill/Prompt 等数据 | 校验后导入、应用和导出 |

## 代码放在哪里

```text
modules/                              模块描述符
internal/modules/                     Core 与 Built-in Feature 实现
data/plugins/                         External Plugin 实现
data/plugin_data/                     External Plugin 运行数据
data/module_data/                     Built-in Feature 本地数据
data/resources/                       主题和其他 Resource Package
data/personal-space/<user_id>/        用户私有文件
```

不要把 `module.yaml` 放入 `data/plugins`，也不要把 `plugin.yaml` 放入
`modules`。风格包不是可执行插件，不能放进插件实现目录。

## 为什么后台还分两个入口

- **内置功能**调用 `/api/v1/features`。这里可以看到课表、个人空间、富文本和
  Appearance 的状态、配置与重启提示。
- **外部插件**调用 `/api/v1/plugins`。这里处理 Wasm/受管进程、安装包、版本、
  Host API 权限和运行日志。
- **外观与风格包**使用资源目录和 Appearance API，不占用插件生命周期。

用户前台的运行清单也分别返回 `modules[]` 和 `plugins[]`。因此外部插件列表
为空时，内置课表和风格切换仍应正常显示。

## Built-in 还能写 plugin.yaml 吗

不能。`runtime: builtin` 只为旧文件审查和迁移保留解析能力：

- CLI 不再生成它；
- Plugin Manager 不安装它；
- 外部插件不能占用 `personal-schedule`、`personal-space`、
  `homepage-customizer` 等保留名称。

新增 Built-in Feature 需要编写 `campusos.module/v1` 描述符和 Go 模块代码，
重新编译 CampusOS。新增可移植扩展应使用 Wasm 或受管进程插件。

## 数据会不会因停用而消失

不会。Built-in Feature 停用只隐藏界面、拒绝 API 或停止事件处理；课表 JSON、
个人主页、文章和资源包都会保留。External Plugin 卸载也必须按其数据声明处理，
不能把代码目录删除等同于删除用户数据。

## 开发前检查

```bash
make data-governance-check
make architecture-check
GOCACHE=/tmp/campusos-go-cache go test ./modules ./internal/plugin/... -count=1
```

继续阅读：[数据目录](/reference/data-layout)、[插件体系](/plugins/overview)、
[课表插件完整教程](/plugins/schedule-plugin-tutorial)。
