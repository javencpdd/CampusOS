import {
  ExactJSONNumber,
  compareExactNumbers,
  exactInteger,
  exactNumberText,
} from "./exact-json";
export type ConfigValue = string | boolean | number | ExactJSONNumber;
export interface ConfigField {
  type: "string" | "boolean" | "integer" | "number";
  title: string;
  enum?: ConfigValue[];
  minimum?: ConfigValue;
  maximum?: ConfigValue;
  minLength?: number;
  maxLength?: number;
  selector?: "profile" | "public_collection";
}
export interface Coordinate {
  publisher: string;
  plugin_key: string;
  version: string;
}
export interface ConfigEntry {
  release: Coordinate & {
    package_digest?: string;
    manifest: {
      api_version: string;
      display_name?: string;
      runtime?: string;
      consumes?: any[];
      host_resources?: any;
      data: { config_scope: string };
      configuration: unknown;
    };
  };
  installation?: {
    installed: boolean;
    observed: string;
    desired: string;
    error_code: string;
    generation?: string;
    version?: string;
    desired_version?: string;
  };
  configuration?: {
    owner_user_id: string;
    revision: string;
    values: Record<string, unknown>;
    secret_refs: Record<string, string>;
  };
  grant?: any;
  consent?: any;
  purpose_hash?: string;
}
export interface ConfigOptions {
  profiles: Array<{ id: string; title: string }>;
  public_collections: Array<{ id: string; title: string }>;
  secret_refs: Record<string, Array<{ id: string; title: string }>>;
}
export interface ConfigForm {
  fields: Record<string, ConfigField>;
  required: string[];
  values: Record<string, ConfigValue>;
  secretNames: string[];
  secretRefs: Record<string, string>;
  revision: string;
  options: ConfigOptions;
}
export const emptyConfigOptions = (): ConfigOptions => ({
  profiles: [],
  public_collections: [],
  secret_refs: {},
});
export const coordinateKey = (c: Coordinate) =>
  `${c.publisher}/${c.plugin_key}@${c.version}`;
const own = (v: object, k: string) =>
  Object.prototype.hasOwnProperty.call(v, k);
const obj = (v: unknown): v is Record<string, any> =>
  v !== null &&
  typeof v === "object" &&
  !Array.isArray(v) &&
  !(v instanceof ExactJSONNumber);
const name = (v: unknown): v is string =>
  typeof v === "string" && /^[a-z][a-z0-9_.-]{0,63}$/.test(v);
const numeric = (v: unknown) =>
  v instanceof ExactJSONNumber || typeof v === "number";
const equal = (a: ConfigValue, b: ConfigValue) =>
  numeric(a) && numeric(b) ? compareExactNumbers(a, b) === 0 : a === b;
