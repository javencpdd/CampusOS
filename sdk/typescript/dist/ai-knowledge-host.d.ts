import type { KnowledgeCitation } from "./knowledge-public.js";
/** A model Profile is selected by its finite capability, never an endpoint. */
export type ModelProfile = "local-chat" | "remote-chat";
export type AIKnowledgeCapability = "ai.chat.local.invoke" | "ai.chat.remote.invoke" | "knowledge.public.answer";
export type AIKnowledgeTransport = <T>(capability: AIKnowledgeCapability, payload: unknown, signal?: AbortSignal) => Promise<T>;
export interface ModelChatRequest {
    profile_id: ModelProfile;
    profile_revision: string;
    binding_revision: string;
    run_id: string;
    prompt: string;
}
export interface PublicAnswerRequest extends ModelChatRequest {
    collection_id: string;
}
export interface ModelChatAnswer {
    run_id: string;
    text: string;
    profile_id: ModelProfile;
}
export interface RAGEvidence {
    id: string;
    title: string;
    excerpt: string;
    citation: KnowledgeCitation;
}
export interface PublicRAGAnswer {
    contract: "campusos.knowledge-public-rag/v1";
    status: "answered" | "refused";
    reason?: string;
    answer: string;
    run_id?: string;
    citations: RAGEvidence[];
}
export declare const modelChat: (host: AIKnowledgeTransport, request: ModelChatRequest, signal?: AbortSignal) => Promise<ModelChatAnswer>;
export declare const knowledgeAnswer: (host: AIKnowledgeTransport, request: PublicAnswerRequest, signal?: AbortSignal) => Promise<PublicRAGAnswer>;
