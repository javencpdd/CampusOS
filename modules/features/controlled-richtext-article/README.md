# Controlled RichText Article built-in feature

Controlled RichText Article is compiled into CampusOS and implemented in
`internal/modules/features/richtext`. Its descriptor and configuration belong
to the Built-in Feature Registry, not the external Plugin Manager.

The feature provides drafts, editing, preview, image upload, publication,
private visibility, offline state, deletion, and administration. Plain text
and rich-text threads share the Community content-governance state machine, so
an author cannot bypass moderation by switching format or republishing.

Rich content metadata is stored in PostgreSQL. Images are written through User
Storage Core to:

```text
data/personal-space/<user_id>/img/richtext/
```

Saving, previewing, publishing, and rendering all pass through the backend
sanitizer. Scripts, event handlers, unsafe URLs, unsupported tags, and
dangerous CSS are rejected or removed.

Disabling the feature rejects rich-text APIs and hides its UI while retaining
articles and assets. Existing plain-text community content remains available.

Validate with:

```bash
GOCACHE=/tmp/campusos-go-cache go test ./internal/modules/features/richtext -count=1
```
