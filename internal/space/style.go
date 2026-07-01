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
