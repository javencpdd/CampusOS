import api from "../../shared/api/client";

export type RuntimeConfigField = {
  key: string;
  label: string;
  group: string;
  kind: "boolean" | "text" | "secret" | "select" | "number";
  help: string;
  options?: string[];
  value: string;
  active_value?: string;
  configured: boolean;
  pending: boolean;
};
export type RuntimeConfigSnapshot = {
  available: boolean;
  revision: string;
  generation: string;
  pending: boolean;
  restarting: boolean;
  fields: RuntimeConfigField[];
  deployment_keys?: string[];
};
export const runtimeConfigApi = {
  read: () => api.get("/platform/runtime-config", { timeout: 4000 }),
  document: (data: { revision: string; verification_method: "password" | "mfa"; current_password?: string }) => api.post("/platform/runtime-config/document", data, { timeout: 15000 }),
  update: (data: { revision: string; changes?: Record<string, string>; document?: string; restart: boolean; verification_method?: "password" | "mfa"; current_password?: string }) =>
    api.put("/platform/runtime-config", data, { timeout: 15000 }),
};
