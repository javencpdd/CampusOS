import type { ConfigEntry } from "./v5-configuration-form";

export function pluginDisplayName(entry: ConfigEntry): string {
  if (entry.release.manifest.display_name)
    return entry.release.manifest.display_name;
  const names: Record<string, string> = {
    "rss-publisher": "RSS 图文订阅",
    "pdf-viewer": "PDF 预览",
    "chat-echo": "聊天回显示例",
    "knowledge-reader": "知识读取示例",
    "ai-knowledge": "AI 与知识接入",
  };
  return entry.release.publisher === "campusos"
    ? names[entry.release.plugin_key] || entry.release.plugin_key
    : entry.release.plugin_key;
}

export function capabilityTitle(code: string): string {
  return (
    (
      {
        "ui.surface.open": "打开插件界面",
        "ui.resource.selected.read": "读取用户明确选择的 PDF",
        "web.public.read": "通过服务器读取公开网页和图片",
        "web.local.read": "通过本人配对的客户端读取公开网页和图片",
        "community.article.compose": "将所选图文导入编辑器，由本人确认发布",
        "knowledge.public.search": "检索限定的公开知识集合",
        "knowledge.public.source.read": "读取限定集合中的公开资料",
        "knowledge.public.answer": "基于限定的公开资料回答问题",
        "ai.chat.local.invoke": "向已批准的本机模型发送本人提交的文本",
        "ai.chat.remote.invoke": "向已批准的远程模型发送本人提交的文本",
      } as Record<string, string>
    )[code] || code
  );
}

export function savedCapability(entry: ConfigEntry, consume: any): boolean {
  return (
    !!entry.grant?.approved &&
    (entry.grant.consumes || []).some((item: any) =>
      sameConsumption(item, consume),
    )
  );
}

export function approvedCapabilities(entry: ConfigEntry): any[] {
  return (entry.release.manifest.consumes || []).filter((consume) =>
    savedCapability(entry, consume),
  );
}

export function consentRequired(entry: ConfigEntry): boolean {
  return (entry.release.manifest.consumes || []).some(
    (consume) => consume.scope.kind === "self",
  );
}

export function consentCurrent(entry: ConfigEntry): boolean {
  return (
    !consentRequired(entry) ||
    !!(
      entry.purpose_hash &&
      entry.grant?.revision &&
      entry.consent?.accepted &&
      entry.consent.purpose_hash === entry.purpose_hash &&
      entry.consent.grant_revision === entry.grant?.revision
    )
  );
}

export function requiresSelectedResource(entry: ConfigEntry): boolean {
  return (entry.release.manifest.consumes || []).some(
    (consume) => consume.capability === "ui.resource.selected.read",
  );
}

export function userProgress(entry: ConfigEntry, now = Date.now()) {
  const result = (
    stage: string,
    label: string,
    detail: string,
    action = "",
  ) => ({ stage, label, detail, action });
  const approval = approvalState(entry, now);
  if (!approval.ready)
    return result(
      "blocked",
      approval.label,
      "需要管理员先完善此版本的审批。已安装的版本仍可停用或卸载。",
    );
  const site = entry.site_state;
  if (!site)
    return result(
      "blocked",
      "本站状态待确认",
      "请刷新状态，确认管理员已启用此版本。",
      "refresh",
    );
  if (!site.installed || !site.enabled)
    return result(
      "blocked",
      "等待管理员启用",
      "本站尚未启用这个版本，请联系管理员；重复点击个人启用不能启动本站插件。",
    );
  if (!site.ready)
    return result(
      "blocked",
      "等待本站运行环境",
      site.error_code === "plugin.runtime_unavailable"
        ? "管理员需要配置插件执行器。它负责启动插件后端，当前插件尚未运行。"
        : "本站插件尚未就绪，请稍后刷新；持续不可用时请联系管理员。",
      "refresh",
    );
  const install = entry.installation;
  if (!install?.installed)
    return result(
      "install",
      "尚未安装到本人账号",
      "先安装到我的账号，再确认用途并启用。",
      "install",
    );
  if (!consentCurrent(entry))
    return result(
      "consent",
      entry.consent?.accepted ? "需要重新确认用途" : "等待本人同意用途",
      "查看下方用途，勾选确认并点击“同意此版本用途”。单独勾选尚未生效。",
      "consent",
    );
  if (
    install.version !== entry.release.version &&
    !(
      install.desired_version === entry.release.version &&
      install.desired === "disabled"
    )
  )
    return result(
      "upgrade",
      "需要切换到此版本",
      "用途已确认，点击切换版本；准备完成后再启用。",
      "upgrade",
    );
  if (install.desired !== "enabled")
    return result(
      "enable",
      "等待本人启用",
      "安装和用途确认已完成，点击启用开始使用。",
      "enable",
    );
  if (install.error_code || install.observed === "failed")
    return result(
      "pending",
      "启用尚未成功",
      install.error_code === "plugin.runtime_unavailable"
        ? "当前正在等待插件执行器，请联系管理员检查运行环境。"
        : "请检查本人配置并刷新状态；持续失败时联系管理员。",
      "refresh",
    );
  if (
    install.observed === "ready" ||
    (entry.release.manifest.runtime === "none" &&
      install.observed === "stopped")
  )
    return result(
      "ready",
      "已启用",
      requiresSelectedResource(entry)
        ? "在支持的 PDF 附件或文件上点击预览，即可使用这个插件。"
        : (entry.release.manifest.ui || []).some((ui) => ui.audience === "user")
          ? "插件已启用，可打开插件界面使用。"
          : "插件已启用，功能由宿主对应的入口调用。",
      "open",
    );
  return result(
    "pending",
    "正在启用",
    "请求已经提交，正在等待服务端就绪。刷新状态即可，无需重复启用。",
    "refresh",
  );
}