export function normalizeValues(
  form: ConfigForm,
  values: Record<string, unknown>,
): Record<string, ConfigValue> {
  const out: Record<string, ConfigValue> = {};
  for (const [key, value] of Object.entries(values)) {
    const f = form.fields[key];
    if (!own(form.fields, key))
      throw new Error("配置包含未声明字段，请重新加载。");
    out[key] =
      f.type === "number" || f.type === "integer"
        ? new ExactJSONNumber(exactNumberText(value))
        : (value as ConfigValue);
  }
  return out;
}
export function validateValues(
  form: ConfigForm,
  values: Record<string, unknown>,
): string {
  try {
    const normalized = normalizeValues(form, values);
    for (const [key, f] of Object.entries(form.fields)) {
      if (!own(values, key)) {
        if (form.required.includes(key)) return `请填写${f.title}。`;
        continue;
      }
      const value = normalized[key];
      if (f.type === "string" || f.type === "boolean") {
        if (typeof value !== f.type) return `${f.title}的类型不正确。`;
      } else {
        if (f.type === "integer" && !exactInteger(value))
          return `${f.title}必须是整数。`;
        if (
          f.minimum !== undefined &&
          compareExactNumbers(value, f.minimum) < 0
        )
          return `${f.title}低于最小值。`;
        if (
          f.maximum !== undefined &&
          compareExactNumbers(value, f.maximum) > 0
        )
          return `${f.title}超过最大值。`;
      }
      if (f.enum && !f.enum.some((v) => equal(v, value)))
        return `请选择有效的${f.title}。`;
      if (typeof value === "string") {
        const size = [...value].length;
        if (size > 512 || (f.maxLength !== undefined && size > f.maxLength))
          return `${f.title}最多允许 ${f.maxLength ?? 512} 个字符。`;
        if (f.minLength !== undefined && size < f.minLength)
          return `${f.title}至少需要 ${f.minLength} 个字符。`;
      }
      if (f.selector) {
        const available =
          f.selector === "profile"
            ? form.options.profiles
            : form.options.public_collections;
        if (!available.some((o) => o.id === value))
          return `${f.title}已不可选，请重新加载或移除此项。`;
      }
    }
    return "";
  } catch {
    return "数字格式、范围或配置字段无效，请检查输入。";
  }
}
export function configurationForm(
  entry: ConfigEntry,
  options = emptyConfigOptions(),
  scope: "system" | "user" = "system",
): ConfigForm {
  function fail(): never {
    throw new Error("当前配置定义不受支持或配置不可用，请重新加载。");
  }
  const m = entry.release?.manifest,
    c = entry.configuration,
    d = m?.configuration;
  if (
    m?.api_version !== "campusos.plugin/v5" ||
    ![scope, "both"].includes(m.data?.config_scope)
  )
    throw new Error(
      scope === "system"
        ? "此版本没有系统配置，请使用本人配置入口。"
        : "此版本没有本人配置。",
    );
  if (!entry.installation?.installed)
    throw new Error("请先安装该插件，再编辑配置。");
  if (
    !c ||
    typeof c.revision !== "string" ||
    !/^(0|[1-9][0-9]{0,18})$/.test(c.revision) ||
    (scope === "system" && c.owner_user_id !== "") ||
    (scope === "user" && !/^[1-9][0-9]*$/.test(c.owner_user_id))
  )
    fail();
  if (
    !obj(d) ||
    Object.keys(d).some(
      (k) =>
        ![
          "contract",
          "version",
          "schema",
          "defaults",
          "selectors",
          "secret_names",
        ].includes(k),
    ) ||
    d.contract !== "campusos.plugin-config/v1" ||
    !/^v[1-9][0-9]*$/.test(d.version)
  )
    fail();
  const s = d.schema;
  if (
    !obj(s) ||
    s.type !== "object" ||
    s.additionalProperties !== false ||
    !obj(s.properties) ||
    Object.keys(s).some(
      (k) =>
        !["type", "additionalProperties", "properties", "required"].includes(k),
    ) ||
    !Array.isArray(s.required) ||
    s.required.length > 32 ||
    new Set(s.required).size !== s.required.length
  )
    fail();
  if (
    !Array.isArray(d.selectors) ||
    d.selectors.length > 32 ||
    !Array.isArray(d.secret_names) ||
    d.secret_names.length > 32 ||
    !d.secret_names.every(name) ||
    new Set(d.secret_names).size !== d.secret_names.length ||
    !obj(c.secret_refs) ||
    !obj(c.values)
  )
    fail();
  const fields: Record<string, ConfigField> = {};
  if (Object.keys(s.properties).length > 32) fail();
  for (const [key, raw] of Object.entries(s.properties)) {
    if (
      !name(key) ||
      d.secret_names.includes(key) ||
      !obj(raw) ||
      !["string", "boolean", "integer", "number"].includes(raw.type) ||
      typeof raw.title !== "string" ||
      !raw.title ||
      [...raw.title].length > 128 ||
      Object.keys(raw).some(
        (k) =>
          ![
            "type",
            "title",
            "enum",
            "minimum",
            "maximum",
            "minLength",
            "maxLength",
          ].includes(k),
      )
    )
      fail();
    const field = { ...raw } as ConfigField;
    for (const bound of ["minLength", "maxLength"] as const)
      if (raw[bound] !== undefined) {
        if (raw.type !== "string" || !numeric(raw[bound])) fail();
        const n = Number(exactNumberText(raw[bound]));
        if (!Number.isInteger(n) || n < 0 || n > 512) fail();
        field[bound] = n;
      }
    for (const bound of ["minimum", "maximum"] as const)
      if (raw[bound] !== undefined) {
        if (!["integer", "number"].includes(raw.type) || !numeric(raw[bound]))
          fail();
        exactNumberText(raw[bound]);
      }
    if (
      (field.minLength !== undefined &&
        field.maxLength !== undefined &&
        field.minLength > field.maxLength) ||
      (field.minimum !== undefined &&
        field.maximum !== undefined &&
        compareExactNumbers(field.minimum, field.maximum) > 0)
    )
      fail();
    if (
      raw.enum !== undefined &&
      (!Array.isArray(raw.enum) ||
        raw.enum.length < 1 ||
        raw.enum.length > 128 ||
        raw.enum.some((v: unknown) =>
          raw.type === "string" || raw.type === "boolean"
            ? typeof v !== raw.type
            : !numeric(v) || (raw.type === "integer" && !exactInteger(v)),
        ))
    )
      fail();
    fields[key] = field;
  }
  for (const k of s.required) if (!name(k) || !own(fields, k)) fail();
  const seen = new Set<string>();
  for (const selector of d.selectors) {
    if (
      !obj(selector) ||
      !name(selector.field) ||
      !own(fields, selector.field) ||
      fields[selector.field].type !== "string" ||
      !["profile", "public_collection"].includes(selector.kind) ||
      seen.has(selector.field) ||
      Object.keys(selector).some((k) => !["field", "kind"].includes(k))
    )
      fail();
    seen.add(selector.field);
    fields[selector.field].selector = selector.kind;
  }
  const refs: Record<string, string> = {};
  for (const [key, value] of Object.entries(c.secret_refs)) {
    if (
      !d.secret_names.includes(key) ||
      typeof value !== "string" ||
      !/^secret-ref:[a-z0-9-]{2,128}$/.test(value) ||
      Object.values(refs).includes(value)
    )
      fail();
    refs[key] = value;
  }
  const form: ConfigForm = {
    fields,
    required: s.required,
    values: { ...c.values } as Record<string, ConfigValue>,
    secretNames: [...d.secret_names],
    secretRefs: refs,
    revision: c.revision,
    options,
  };
  // A removed selector is shown for repair; it is never silently cleared and
  // must be replaced/removed before the server accepts a values write.
  const withoutSelectors = {
    ...form,
    fields: Object.fromEntries(
      Object.entries(fields).map(([k, v]) => [
        k,
        { ...v, selector: undefined },
      ]),
    ),
  };
  const err = validateValues(withoutSelectors, form.values);
  if (err) throw new Error(err);
  return form;
}
