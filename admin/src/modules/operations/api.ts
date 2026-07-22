import api from "../../shared/api/client";

export const eventApi = {
  list: (params?: { limit?: number }) => api.get("/events", { params }),
};

export const platformLogApi = {
  sources: () => api.get("/platform/logs/sources"),
  streamUrl: (params: { source: string; lines?: number; follow?: boolean }) => {
    const search = new URLSearchParams();
    search.set("source", params.source);
    search.set("lines", String(params.lines || 200));
    search.set("follow", params.follow === false ? "false" : "true");
    return `/api/v1/platform/logs/stream?${search.toString()}`;
  },
};

export const healthApi = {
  check: () => api.get("/health"),
};

type PageParams = { page?: number; page_size?: number; limit?: number };

export const reliabilityApi = {
  summary: () => api.get("/platform/reliability/summary"),
  emailDeliveryStatus: () => api.get("/platform/email-delivery/status"),
  events: (params?: PageParams & { status?: string; type?: string }) =>
    api.get("/platform/reliability/events", { params }),
  attempts: (params?: PageParams & { event_id?: string }) =>
    api.get("/platform/reliability/attempts", { params }),
  workers: (params?: PageParams) =>
    api.get("/platform/reliability/workers", { params }),
  operations: (params?: PageParams & { kind?: string }) =>
    api.get("/platform/reliability/operations", { params }),
  commandAudits: (params?: PageParams) =>
    api.get("/platform/reliability/command-audits", { params }),
  compatibility: (params?: PageParams) =>
    api.get("/platform/reliability/compatibility", { params }),
  retentionPreview: (params?: { target?: string; before?: string }) =>
    api.get("/platform/reliability/retention-preview", { params }),
  retentionRuns: (params?: PageParams) =>
    api.get("/platform/reliability/retention-runs", { params }),
  startRetentionPreview: (params?: { target?: string; before?: string }) =>
    api.post("/platform/reliability/retention-runs/preview", null, { params }),
  replay: (id: string, idempotencyKey: string) =>
    api.post(
      `/platform/reliability/events/${encodeURIComponent(id)}/replay`,
      null,
      { headers: { "Idempotency-Key": idempotencyKey } },
    ),
};
