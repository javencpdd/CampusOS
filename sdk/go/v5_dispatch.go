package campusos

// V5DispatchCall contains intent only. Credentials travel on the host transport;
// installations, grants, resource ACLs and current generations are host facts.
type V5DispatchCall struct {
	RequestID          string           `json:"request_id"`
	Contract           string           `json:"contract"`
	Target             V5DispatchTarget `json:"target"`
	InstanceGeneration int64            `json:"instance_generation"`
	DeadlineMS         int64            `json:"deadline_ms"`
	RequestBytes       int64            `json:"request_bytes"`
	ResponseLimitBytes int64            `json:"response_limit_bytes"`
}
type V5DispatchTarget struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type V5PreviewRequest struct {
	RequestID          string `json:"request_id"`
	ContributionID     string `json:"contribution_id"`
	InstanceGeneration int64  `json:"instance_generation"`
	DeadlineMS         int64  `json:"deadline_ms"`
	ResourceRef        string `json:"resource_ref"`
	MimeType           string `json:"mime_type"`
}
type V5PreviewResponse struct {
	RequestID          string `json:"request_id"`
	ContributionID     string `json:"contribution_id"`
	InstanceGeneration int64  `json:"instance_generation"`
	UISlotID           string `json:"ui_slot_id"`
	ResourceRevision   string `json:"resource_revision"`
}
type V5KnowledgeRequest struct {
	RequestID          string `json:"request_id"`
	ContributionID     string `json:"contribution_id"`
	InstanceGeneration int64  `json:"instance_generation"`
	DeadlineMS         int64  `json:"deadline_ms"`
	BindingID          string `json:"binding_id"`
	Cursor             string `json:"cursor,omitempty"`
	Limit              int64  `json:"limit"`
}
type V5KnowledgeItem struct {
	ExternalID string `json:"external_id"`
	Revision   string `json:"revision"`
	Title      string `json:"title"`
	Text       string `json:"text"`
	SourceURL  string `json:"source_url,omitempty"`
}
type V5KnowledgeResponse struct {
	RequestID          string            `json:"request_id"`
	ContributionID     string            `json:"contribution_id"`
	InstanceGeneration int64             `json:"instance_generation"`
	Items              []V5KnowledgeItem `json:"items"`
	NextCursor         string            `json:"next_cursor,omitempty"`
}
type V5SafeEvent struct {
	Contract string `json:"contract"`
	EventID  string `json:"event_id"`
	Type     string `json:"type"`
	// Revision identifies the committed notification, not a resource version or replay cursor.
	Revision    int64              `json:"revision"`
	CreatedAtMS int64              `json:"created_at_ms"`
	ExpiresAtMS int64              `json:"expires_at_ms"`
	Payload     V5SafeEventPayload `json:"payload"`
}
type V5SafeEventPayload struct {
	ResourceRef string `json:"resource_ref"`
	State       string `json:"state"`
}
