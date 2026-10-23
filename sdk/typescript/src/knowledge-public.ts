/** Managed transport only. Payloads are intent; the current host owns authority. */
export type KnowledgeHostTransport = <T>(capability: "knowledge.public.search" | "knowledge.public.source.read", payload: unknown, signal?: AbortSignal) => Promise<T>;
export interface KnowledgeSearchRequest { collection_id: string; query: string; page: number; page_size: number }
export interface KnowledgeSourceRequest { collection_id: string; member_id: string; revision: string }
export interface KnowledgeReference { kind: "community.thread" | "richtext.article" | "knowledge.official"; id: string; revision: string }
export interface KnowledgeCitation { collection_id: string; member_id: string; visibility_revision: string; source: KnowledgeReference; generation: string; chunk_index: number; url: string }
export interface KnowledgeSearchHit { title: string; snippet: string; score: number; explanation: { terms: string[]; title_terms: string[]; chunk_terms: string[]; title_weight: number; chunk_weight: number }; citation: KnowledgeCitation }
export interface KnowledgeSearchPage { items: KnowledgeSearchHit[]; total: number; page: number; page_size: number; provider: "builtin.lexical/v1"; generation: string }
export interface KnowledgeSource { collection_id: string; member_id: string; revision: string; source: { reference: KnowledgeReference; category_id: string; title: string; content: string; format: string; url: string } }
export const knowledgeSearch = (host: KnowledgeHostTransport, request: KnowledgeSearchRequest, signal?: AbortSignal) => host<KnowledgeSearchPage>("knowledge.public.search", request, signal);
export const knowledgeSource = (host: KnowledgeHostTransport, request: KnowledgeSourceRequest, signal?: AbortSignal) => host<KnowledgeSource>("knowledge.public.source.read", request, signal);
