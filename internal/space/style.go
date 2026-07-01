package space

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const StyleSchemaVersion = "space-style.v1"

var (
	styleNamePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
	styleVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,2}(-[a-zA-Z0-9.-]+)?$`)
)

type StylePackage struct {
	Manifest StyleManifest `json:"manifest" binding:"required"`
}

type StyleExportRequest struct {
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
}

type StyleExportResult struct {
	Package    StylePackage          `json:"package"`
	Filename   string                `json:"filename"`
	Validation StyleValidationResult `json:"validation"`
}

type StyleApplyResult struct {
	Validation StyleValidationResult `json:"validation"`
	Owner      Owner                 `json:"owner"`
	Space      *Space                `json:"space"`
	Applied    *StyleManifest        `json:"applied,omitempty"`
}

type StyleManifest struct {
	SchemaVersion      string            `json:"schema_version"`
	Name               string            `json:"name"`
	Version            string            `json:"version"`
	Author             string            `json:"author,omitempty"`
	Description        string            `json:"description,omitempty"`
	PreviewImage       string            `json:"preview_image,omitempty"`
	CompatibleCampusOS []string          `json:"compatible_campusos,omitempty"`
	Layout             string            `json:"layout"`
	Components         []StyleComponent  `json:"components"`
	Tokens             map[string]string `json:"tokens,omitempty"`
	Assets             []StyleAsset      `json:"assets,omitempty"`
}

type StyleComponent struct {
	Slot  string                 `json:"slot"`
	Type  string                 `json:"type"`
	Props map[string]interface{} `json:"props,omitempty"`
}

type StyleAsset struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

type StyleValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type StylePreview struct {
	Validation StyleValidationResult `json:"validation"`
	Owner      Owner                 `json:"owner,omitempty"`
	Space      *Space                `json:"space,omitempty"`
	Manifest   *StyleManifest        `json:"manifest,omitempty"`
	Layout     string                `json:"layout,omitempty"`
	Components []StyleComponent      `json:"components,omitempty"`
	Tokens     map[string]string     `json:"tokens,omitempty"`
	Assets     []StyleAsset          `json:"assets,omitempty"`
}

func ValidateStylePackage(pkg StylePackage) StyleValidationResult {
	validator := &styleValidator{}
	validator.validateManifest(NormalizeStyleManifest(pkg.Manifest))
	return StyleValidationResult{
		Valid:    len(validator.errors) == 0,
		Errors:   validator.errors,
		Warnings: validator.warnings,
	}
}

func NormalizeStyleManifest(manifest StyleManifest) StyleManifest {
	normalized := manifest
	normalized.SchemaVersion = strings.TrimSpace(normalized.SchemaVersion)
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.Version = strings.TrimSpace(normalized.Version)
	normalized.Author = strings.TrimSpace(normalized.Author)
	normalized.Description = strings.TrimSpace(normalized.Description)
	normalized.PreviewImage = strings.TrimSpace(normalized.PreviewImage)
	normalized.Layout = strings.TrimSpace(normalized.Layout)
	if normalized.Layout == "" {
		normalized.Layout = "blog"
	}
	normalized.CompatibleCampusOS = normalizeList(normalized.CompatibleCampusOS, 10)
	normalized.Components = normalizeComponents(normalized.Components)
	normalized.Tokens = normalizeTokens(normalized.Tokens)
	normalized.Assets = normalizeAssets(normalized.Assets)
	return normalized
}

func BuildStylePreview(owner Owner, space *Space, pkg StylePackage) StylePreview {
	validation := ValidateStylePackage(pkg)
	preview := StylePreview{
		Validation: validation,
		Owner:      owner,
		Space:      cloneSpace(space),
	}
	if !validation.Valid {
		return preview
	}

	manifest := NormalizeStyleManifest(pkg.Manifest)
	preview.Manifest = &manifest
	preview.Layout = manifest.Layout
	preview.Components = append([]StyleComponent(nil), manifest.Components...)
	preview.Tokens = copyStringMap(manifest.Tokens)
	preview.Assets = append([]StyleAsset(nil), manifest.Assets...)
	return preview
}

func BuildStyleExport(owner Owner, space *Space, req StyleExportRequest) StyleExportResult {
	if space == nil {
		space = &Space{}
	}

	if space.StyleManifest != nil && len(space.StyleManifest.Components) > 0 {
		manifest := NormalizeStyleManifest(*space.StyleManifest)
		if name := slugStyleName(req.Name); name != "" {
			manifest.Name = name
		}
		if version := strings.TrimSpace(req.Version); version != "" {
			manifest.Version = version
		}
		if description := strings.TrimSpace(req.Description); description != "" {
			manifest.Description = truncateRunes(description, 240)
		}
		if manifest.Author == "" {
			manifest.Author = exportAuthor(owner)
		}
		pkg := StylePackage{Manifest: manifest}
		return StyleExportResult{
			Package:    pkg,
			Filename:   manifest.Name + "-" + manifest.Version + ".space-style.json",
			Validation: ValidateStylePackage(pkg),
		}
	}

	name := slugStyleName(req.Name)
	if name == "" {
		name = slugStyleName(owner.Username + "-space")
	}
	if name == "" {
		name = "space-style"
	}

	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = "0.1.0"
	}

	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = "Exported CampusOS personal space style."
	}
	description = truncateRunes(description, 240)

	manifest := NormalizeStyleManifest(StyleManifest{
		SchemaVersion:      StyleSchemaVersion,
		Name:               name,
		Version:            version,
		Author:             exportAuthor(owner),
		Description:        description,
		CompatibleCampusOS: []string{">=0.4.0"},
		Layout:             exportLayout(space.Layout),
		Components:         exportComponents(space),
		Tokens:             exportTokens(space.Theme),
	})
	pkg := StylePackage{Manifest: manifest}
	return StyleExportResult{
		Package:    pkg,
		Filename:   manifest.Name + "-" + manifest.Version + ".space-style.json",
		Validation: ValidateStylePackage(pkg),
	}
}

func BuildStyleApply(owner Owner, space *Space, pkg StylePackage) StyleApplyResult {
	validation := ValidateStylePackage(pkg)
	result := StyleApplyResult{
		Validation: validation,
		Owner:      owner,
		Space:      cloneSpace(space),
	}
	if !validation.Valid {
		return result
	}
	manifest := NormalizeStyleManifest(pkg.Manifest)
	result.Applied = &manifest
	return result
}

type styleValidator struct {
	errors   []string
	warnings []string
}

func (v *styleValidator) validateManifest(manifest StyleManifest) {
	if strings.TrimSpace(manifest.SchemaVersion) == "" {
		v.addError("manifest.schema_version is required")
	} else if manifest.SchemaVersion != StyleSchemaVersion {
		v.addError(fmt.Sprintf("manifest.schema_version must be %q", StyleSchemaVersion))
	}
	if !styleNamePattern.MatchString(manifest.Name) {
		v.addError("manifest.name must use lowercase letters, numbers and hyphens")
	}
	if !styleVersionPattern.MatchString(manifest.Version) {
		v.addError("manifest.version must be a semantic version like 0.1.0")
	}
	if strings.TrimSpace(manifest.Layout) == "" {
		manifest.Layout = "blog"
	}
	if !allowedLayout(manifest.Layout) {
		v.addError(fmt.Sprintf("manifest.layout %q is not supported", manifest.Layout))
	}
	if len(manifest.Components) == 0 {
		v.addError("manifest.components must contain at least one component")
	}
	if len(manifest.Components) > 30 {
		v.addError("manifest.components must not contain more than 30 components")
	}
	if len(manifest.Tokens) > 80 {
		v.addError("manifest.tokens must not contain more than 80 tokens")
	}
	if len(manifest.Assets) > 20 {
		v.addError("manifest.assets must not contain more than 20 assets")
	}
	if manifest.PreviewImage != "" {
		v.validateRelativePath("manifest.preview_image", manifest.PreviewImage)
	}

	for i, component := range manifest.Components {
		v.validateComponent(i, component)
	}
	for key, value := range manifest.Tokens {
		v.validateToken(key, value)
	}
	for i, asset := range manifest.Assets {
		v.validateAsset(i, asset)
	}
	if len(manifest.CompatibleCampusOS) == 0 {
		v.addWarning("manifest.compatible_campusos is empty; imports will assume current v0.4 compatibility")
	}
}

func (v *styleValidator) validateComponent(index int, component StyleComponent) {
	prefix := fmt.Sprintf("manifest.components[%d]", index)
	if !allowedSlot(component.Slot) {
		v.addError(fmt.Sprintf("%s.slot %q is not supported", prefix, component.Slot))
	}
	if !allowedComponentType(component.Type) {
		v.addError(fmt.Sprintf("%s.type %q is not supported", prefix, component.Type))
	}
	if len(component.Props) > 30 {
		v.addError(fmt.Sprintf("%s.props must not contain more than 30 keys", prefix))
	}
	for key, value := range component.Props {
		v.validateProp(prefix, key, value)
	}
}

func (v *styleValidator) validateProp(prefix, key string, value interface{}) {
	normalizedKey := strings.ToLower(strings.TrimSpace(key))
	if normalizedKey == "" {
		v.addError(fmt.Sprintf("%s.props contains an empty key", prefix))
		return
	}
	if strings.HasPrefix(normalizedKey, "on") || strings.Contains(normalizedKey, "html") || strings.Contains(normalizedKey, "script") {
		v.addError(fmt.Sprintf("%s.props.%s is not allowed", prefix, key))
		return
	}
	v.validateValue(fmt.Sprintf("%s.props.%s", prefix, key), value)
}

func (v *styleValidator) validateValue(path string, value interface{}) {
	switch value := value.(type) {
	case string:
		if dangerousString(value) {
			v.addError(fmt.Sprintf("%s contains unsafe content", path))
		}
	case []interface{}:
		if len(value) > 50 {
			v.addError(fmt.Sprintf("%s contains too many items", path))
		}
		for i, item := range value {
			v.validateValue(fmt.Sprintf("%s[%d]", path, i), item)
		}
	case map[string]interface{}:
		if len(value) > 30 {
			v.addError(fmt.Sprintf("%s contains too many keys", path))
		}
		for key, item := range value {
			v.validateProp(path, key, item)
		}
	}
}

func (v *styleValidator) validateToken(key, value string) {
	key = strings.TrimSpace(key)
	if !allowedTokenKey(key) {
		v.addError(fmt.Sprintf("manifest.tokens.%s is not supported", key))
	}
	if strings.TrimSpace(value) == "" || len(value) > 120 {
		v.addError(fmt.Sprintf("manifest.tokens.%s has invalid value length", key))
		return
	}
	if dangerousString(value) || strings.Contains(strings.ToLower(value), "url(") || strings.Contains(strings.ToLower(value), "expression(") {
		v.addError(fmt.Sprintf("manifest.tokens.%s contains unsafe content", key))
	}
}

func (v *styleValidator) validateAsset(index int, asset StyleAsset) {
	prefix := fmt.Sprintf("manifest.assets[%d]", index)
	if !styleNamePattern.MatchString(asset.Name) {
		v.addError(fmt.Sprintf("%s.name must use lowercase letters, numbers and hyphens", prefix))
	}
	if !allowedAssetType(asset.Type) {
		v.addError(fmt.Sprintf("%s.type %q is not supported", prefix, asset.Type))
	}
	v.validateRelativePath(prefix+".path", asset.Path)
}

func (v *styleValidator) validateRelativePath(field, value string) {
	clean, ok := cleanStylePath(value)
	if !ok {
		v.addError(fmt.Sprintf("%s must be a safe relative path", field))
		return
	}
	ext := strings.ToLower(path.Ext(clean))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
		v.addError(fmt.Sprintf("%s must point to png, jpg, jpeg or webp asset", field))
	}
}

func (v *styleValidator) addError(message string) {
	v.errors = append(v.errors, message)
}

func (v *styleValidator) addWarning(message string) {
	v.warnings = append(v.warnings, message)
}

func allowedLayout(layout string) bool {
	switch layout {
	case "blog", "grid", "timeline", "magazine":
		return true
	default:
		return false
	}
}

func allowedSlot(slot string) bool {
	switch slot {
	case "header", "main", "sidebar", "footer":
		return true
	default:
		return false
	}
}

func allowedComponentType(componentType string) bool {
	switch componentType {
	case "profile-header", "content-list", "category-tabs", "tag-cloud", "rich-text", "link-list", "hero", "footer":
		return true
	default:
		return false
	}
}

func allowedTokenKey(key string) bool {
	switch {
	case strings.HasPrefix(key, "color."):
		return true
	case strings.HasPrefix(key, "font."):
		return true
	case strings.HasPrefix(key, "space."):
		return true
	case strings.HasPrefix(key, "radius."):
		return true
	case strings.HasPrefix(key, "shadow."):
		return true
	default:
		return false
	}
}

func allowedAssetType(assetType string) bool {
	switch strings.ToLower(strings.TrimSpace(assetType)) {
	case "image/png", "image/jpeg", "image/webp":
		return true
	default:
		return false
	}
}

func cleanStylePath(value string) (string, bool) {
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	lower := strings.ToLower(strings.TrimSpace(normalized))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//") {
		return "", false
	}
	clean := path.Clean(normalized)
	if clean == "." || clean == "" || clean == ".." || path.IsAbs(clean) || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func dangerousString(value string) bool {
	lower := strings.ToLower(value)
	dangerous := []string{
		"<script",
		"</script",
		"javascript:",
		"data:text/html",
		"vbscript:",
		"onerror=",
		"onload=",
		"<iframe",
	}
	for _, marker := range dangerous {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizeComponents(components []StyleComponent) []StyleComponent {
	normalized := make([]StyleComponent, 0, len(components))
	for _, component := range components {
		item := StyleComponent{
			Slot:  strings.TrimSpace(component.Slot),
			Type:  strings.TrimSpace(component.Type),
			Props: normalizeProps(component.Props),
		}
		normalized = append(normalized, item)
	}
	return normalized
}

func normalizeProps(props map[string]interface{}) map[string]interface{} {
	if len(props) == 0 {
		return nil
	}
	normalized := make(map[string]interface{}, len(props))
	for key, value := range props {
		normalized[strings.TrimSpace(key)] = value
	}
	return normalized
}

func normalizeTokens(tokens map[string]string) map[string]string {
	if len(tokens) == 0 {
		return nil
	}
	normalized := make(map[string]string, len(tokens))
	for key, value := range tokens {
		normalized[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return normalized
}

func normalizeAssets(assets []StyleAsset) []StyleAsset {
	normalized := make([]StyleAsset, 0, len(assets))
	for _, asset := range assets {
		normalized = append(normalized, StyleAsset{
			Name: strings.TrimSpace(asset.Name),
			Path: strings.TrimSpace(asset.Path),
			Type: strings.TrimSpace(asset.Type),
		})
	}
	return normalized
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func slugStyleName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastHyphen := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen && builder.Len() > 0 {
			builder.WriteByte('-')
			lastHyphen = true
		}
		if builder.Len() >= 63 {
			break
		}
	}
	name := strings.Trim(builder.String(), "-")
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "-")
	}
	if len(name) < 2 {
		return ""
	}
	return name
}

func exportAuthor(owner Owner) string {
	if strings.TrimSpace(owner.Nickname) != "" {
		return strings.TrimSpace(owner.Nickname)
	}
	if strings.TrimSpace(owner.Username) != "" {
		return strings.TrimSpace(owner.Username)
	}
	return "CampusOS User"
}

func exportLayout(layout string) string {
	layout = strings.TrimSpace(layout)
	if allowedLayout(layout) {
		return layout
	}
	return "blog"
}

func exportComponents(space *Space) []StyleComponent {
	layout := exportLayout(space.Layout)
	components := []StyleComponent{
		{
			Slot: "header",
			Type: "profile-header",
			Props: map[string]interface{}{
				"align":       "center",
				"show_avatar": true,
				"show_cover":  strings.TrimSpace(space.CoverImage) != "",
			},
		},
	}

	switch layout {
	case "grid":
		components = append(components,
			StyleComponent{
				Slot: "main",
				Type: "category-tabs",
				Props: map[string]interface{}{
					"show_all": true,
				},
			},
			StyleComponent{
				Slot: "main",
				Type: "content-list",
				Props: map[string]interface{}{
					"columns":      3,
					"density":      "compact",
					"show_excerpt": true,
				},
			},
		)
	case "timeline":
		components = append(components, StyleComponent{
			Slot: "main",
			Type: "content-list",
			Props: map[string]interface{}{
				"variant":      "timeline",
				"show_excerpt": true,
				"show_meta":    true,
			},
		})
	case "magazine":
		components = []StyleComponent{
			{
				Slot: "header",
				Type: "hero",
				Props: map[string]interface{}{
					"height":      "medium",
					"show_avatar": true,
				},
			},
			{
				Slot: "main",
				Type: "content-list",
				Props: map[string]interface{}{
					"variant":      "featured",
					"density":      "comfortable",
					"show_excerpt": true,
				},
			},
		}
	default:
		components = append(components, StyleComponent{
			Slot: "main",
			Type: "content-list",
			Props: map[string]interface{}{
				"density":      "comfortable",
				"show_excerpt": true,
				"show_meta":    true,
			},
		})
	}

	if len(space.SyncTags) > 0 {
		components = append(components, StyleComponent{
			Slot: "sidebar",
			Type: "tag-cloud",
			Props: map[string]interface{}{
				"max_items": 20,
			},
		})
	}
	return components
}

func exportTokens(theme string) map[string]string {
	tokens := map[string]string{
		"color.primary":    "#2563eb",
		"color.background": "#ffffff",
		"color.surface":    "#f8fafc",
		"font.body":        "system-ui",
		"radius.card":      "8px",
		"space.section":    "24px",
	}
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case "ink":
		tokens["color.primary"] = "#111827"
		tokens["color.surface"] = "#f3f4f6"
	case "dark":
		tokens["color.primary"] = "#60a5fa"
		tokens["color.background"] = "#111827"
		tokens["color.surface"] = "#1f2937"
	case "warm":
		tokens["color.primary"] = "#b45309"
		tokens["color.background"] = "#fff7ed"
		tokens["color.surface"] = "#ffedd5"
	}
	return tokens
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
