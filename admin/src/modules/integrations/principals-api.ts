import api from "../../shared/api/client";
export interface MachineRule {
  id: string;
  channel: string;
  endpoint: string;
  action: string;
  resource_kind: string;
  resource_ids: string[];
  status: string;
  not_before_ms: number;
  expires_at_ms: number;
}
export interface MachinePrincipal {
  id: string;
  name: string;
  status: string;
  version: string;
}
export interface MachineCredential {
  id: string;
  principal_id: string;
  prefix: string;
  purpose: string;
  status: string;
  version: string;
  rules: MachineRule[];
  not_before_ms: number;
  expires_at_ms: number;
}
export interface MachineInventory {
  principals: MachinePrincipal[];
  credentials: MachineCredential[];
  next_cursor: string;
}
export interface MachineIssue {
  principal_version: string;
  not_before_ms: number;
  expires_at_ms: number;
  rules: MachineRule[];
}
export interface MachineIssueResult {
  credential: MachineCredential;
  token: string;
}
const base = "/admin/identity/integrations";
const path = (id: string) => `${base}/${encodeURIComponent(id)}`;
const keyPath = (id: string, key: string) =>
  `${path(id)}/credentials/${encodeURIComponent(key)}`;
export const machineApi = {
  list: (after = "") =>
    api.get<MachineInventory>(base, { params: after ? { after } : {} }),
  credentials: (id: string, after = "") =>
    api.get<MachineInventory>(`${path(id)}/credentials`, {
      params: after ? { after } : {},
    }),
  create: (name: string) => api.post<MachinePrincipal>(base, { name }),
  update: (p: MachinePrincipal, status: string) =>
    api.put<MachinePrincipal>(path(p.id), {
      name: p.name,
      version: p.version,
      status,
    }),
  issue: (id: string, data: MachineIssue) =>
    api.post<MachineIssueResult>(`${path(id)}/credentials`, data),
  rotate: (id: string, key: MachineCredential, data: MachineIssue) =>
    api.post<MachineIssueResult>(`${keyPath(id, key.id)}/rotate`, {
      ...data,
      version: key.version,
    }),
  scope: (id: string, key: MachineCredential, rules: MachineRule[]) =>
    api.put<MachineCredential>(keyPath(id, key.id), {
      version: key.version,
      rules,
    }),
  revoke: (id: string, key: MachineCredential) =>
    api.post<MachineCredential>(`${keyPath(id, key.id)}/revoke`, {
      version: key.version,
    }),
};
