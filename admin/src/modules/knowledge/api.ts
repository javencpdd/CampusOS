import api from "../../shared/api/client";
export interface Collection {
  id: string;
  version: string;
  title: string;
  status: "active" | "disabled";
}
export interface Official {
  id: string;
  version: string;
  title: string;
  content: string;
  format: "text" | "markdown";
  status: "draft" | "published" | "withdrawn";
}
export interface Selection {
  id: string;
  version: string;
  kind: "thread" | "board" | "official";
  source_id: string;
  active: boolean;
}
export interface Member {
  id: string;
  kind: string;
  source_id: string;
  selection_id: string;
}
export interface CollectionState {
  collection: Collection;
  selections: Selection[];
  members: Member[];
}
export interface History {
  items: {
    official_version: string;
    snapshot: Official;
    changed_at_ms: number;
  }[];
  next_before: string;
}
export interface Diagnostics {
  collection_id: string;
  collection_version: string;
  job_version: string;
  state: string;
  phase: string;
  attempts: number;
  last_error: string;
  ready_generation: string;
  run_generation: string;
  available_at_ms: string;
  selections: number;
  active_selections: number;
  members: number;
  projected_documents: number;
  observed_at_ms: string;
  retryable: boolean;
}
async function data<T>(request: Promise<unknown>): Promise<T> {
  const envelope = (await request) as { code: number; data: T };
  if (envelope.code !== 0 || envelope.data === undefined)
    throw Error("Knowledge request failed");
  return envelope.data;
}
const id = (value: string) => encodeURIComponent(value);
const base = "/admin/knowledge";
export const knowledgeAdminApi = {
  collections: () => data<Collection[]>(api.get(base + "/collections")),
  officials: () => data<Official[]>(api.get(base + "/officials")),
  state: (collection: string) =>
    data<CollectionState>(api.get(`${base}/collections/${id(collection)}`)),
  diagnostics: (collection: string) =>
    data<Diagnostics>(api.get(`${base}/collections/${id(collection)}/index`)),
  saveCollection: (
    value: Pick<Collection, "title" | "status">,
    current?: Collection,
  ) =>
    data<Collection>(
      current
        ? api.put(`${base}/collections/${id(current.id)}`, {
            ...value,
            version: current.version,
          })
        : api.post(base + "/collections", value),
    ),
  saveOfficial: (
    value: Pick<Official, "title" | "content" | "format" | "status">,
    current?: Official,
  ) =>
    data<Official>(
      current
        ? api.put(`${base}/officials/${id(current.id)}`, {
            ...value,
            version: current.version,
          })
        : api.post(base + "/officials", value),
    ),
  history: (official: string, before?: string) =>
    data<History>(
      api.get(`${base}/officials/${id(official)}/revisions`, {
        params: before ? { before } : undefined,
      }),
    ),
  select: (
    state: CollectionState,
    value: Pick<Selection, "kind" | "source_id" | "active">,
    current?: Selection,
  ) =>
    data<CollectionState>(
      current
        ? api.put(
            `${base}/collections/${id(state.collection.id)}/selections/${id(current.id)}`,
            {
              ...value,
              collection_version: state.collection.version,
              version: current.version,
            },
          )
        : api.post(
            `${base}/collections/${id(state.collection.id)}/selections`,
            { ...value, collection_version: state.collection.version },
          ),
    ),
  refresh: (state: CollectionState, rule: Selection, page: number) =>
    data<{
      state: CollectionState;
      next_page: number;
      has_more: boolean;
      examined: number;
    }>(
      api.post(
        `${base}/collections/${id(state.collection.id)}/selections/${id(rule.id)}/refresh`,
        {
          collection_version: state.collection.version,
          selection_version: rule.version,
          page,
        },
      ),
    ),
  remove: (state: CollectionState, member: Member) =>
    data<CollectionState>(
      api.post(
        `${base}/collections/${id(state.collection.id)}/members/${id(member.id)}/remove`,
        { version: state.collection.version },
      ),
    ),
  retry: (snapshot: Diagnostics) =>
    data<{ accepted: true }>(
      api.post(
        `${base}/collections/${id(snapshot.collection_id)}/index/retry`,
        {
          collection_version: snapshot.collection_version,
          version: snapshot.job_version,
        },
      ),
    ),
};