function canonical(value: any): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (value && typeof value === "object")
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${canonical(value[key])}`)
      .join(",")}}`;
  return JSON.stringify(value);
}

export function sameConsumption(a: any, b: any): boolean {
  return canonical(a) === canonical(b);
}

// These are UI hints from the last server read. Commands still require the
// server's current authorization, configuration and generation checks.
export function approvalState(entry: ConfigEntry, now = Date.now()) {
  const grant = entry.grant;
  if (!grant) return { ready: false, label: "未审批" };
  if (!grant.approved) return { ready: false, label: "未获批准" };
  if (grant.expires_at_ms && grant.expires_at_ms !== "0") {
    const expiry = Number(grant.expires_at_ms);
    if (!Number.isSafeInteger(expiry) || expiry <= now)
      return { ready: false, label: "审批已过期" };
  }
  if (
    (entry.release.manifest.consumes || []).some(
      (required) =>
        required.required &&
        !(grant.consumes || []).some((approved: any) =>
          sameConsumption(required, approved),
        ),
    )
  )
    return { ready: false, label: "审批缺少必需能力" };
  if (entry.release.manifest.host_resources && !grant.host_resources)
    return { ready: false, label: "审批缺少运行资源" };
  return { ready: true, label: "已审批" };
}

export function enableBlockReason(
  entry: ConfigEntry,
  now = Date.now(),
): string {
  if (!entry.installation?.installed) return "请先安装此插件。";
  if (
    entry.installation.version !== entry.release.version &&
    !(
      entry.installation.desired_version === entry.release.version &&
      entry.installation.desired === "disabled"
    )
  )
    return "请先切换到此版本，等待准备完成后再启用。";
  const approval = approvalState(entry, now);
  if (!approval.ready)
    return `${approval.label}：请先在下方完成并保存此版本审批，再点击启用。`;
  if (
    entry.installation.desired === "enabled" &&
    (entry.installation.desired_version || entry.installation.version) ===
      entry.release.version
  )
    return "已提交启用，请查看当前运行状态，无需重复点击。";
  return "";
}

export function runtimeLabel(entry: ConfigEntry): string {
  const install = entry.installation;
  if (!install?.installed) return "未安装";
  if (install.desired !== "enabled") return "已停用";
  if (install.error_code === "plugin.runtime_unavailable")
    return "等待执行器（尚未运行）";
  if (install.observed === "ready") return "已就绪";
  if (install.observed === "failed") return "启动失败";
  if (
    entry.release.manifest.runtime === "none" &&
    install.observed === "stopped"
  )
    return "界面已启用（无后端进程）";
  return "正在启动";
}

export function managementUIState(entry: ConfigEntry, now = Date.now()) {
  const artifacts = entry.release.manifest.ui || [];
  if (!artifacts.some((artifact) => artifact.audience === "admin"))
    return {
      declared: false,
      canOpen: false,
      message: artifacts.some((artifact) => artifact.audience === "user")
        ? "此版本仅提供用户界面。请在用户端插件中心完成本人安装、用途同意和启用后打开。"
        : "此版本没有独立界面，功能由宿主调用。普通配置与密钥引用可在系统配置页管理。",
    };
  let message = "";
  const install = entry.installation;
  if (!install?.installed) message = "请先安装此版本，再审批并启用。";
  else if (!approvalState(entry, now).ready)
    message = `${approvalState(entry, now).label}：请先完成并保存此版本审批，再启用。`;
  else if (
    install.version !== entry.release.version ||
    (install.desired_version || install.version) !== entry.release.version ||
    install.desired !== "enabled"
  )
    message = "请先启用此版本，再打开插件管理界面。";
  else if (
    entry.release.manifest.runtime !== "none" &&
    install.observed !== "ready"
  )
    message = `${runtimeLabel(entry)}，管理界面尚不可用。`;
  return {
    declared: true,
    canOpen: !message,
    message: message || "在受限的独立页面中打开插件自带的管理界面。",
  };
}
