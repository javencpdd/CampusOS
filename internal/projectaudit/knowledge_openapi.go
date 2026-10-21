package projectaudit

func knowledgeProfile(operation string) (operationProfile, bool) {
	switch operation {
	case "http.knowledge.collection.list":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeCollectionList", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.source.list":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeSourcePage", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.source.read":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeSource", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.collection.inventory":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeCollectionList", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.collection.read":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeState", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.official.inventory":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeOfficialList", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.official.history":
		return operationProfile{RequestSchema: "", ResponseSchema: "KnowledgeHistory", NoBody: true, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.collection.create":
		return operationProfile{RequestSchema: "KnowledgeCollectionCreate", ResponseSchema: "KnowledgeCollection", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.collection.update":
		return operationProfile{RequestSchema: "KnowledgeCollectionUpdate", ResponseSchema: "KnowledgeCollection", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.official.create":
		return operationProfile{RequestSchema: "KnowledgeOfficialCreate", ResponseSchema: "KnowledgeOfficial", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.official.update":
		return operationProfile{RequestSchema: "KnowledgeOfficialUpdate", ResponseSchema: "KnowledgeOfficial", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.selection.create":
		return operationProfile{RequestSchema: "KnowledgeSelectionCreate", ResponseSchema: "KnowledgeState", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.selection.update":
		return operationProfile{RequestSchema: "KnowledgeSelectionUpdate", ResponseSchema: "KnowledgeState", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.selection.refresh":
		return operationProfile{RequestSchema: "KnowledgeRefreshRequest", ResponseSchema: "KnowledgeRefresh", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	case "http.knowledge.member.remove":
		return operationProfile{RequestSchema: "KnowledgeRemoveMember", ResponseSchema: "KnowledgeState", NoBody: false, AdditionalErrors: []string{"401", "403", "404", "409", "503"}}, true
	}
	return operationProfile{}, false
}

func knowledgeOpenAPISchemas() string {
	return `    KnowledgeCollectionCreate:
      type: object
      additionalProperties: false
      required:
      - title
      - status
      properties:
        title: &id001
          type: string
          minLength: 1
          maxLength: 1024
          description: UTF-8 bytes <=1024; nonblank and single line.
        status: &id002
          type: string
          enum:
          - active
          - disabled
    KnowledgeCollectionUpdate:
      type: object
      additionalProperties: false
      required:
      - title
      - status
      - version
      properties:
        title: *id001
        status: *id002
        version: &id006
          type: string
          pattern: ^[1-9][0-9]*$
    KnowledgeOfficialCreate:
      type: object
      additionalProperties: false
      required:
      - title
      - content
      - format
      - status
      properties:
        title: *id001
        content: &id003
          type: string
          minLength: 1
          maxLength: 65536
          description: UTF-8 bytes <=65536, text/Markdown data only; never interpreted
            or fetched.
        format: &id004
          type: string
          enum:
          - text
          - markdown
        status: &id005
          type: string
          enum:
          - draft
          - published
          - withdrawn
    KnowledgeOfficialUpdate:
      type: object
      additionalProperties: false
      required:
      - title
      - content
      - format
      - status
      - version
      properties:
        title: *id001
        content: *id003
        format: *id004
        status: *id005
        version: *id006
    KnowledgeSelectionCreate:
      type: object
      additionalProperties: false
      required:
      - kind
      - source_id
      - active
      - collection_version
      properties:
        kind: &id007
          type: string
          enum:
          - thread
          - board
          - official
        source_id: &id008
          type: string
        active: &id009
          type: boolean
        collection_version: *id006
    KnowledgeSelectionUpdate:
      type: object
      additionalProperties: false
      required:
      - kind
      - source_id
      - active
      - collection_version
      - version
      properties:
        kind: *id007
        source_id: *id008
        active: *id009
        collection_version: *id006
        version: *id006
    KnowledgeCollection:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - title
      - status
      - created_by_admin_id
      properties:
        id: *id008
        version: *id006
        title: *id001
        status: *id002
        created_by_admin_id: *id008
    KnowledgeOfficial:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - title
      - content
      - format
      - status
      - created_by_admin_id
      properties:
        id: *id008
        version: *id006
        title: *id001
        content: *id003
        format: *id004
        status: *id005
        created_by_admin_id: *id008
    KnowledgeSelection:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - collection_id
      - kind
      - source_id
      - active
      properties:
        id: *id008
        version: *id006
        collection_id: *id008
        kind: *id007
        source_id: *id008
        active:
          type: boolean
    KnowledgeMember:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - collection_id
      - selection_id
      - selection_version
      - kind
      - source_id
      - source_revision
      properties:
        id: *id008
        version: *id006
        collection_id: *id008
        selection_id: *id008
        selection_version: *id006
        kind: &id010
          type: string
          enum:
          - community.thread
          - richtext.article
          - knowledge.official
        source_id: *id008
        source_revision: *id008
    KnowledgeState:
      type: object
      additionalProperties: false
      required:
      - collection
      - selections
      - members
      properties:
        collection:
          $ref: '#/components/schemas/KnowledgeCollection'
        selections:
          type: array
          items:
            $ref: '#/components/schemas/KnowledgeSelection'
        members:
          type: array
          items:
            $ref: '#/components/schemas/KnowledgeMember'
    KnowledgeRefreshRequest:
      type: object
      additionalProperties: false
      required:
      - collection_version
      - selection_version
      - page
      properties:
        collection_version: *id006
        selection_version: *id006
        page:
          type: integer
          minimum: 1
          maximum: 100000
    KnowledgeRefresh:
      type: object
      additionalProperties: false
      required:
      - state
      - page
      - next_page
      - examined
      - materialized
      - skipped_unsupported
      - has_more
      properties:
        state:
          $ref: '#/components/schemas/KnowledgeState'
        page:
          type: integer
        next_page:
          type: integer
        examined:
          type: integer
        materialized:
          type: integer
        skipped_unsupported:
          type: integer
        has_more:
          type: boolean
    KnowledgeRemoveMember:
      type: object
      additionalProperties: false
      required:
      - version
      properties:
        version: *id006
    KnowledgeOfficialRevision:
      type: object
      additionalProperties: false
      required:
      - id
      - version
      - source_id
      - official_version
      - snapshot
      - changed_by_admin_id
      - changed_at_ms
      properties:
        id: *id008
        version: *id006
        source_id: *id008
        official_version: *id006
        snapshot:
          $ref: '#/components/schemas/KnowledgeOfficial'
        changed_by_admin_id: *id008
        changed_at_ms:
          type: integer
    KnowledgeHistory:
      type: object
      additionalProperties: false
      required:
      - items
      - next_before
      properties:
        items:
          type: array
          items:
            $ref: '#/components/schemas/KnowledgeOfficialRevision'
        next_before:
          type: string
          pattern: ^(0|[1-9][0-9]*)$
          description: 0 means end; pass other value as before for next 50 revisions.
    KnowledgeSourceReference:
      type: object
      additionalProperties: false
      required:
      - kind
      - id
      - revision
      properties:
        kind: *id010
        id: *id008
        revision: *id008
    KnowledgeCandidate:
      type: object
      additionalProperties: false
      required:
      - reference
      - category_id
      - title
      - content
      - format
      - url
      properties:
        reference:
          $ref: '#/components/schemas/KnowledgeSourceReference'
        category_id: *id008
        title: *id001
        content: *id003
        format: *id008
        url: *id008
    KnowledgeSource:
      type: object
      additionalProperties: false
      required:
      - member_id
      - collection_id
      - revision
      - source
      properties:
        member_id: *id008
        collection_id: *id008
        revision: *id008
        source:
          $ref: '#/components/schemas/KnowledgeCandidate'
    KnowledgeSourcePage:
      type: object
      additionalProperties: false
      required:
      - items
      - total
      - page
      - page_size
      properties:
        items:
          type: array
          items:
            $ref: '#/components/schemas/KnowledgeSource'
        total:
          type: integer
        page:
          type: integer
        page_size:
          type: integer
    KnowledgeCollectionList:
      type: array
      items:
        $ref: '#/components/schemas/KnowledgeCollection'
    KnowledgeOfficialList:
      type: array
      items:
        $ref: '#/components/schemas/KnowledgeOfficial'
`
}
