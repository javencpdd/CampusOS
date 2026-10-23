import { expect, it } from "vitest";
import {
  configurationForm,
  validateValues,
  type ConfigEntry,
} from "../src/modules/plugins/v5-config";
export function entry(): ConfigEntry {
  return {
    release: {
      publisher: "test",
      plugin_key: "config",
      version: "1.0.0",
      manifest: {
        api_version: "campusos.plugin/v5",
        data: { config_scope: "system" },
        configuration: {
          contract: "campusos.plugin-config/v1",
          version: "v1",
          schema: {
            type: "object",
            additionalProperties: false,
            properties: {
              label: {
                type: "string",
                title: "名称",
                minLength: 1,
                maxLength: 4,
              },
              enabled: { type: "boolean", title: "启用" },
              mode: { type: "string", title: "模式", enum: ["a", "b"] },
            },
            required: ["label", "enabled"],
          },
          defaults: {},
          selectors: [],
          secret_names: ["api_key"],
        },
      },
    },
    installation: {
      installed: true,
      observed: "stopped",
      desired: "disabled",
      error_code: "",
    },
    configuration: {
      owner_user_id: "",
      revision: "9007199254740993",
      values: { label: "测试", enabled: false },
      secret_refs: {},
    },
  };
}
it("keeps BIGINT revisions exact and validates required, enum, unicode length and false", () => {
  const form = configurationForm(entry());
  expect(form.revision).toBe("9007199254740993");
  expect(validateValues(form, { label: "😀😀😀😀", enabled: false })).toBe("");
  for (const values of [
    { label: "", enabled: false },
    { label: "x" },
    { label: "x", enabled: "false" },
    { label: "x", enabled: true, mode: "c" },
    { label: "x", enabled: false, extra: "x" },
  ])
    expect(validateValues(form, values)).not.toBe("");
});
it.each([
  "user",
  "uninstalled",
  "selector",
  "secret",
  "number",
  "script",
  "unknown",
  "owner",
])("closes unsupported or non-system input: %s", (kind) => {
  const e = entry();
  const d = e.release.manifest.configuration as any;
  if (kind === "user") e.release.manifest.data.config_scope = "user";
  if (kind === "uninstalled") e.installation!.installed = false;
  if (kind === "selector") d.selectors = [{ field: "mode", kind: "profile" }];
  if (kind === "secret")
    e.configuration!.secret_refs = { api_key: "secret-ref:bound" };
  if (kind === "number") d.schema.properties.label.type = "number";
  if (kind === "script") d.schema.properties.label.script = "alert(1)";
  if (kind === "unknown") e.configuration!.values.extra = "unknown";
  if (kind === "owner") e.configuration!.owner_user_id = "123";
  if (kind === "selector") {
    const form = configurationForm(e);
    expect(validateValues(form, { ...form.values, mode: "a" })).toContain(
      "不可选",
    );
    return;
  }
  if (kind === "secret") {
    const form = configurationForm(e);
    expect(form.secretRefs).toEqual({ api_key: "secret-ref:bound" });
    expect(validateValues(form, form.values)).toBe("");
    return;
  }
  expect(() => configurationForm(e)).toThrow();
});
