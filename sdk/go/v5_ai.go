package campusos

import "context"

const V5AIChatLocal = "ai.chat.local.invoke"
const V5AIChatRemote = "ai.chat.remote.invoke"
const V5KnowledgeAnswer = "knowledge.public.answer"

type V5AIChatRequest struct {
	ProfileID       string `json:"profile_id"`
	ProfileRevision int64  `json:"profile_revision,string"`
	BindingRevision int64  `json:"binding_revision,string"`
	RunID           string `json:"run_id"`
	Prompt          string `json:"prompt"`
	CollectionID    string `json:"collection_id,omitempty"`
}
type V5AIChatAnswer struct {
	RunID     string `json:"run_id"`
	Text      string `json:"text"`
	ProfileID string `json:"profile_id"`
}
type V5RAGEvidence struct {
	ID       string              `json:"id"`
	Title    string              `json:"title"`
	Excerpt  string              `json:"excerpt"`
	Citation V5KnowledgeCitation `json:"citation"`
}
type V5RAGAnswer struct {
	Contract  string          `json:"contract"`
	Status    string          `json:"status"`
	Reason    string          `json:"reason,omitempty"`
	Answer    string          `json:"answer"`
	RunID     string          `json:"run_id,omitempty"`
	Citations []V5RAGEvidence `json:"citations"`
}

func ModelChat(ctx context.Context, host V5HostTransport, q V5AIChatRequest) (out V5AIChatAnswer, err error) {
	code := V5AIChatLocal
	if q.ProfileID == "remote-chat" {
		code = V5AIChatRemote
	} else if q.ProfileID != "local-chat" {
		return out, ErrV5ContainerChatProtocol
	}
	err = v5KnowledgeRead(ctx, host, code, q, &out)
	if err != nil {
		return V5AIChatAnswer{}, err
	}
	return out, nil
}
func KnowledgeAnswer(ctx context.Context, host V5HostTransport, q V5AIChatRequest) (out V5RAGAnswer, err error) {
	err = v5KnowledgeRead(ctx, host, V5KnowledgeAnswer, q, &out)
	if err != nil {
		return V5RAGAnswer{}, err
	}
	return out, nil
}
