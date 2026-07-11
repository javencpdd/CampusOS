package stylepack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/campusos/CampusOS/internal/safehtml"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion    = "page-style-pack.v1"
	AppSchemaVersion = "campusos.app-style-pack.v2"

	MaxFiles        = 80
	MaxPackageBytes = 8 * 1024 * 1024
	MaxFileBytes    = 4 * 1024 * 1024
	MaxCSSBytes     = 20000
)

var (
	packNamePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
	packVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,2}(-[a-zA-Z0-9.-]+)?$`)
)

type Manifest struct {
	SchemaVersion      string            `json:"schema_version" yaml:"schema_version"`
	Target             string            `json:"target" yaml:"target"`
	Name               string            `json:"name" yaml:"name"`
	Version            string            `json:"version" yaml:"version"`
	DisplayName        string            `json:"display_name,omitempty" yaml:"display_name,omitempty"`
	Author             string            `json:"author,omitempty" yaml:"author,omitempty"`
	Description        string            `json:"description,omitempty" yaml:"description,omitempty"`
	CompatibleCampusOS []string          `json:"compatible_campusos,omitempty" yaml:"compatible_campusos,omitempty"`
	Entry              string            `json:"entry" yaml:"entry"`
	Templates          []Template        `json:"templates,omitempty" yaml:"templates,omitempty"`
	Styles             []string          `json:"styles,omitempty" yaml:"styles,omitempty"`
	PreviewImage       string            `json:"preview_image,omitempty" yaml:"preview_image,omitempty"`
	ConfigSchema       string            `json:"config_schema,omitempty" yaml:"config_schema,omitempty"`
	Tokens             map[string]string `json:"tokens,omitempty" yaml:"tokens,omitempty"`
	Assets             []Asset           `json:"assets,omitempty" yaml:"assets,omitempty"`
	Effect             *Effect           `json:"effect,omitempty" yaml:"effect,omitempty"`
	Capabilities       []string          `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Layout             *AppLayout        `json:"layout,omitempty" yaml:"layout,omitempty"`
	SurfaceOverrides   []SurfaceOverride `json:"surface_overrides,omitempty" yaml:"surface_overrides,omitempty"`
}

type AppLayout struct {
	Mode              string `json:"mode,omitempty" yaml:"mode,omitempty"`
	ContentWidth      string `json:"content_width,omitempty" yaml:"content_width,omitempty"`
	PagePadding       string `json:"page_padding,omitempty" yaml:"page_padding,omitempty"`
	HeaderMode        string `json:"header_mode,omitempty" yaml:"header_mode,omitempty"`
	ScrollMode        string `json:"scroll_mode,omitempty" yaml:"scroll_mode,omitempty"`
	BackgroundAsset   string `json:"background_asset,omitempty" yaml:"background_asset,omitempty"`
	Overlay           string `json:"overlay,omitempty" yaml:"overlay,omitempty"`
	AnimationPreset   string `json:"animation_preset,omitempty" yaml:"animation_preset,omitempty"`
	LeftSidebarWidth  string `json:"left_sidebar_width,omitempty" yaml:"left_sidebar_width,omitempty"`
	RightSidebarWidth string `json:"right_sidebar_width,omitempty" yaml:"right_sidebar_width,omitempty"`
}

type SurfaceOverride struct {
	SurfaceID string `json:"surface_id" yaml:"surface_id"`
	Variant   string `json:"variant,omitempty" yaml:"variant,omitempty"`
	Region    string `json:"region,omitempty" yaml:"region,omitempty"`
	Order     int    `json:"order,omitempty" yaml:"order,omitempty"`
}

type Effect struct {
	Runtime string `json:"runtime" yaml:"runtime"`
	Entry   string `json:"entry" yaml:"entry"`
	Source  string `json:"source,omitempty" yaml:"source,omitempty"`
}

type Template struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
}

type Asset struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
	Type string `json:"type" yaml:"type"`
}

type FileInfo struct {
	Path string `json:"path"`
	Size int    `json:"size"`
	Type string `json:"type"`
}

type Package struct {
	Manifest Manifest          `json:"manifest"`
	HTML     string            `json:"html"`
	CSS      string            `json:"css,omitempty"`
	EffectJS string            `json:"effect_js,omitempty"`
	Files    []FileInfo        `json:"files,omitempty"`
	RawFiles map[string]string `json:"-"`
}

type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

type FileBundle struct {
	Filename string            `json:"filename"`
	Files    map[string]string `json:"files"`
	Package  Package           `json:"package"`
}

type SourcePackInfo struct {
	Name        string           `json:"name"`
	Path        string           `json:"path"`
	Target      string           `json:"target,omitempty"`
	Version     string           `json:"version,omitempty"`
	DisplayName string           `json:"display_name,omitempty"`
	Description string           `json:"description,omitempty"`
	Manifest    *Manifest        `json:"manifest,omitempty"`
	Validation  ValidationResult `json:"validation"`
}

type SourcePackList struct {
	Items []SourcePackInfo `json:"items"`
}

func LoadDir(root string) (*Package, ValidationResult) {
	files := map[string][]byte{}
	var total int64
	var result ValidationResult

	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.addError(walkErr.Error())
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			result.addError(err.Error())
			return nil
		}
		rel = filepath.ToSlash(rel)
		if err := validateFilePath(rel); err != nil {
			result.addError(err.Error())
			return nil
		}
		if len(files) >= MaxFiles {
			result.addError(fmt.Sprintf("style pack must not contain more than %d files", MaxFiles))
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			result.addError(err.Error())
			return nil
		}
		if info.Size() > MaxFileBytes {
			result.addError(fmt.Sprintf("%s exceeds %d bytes", rel, MaxFileBytes))
			return nil
		}
		total += info.Size()
		if total > MaxPackageBytes {
			result.addError(fmt.Sprintf("style pack must not exceed %d bytes", MaxPackageBytes))
			return nil
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			result.addError(err.Error())
			return nil
		}
		files[rel] = data
		return nil
	})
	if err != nil {
		result.addError(err.Error())
	}
	if len(result.Errors) > 0 {
		return nil, result.finish()
	}
	return buildPackage(files)
}

func LoadZip(reader io.ReaderAt, size int64) (*Package, ValidationResult) {
	var result ValidationResult
	if size > MaxPackageBytes {
		result.addError(fmt.Sprintf("style pack zip must not exceed %d bytes", MaxPackageBytes))
		return nil, result.finish()
	}
	zr, err := zip.NewReader(reader, size)
	if err != nil {
		result.addError("zip cannot be read")
		return nil, result.finish()
	}
	if len(zr.File) > MaxFiles {
		result.addError(fmt.Sprintf("style pack must not contain more than %d files", MaxFiles))
		return nil, result.finish()
	}

	raw := map[string][]byte{}
	var total int64
	for _, file := range zr.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if err := validateFilePath(name); err != nil {
			result.addError(err.Error())
			continue
		}
		name = cleanPackPath(name)
		if file.UncompressedSize64 > MaxFileBytes {
			result.addError(fmt.Sprintf("%s exceeds %d bytes", name, MaxFileBytes))
			continue
		}
		total += int64(file.UncompressedSize64)
		if total > MaxPackageBytes {
			result.addError(fmt.Sprintf("style pack must not exceed %d bytes", MaxPackageBytes))
			continue
		}
		rc, err := file.Open()
		if err != nil {
			result.addError(err.Error())
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, MaxFileBytes+1))
		_ = rc.Close()
		if err != nil {
			result.addError(err.Error())
			continue
		}
		raw[name] = data
	}
	if len(result.Errors) > 0 {
		return nil, result.finish()
	}
	return buildPackage(stripArchiveRoot(raw))
}

func ListSourcePacks(pluginName string) ([]SourcePackInfo, error) {
	root := SourceRoot(pluginName)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []SourcePackInfo{}, nil
		}
		return nil, err
	}

	items := make([]SourcePackInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		fullPath := filepath.Join(root, name)
		item := SourcePackInfo{
			Name: name,
			Path: filepath.ToSlash(fullPath),
		}
		if !packNamePattern.MatchString(name) {
			validation := ValidationResult{
				Valid:  false,
				Errors: []string{"source style pack directory name must use lowercase letters, numbers and hyphens"},
			}
			item.Validation = validation.finish()
			items = append(items, item)
			continue
		}
		pack, validation := LoadDir(fullPath)
		if pack != nil {
			manifest := pack.Manifest
			item.Manifest = &manifest
			item.Target = manifest.Target
			item.Version = manifest.Version
			item.DisplayName = manifest.DisplayName
			item.Description = manifest.Description
			if item.DisplayName == "" {
				item.DisplayName = manifest.Name
			}
		}
		item.Validation = validation
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func BuildExample(target, name, displayName, title, subtitle, primary, background, surface string) FileBundle {
	name = normalizePackName(name)
	if name == "" {
		name = target + "-example"
	}
	if displayName == "" {
		displayName = name
	}
	if title == "" {
		title = displayName
	}
	if subtitle == "" {
		subtitle = "CampusOS page style pack example."
	}
	if primary == "" {
		primary = "#2563eb"
	}
	if background == "" {
		background = "#ffffff"
	}
	if surface == "" {
		surface = "#f8fafc"
	}
	scope := ScopeRootSelector(target)
	exampleCSS := fmt.Sprintf(`.cstyle-page {
  padding: 24px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: %s;
}
.cstyle-hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.cstyle-hero h2 {
  margin: 0;
  color: %s;
}
.cstyle-hero p {
  margin: 8px 0 0;
  color: #475569;
  line-height: 1.7;
}
.cstyle-badge {
  padding: 6px 10px;
  border-radius: 999px;
  background: %s;
  color: %s;
}
.cstyle-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  margin-top: 18px;
}
.cstyle-grid article {
  padding: 16px;
  border-radius: 8px;
  background: %s;
}
`, background, primary, surface, primary, surface)
	exampleCSS = scope + " " + strings.ReplaceAll(exampleCSS, "\n.cstyle", "\n"+scope+" .cstyle")

	files := map[string]string{
		"README.md": fmt.Sprintf(`# %s

Generated CampusOS page style pack example.

This folder follows the page-style-pack.v1 source package layout:

- style.yaml
- templates/page.html
- templates/card.html
- styles/theme.css
- assets/
- preview.png
- config.schema.json
`, displayName),
		"style.yaml": fmt.Sprintf(`schema_version: %s
target: %s
name: %s
version: 0.1.0
display_name: %q
author: CampusOS
description: Generated example style pack.
compatible_campusos:
  - ">=0.5.0"
entry: templates/page.html
templates:
  - name: page
    path: templates/page.html
  - name: card
    path: templates/card.html
styles:
  - styles/theme.css
preview_image: preview.png
config_schema: config.schema.json
tokens:
  color.primary: %q
  color.background: %q
  color.surface: %q
assets:
  - name: cover
    path: assets/cover.webp
    type: image/webp
  - name: avatar-frame
    path: assets/avatar-frame.png
    type: image/png
`, SchemaVersion, target, name, displayName, primary, background, surface),
		"templates/page.html": fmt.Sprintf(`<section class="cstyle-page">
  <header class="cstyle-hero">
    <div>
      <h2>%s</h2>
      <p>%s</p>
    </div>
    <span class="cstyle-badge">%s</span>
  </header>
  <div class="cstyle-grid">
    <article>
      <strong>精选内容</strong>
      <p>在这里放置主页介绍、导航入口、课程项目或社区板块说明。</p>
    </article>
    <article>
      <strong>可维护文件</strong>
      <p>修改 templates、styles、assets 和 config.schema.json 后重新校验，通过后再应用。</p>
    </article>
  </div>
</section>
`, escapeHTML(title), escapeHTML(subtitle), escapeHTML(target)),
		"templates/card.html": `<article class="cstyle-card">
  <strong>{{ title }}</strong>
  <p>{{ summary }}</p>
</article>
`,
		"config.schema.json": fmt.Sprintf(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "title": {
      "type": "string",
      "default": %q
    },
    "subtitle": {
      "type": "string",
      "default": %q
    },
    "primary_color": {
      "type": "string",
      "default": %q
    }
  }
}
`, title, subtitle, primary),
		"preview.png":             "CampusOS style pack preview placeholder.\n",
		"assets/cover.webp":       "CampusOS style pack cover placeholder.\n",
		"assets/avatar-frame.png": "CampusOS avatar frame placeholder.\n",
		"styles/theme.css":        exampleCSS,
	}
	pkg, result := BuildFromFiles(files)
	if !result.Valid || pkg == nil {
		pkg = &Package{}
	}
	return FileBundle{
		Filename: name + ".page-style-pack",
		Files:    files,
		Package:  *pkg,
	}
}

func BuildFromFiles(textFiles map[string]string) (*Package, ValidationResult) {
	files := make(map[string][]byte, len(textFiles))
	for name, content := range textFiles {
		files[name] = []byte(content)
	}
	return buildPackage(files)
}

func ZipBundle(bundle FileBundle) ([]byte, error) {
	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	names := make([]string, 0, len(bundle.Files))
	for name := range bundle.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := validateFilePath(name); err != nil {
			_ = zw.Close()
			return nil, err
		}
		writer, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := writer.Write([]byte(bundle.Files[name])); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func ValidateCSS(input string) ValidationResult {
	var result ValidationResult
	css := strings.TrimSpace(input)
	if css == "" {
		return ValidationResult{Valid: true}
	}
	if len(css) > MaxCSSBytes {
		result.addError(fmt.Sprintf("css must not exceed %d bytes", MaxCSSBytes))
		return result.finish()
	}
	lower := strings.ToLower(css)
	blocked := []string{
		"<",
		">",
		"@import",
		"expression(",
		"javascript:",
		"vbscript:",
		"data:",
		"behavior:",
		"-moz-binding",
		"position:fixed",
		"position: sticky",
		"position:sticky",
		"url(",
	}
	for _, marker := range blocked {
		if strings.Contains(lower, marker) {
			result.addError("css contains unsafe content: " + marker)
		}
	}
	if strings.Count(css, "{") != strings.Count(css, "}") {
		result.addError("css braces are not balanced")
	}
	return result.finish()
}

func buildPackage(files map[string][]byte) (*Package, ValidationResult) {
	var result ValidationResult
	manifestData, ok := files["style.yaml"]
	if !ok {
		if manifestData, ok = files["style.yml"]; !ok {
			result.addError("style.yaml is required")
			return nil, result.finish()
		}
	}
	var manifest Manifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		result.addError("style.yaml cannot be parsed")
		return nil, result.finish()
	}
	manifest = NormalizeManifest(manifest)
	pkg := &Package{
		Manifest: manifest,
		RawFiles: map[string]string{},
	}
	for name, data := range files {
		pkg.RawFiles[name] = string(data)
		pkg.Files = append(pkg.Files, FileInfo{Path: name, Size: len(data), Type: fileType(name)})
	}
	sort.Slice(pkg.Files, func(i, j int) bool { return pkg.Files[i].Path < pkg.Files[j].Path })

	validateManifest(pkg, files, &result)
	if result.Valid = len(result.Errors) == 0; !result.Valid {
		return pkg, result
	}
	pkg.HTML = strings.TrimSpace(string(files[manifest.Entry]))
	var cssParts []string
	for _, cssPath := range manifest.Styles {
		cssParts = append(cssParts, strings.TrimSpace(string(files[cssPath])))
	}
	pkg.CSS = strings.TrimSpace(strings.Join(cssParts, "\n\n"))
	if manifest.Effect != nil {
		pkg.EffectJS = strings.TrimSpace(string(files[manifest.Effect.Entry]))
	}
	return pkg, result.finish()
}

func NormalizeManifest(manifest Manifest) Manifest {
	manifest.SchemaVersion = strings.TrimSpace(manifest.SchemaVersion)
	manifest.Target = strings.TrimSpace(manifest.Target)
	manifest.Name = normalizePackName(manifest.Name)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.DisplayName = strings.TrimSpace(manifest.DisplayName)
	manifest.Author = strings.TrimSpace(manifest.Author)
	manifest.Description = strings.TrimSpace(manifest.Description)
	manifest.Entry = cleanPackPath(manifest.Entry)
	manifest.PreviewImage = cleanPackPath(manifest.PreviewImage)
	manifest.ConfigSchema = cleanPackPath(manifest.ConfigSchema)
	manifest.CompatibleCampusOS = normalizeList(manifest.CompatibleCampusOS, 10)
	manifest.Templates = normalizeTemplates(manifest.Templates, 20)
	manifest.Styles = normalizePaths(manifest.Styles, 8)
	manifest.Tokens = normalizeTokens(manifest.Tokens)
	manifest.Capabilities = normalizeList(manifest.Capabilities, 12)
	if manifest.Effect != nil {
		manifest.Effect.Runtime = strings.TrimSpace(manifest.Effect.Runtime)
		manifest.Effect.Entry = cleanPackPath(manifest.Effect.Entry)
		manifest.Effect.Source = cleanPackPath(manifest.Effect.Source)
	}
	for i := range manifest.Assets {
		manifest.Assets[i].Name = normalizePackName(manifest.Assets[i].Name)
		manifest.Assets[i].Path = cleanPackPath(manifest.Assets[i].Path)
		manifest.Assets[i].Type = strings.TrimSpace(manifest.Assets[i].Type)
	}
	return manifest
}

func validateManifest(pkg *Package, files map[string][]byte, result *ValidationResult) {
	manifest := pkg.Manifest
	if manifest.PreviewImage == "" {
		for _, candidate := range []string{"preview.png", "preview.jpg", "preview.jpeg", "preview.webp"} {
			if _, ok := files[candidate]; ok {
				manifest.PreviewImage = candidate
				break
			}
		}
	}
	if manifest.ConfigSchema == "" {
		if _, ok := files["config.schema.json"]; ok {
			manifest.ConfigSchema = "config.schema.json"
		}
	}
	pkg.Manifest = manifest

	if manifest.SchemaVersion != SchemaVersion && manifest.SchemaVersion != AppSchemaVersion {
		result.addError(fmt.Sprintf("schema_version must be %q or %q", SchemaVersion, AppSchemaVersion))
	}
	if ScopeRootSelector(manifest.Target) == "" {
		result.addError("target must be personal-space, homepage or web")
	}
	validateAppLayout(manifest, files, result)
	if !packNamePattern.MatchString(manifest.Name) {
		result.addError("name must use lowercase letters, numbers and hyphens")
	}
	if !packVersionPattern.MatchString(manifest.Version) {
		result.addError("version must be a semantic version like 0.1.0")
	}

	htmlSeen := map[string]struct{}{}
	validateHTMLFile("entry", manifest.Entry, files, result, htmlSeen)
	for i, template := range manifest.Templates {
		prefix := fmt.Sprintf("templates[%d]", i)
		if !packNamePattern.MatchString(template.Name) {
			result.addError(prefix + ".name must use lowercase letters, numbers and hyphens")
		}
		validateHTMLFile(prefix+".path", template.Path, files, result, htmlSeen)
	}
	for filePath := range files {
		if strings.ToLower(path.Ext(filePath)) == ".html" {
			if _, ok := htmlSeen[filePath]; !ok {
				validateHTMLFile("html "+filePath, filePath, files, result, htmlSeen)
			}
		}
	}

	cssSeen := map[string]struct{}{}
	for _, cssPath := range manifest.Styles {
		validateCSSFile("style", cssPath, manifest.Target, files, result, cssSeen)
	}
	for filePath := range files {
		if strings.ToLower(path.Ext(filePath)) == ".css" {
			if _, ok := cssSeen[filePath]; !ok {
				validateCSSFile("css "+filePath, filePath, manifest.Target, files, result, cssSeen)
			}
		}
	}
	if manifest.PreviewImage != "" {
		validateImagePath("preview_image", manifest.PreviewImage, files, result)
	}
	if manifest.ConfigSchema != "" {
		validateConfigSchemaPath("config_schema", manifest.ConfigSchema, files, result)
	}
	for i, asset := range manifest.Assets {
		prefix := fmt.Sprintf("assets[%d]", i)
		if !packNamePattern.MatchString(asset.Name) {
			result.addError(prefix + ".name must use lowercase letters, numbers and hyphens")
		}
		validateImagePath(prefix+".path", asset.Path, files, result)
		if asset.Type != "" && !allowedAssetType(asset.Type) {
			result.addError(prefix + ".type is not supported")
		}
	}
	validateEffect(manifest.Target, manifest.Effect, files, result)
	validateCapabilities(manifest, result)
	for name := range files {
		if err := validateFilePath(name); err != nil {
			result.addError("file path: " + err.Error())
		}
		if !allowedFileExtension(name) {
			result.addError("file extension is not allowed: " + name)
		}
	}
	if len(manifest.CompatibleCampusOS) == 0 {
		result.addWarning("compatible_campusos is empty")
	}
	addRecommendedStructureWarnings(manifest, files, result)
}

func validateAppLayout(manifest Manifest, files map[string][]byte, result *ValidationResult) {
	if manifest.SchemaVersion != AppSchemaVersion {
		if manifest.Layout != nil || len(manifest.SurfaceOverrides) > 0 {
			result.addError("layout and surface_overrides require campusos.app-style-pack.v2")
		}
		return
	}
	if manifest.Target != TargetWeb {
		result.addError("campusos.app-style-pack.v2 currently requires target web")
	}
	if manifest.Layout == nil {
		result.addError("campusos.app-style-pack.v2 requires layout")
		return
	}
	layout := manifest.Layout
	if layout.Mode != "full" && layout.Mode != "contained" {
		result.addError("layout.mode must be full or contained")
	}
	if layout.HeaderMode != "sticky" && layout.HeaderMode != "static" && layout.HeaderMode != "overlay" {
		result.addError("layout.header_mode must be sticky, static or overlay")
	}
	if layout.ScrollMode != "page" && layout.ScrollMode != "content" {
		result.addError("layout.scroll_mode must be page or content")
	}
	if layout.AnimationPreset != "" && layout.AnimationPreset != "none" && layout.AnimationPreset != "fade" && layout.AnimationPreset != "reveal" && layout.AnimationPreset != "parallax" {
		result.addError("layout.animation_preset is not supported")
	}
	if layout.BackgroundAsset != "" {
		validateImagePath("layout.background_asset", layout.BackgroundAsset, files, result)
		declared := false
		for _, asset := range manifest.Assets {
			if asset.Path == layout.BackgroundAsset {
				declared = true
				break
			}
		}
		if !declared {
			result.addError("layout.background_asset must be declared in assets")
		}
	}
	seen := map[string]bool{}
	for _, override := range manifest.SurfaceOverrides {
		if !strings.Contains(override.SurfaceID, ".") || seen[override.SurfaceID] {
			result.addError("surface_overrides require unique namespaced surface_id")
		}
		seen[override.SurfaceID] = true
		if override.Region == "page-outlet" || override.Region == "safety" {
			result.addError("surface override cannot replace required host regions")
		}
	}
}

func validateHTMLFile(field, value string, files map[string][]byte, result *ValidationResult, seen map[string]struct{}) {
	if err := validateFilePath(value); err != nil {
		result.addError(field + ": " + err.Error())
		return
	}
	if strings.ToLower(path.Ext(value)) != ".html" {
		result.addError(field + " must point to an html file")
		return
	}
	data, ok := files[value]
	if !ok {
		result.addError(field + " file is missing: " + value)
		return
	}
	seen[value] = struct{}{}
	htmlResult := safehtml.Validate(string(data))
	result.addSafeHTMLPrefixed(field+": ", htmlResult)
}

func validateCSSFile(field, value, target string, files map[string][]byte, result *ValidationResult, seen map[string]struct{}) {
	if err := validateFilePath(value); err != nil {
		result.addError(field + ": " + err.Error())
		return
	}
	if strings.ToLower(path.Ext(value)) != ".css" {
		result.addError(field + " must point to a css file: " + value)
		return
	}
	data, ok := files[value]
	if !ok {
		result.addError(field + " file is missing: " + value)
		return
	}
	seen[value] = struct{}{}
	cssResult := ValidateCSS(string(data))
	result.addPrefixed("css "+value+": ", cssResult)
	if cssResult.Valid {
		result.addPrefixed("css "+value+": ", ValidateCSSScope(target, string(data)))
	}
}

func validateImagePath(field, value string, files map[string][]byte, result *ValidationResult) {
	if err := validateFilePath(value); err != nil {
		result.addError(field + ": " + err.Error())
		return
	}
	if !allowedImageExtension(value) {
		result.addError(field + " must point to png, jpg, jpeg or webp")
		return
	}
	if _, ok := files[value]; !ok {
		result.addError(field + " file is missing: " + value)
	}
}

func validateConfigSchemaPath(field, value string, files map[string][]byte, result *ValidationResult) {
	if err := validateFilePath(value); err != nil {
		result.addError(field + ": " + err.Error())
		return
	}
	if value != "config.schema.json" {
		result.addError(field + " must point to config.schema.json")
		return
	}
	data, ok := files[value]
	if !ok {
		result.addError(field + " file is missing: " + value)
		return
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		result.addError(field + " cannot be parsed as json")
		return
	}
	if schemaType, ok := schema["type"].(string); ok && schemaType != "object" {
		result.addError(field + ` type must be "object"`)
	}
}

func validateEffect(target string, effect *Effect, files map[string][]byte, result *ValidationResult) {
	declared := map[string]struct{}{}
	if effect != nil {
		if target == TargetHomepage {
			result.addError("target homepage does not support scripts; use target web for administrator-provided sandbox effects")
		}
		if effect.Runtime != EffectRuntimeSandboxWorker {
			result.addError(fmt.Sprintf("effect.runtime must be %q", EffectRuntimeSandboxWorker))
		}
		if err := validateFilePath(effect.Entry); err != nil {
			result.addError("effect.entry: " + err.Error())
		} else if strings.ToLower(path.Ext(effect.Entry)) != ".js" {
			result.addError("effect.entry must point to a js file")
		} else if script, ok := files[effect.Entry]; !ok {
			result.addError("effect.entry file is missing: " + effect.Entry)
		} else {
			declared[effect.Entry] = struct{}{}
			result.addPrefixed("effect "+effect.Entry+": ", ValidateEffectScript(string(script)))
		}
		if effect.Source != "" {
			if err := validateFilePath(effect.Source); err != nil {
				result.addError("effect.source: " + err.Error())
			} else if strings.ToLower(path.Ext(effect.Source)) != ".ts" {
				result.addError("effect.source must point to a ts file")
			} else if _, ok := files[effect.Source]; !ok {
				result.addError("effect.source file is missing: " + effect.Source)
			} else {
				declared[effect.Source] = struct{}{}
			}
		}
	}
	for name := range files {
		ext := strings.ToLower(path.Ext(name))
		if ext != ".js" && ext != ".ts" {
			continue
		}
		if _, ok := declared[name]; !ok {
			result.addError("effect script file is not declared in style.yaml: " + name)
		}
	}
}

func validateCapabilities(manifest Manifest, result *ValidationResult) {
	allowed := map[string]map[string]struct{}{
		TargetPersonalSpace: {
			"space.profile.read": {},
			"space.posts.read":   {},
			"schedule.me.read":   {},
		},
		TargetHomepage: {},
		TargetWeb: {
			"community.threads.read": {},
			"categories.read":        {},
			"schedule.me.read":       {},
		},
	}
	for _, capability := range manifest.Capabilities {
		if _, ok := allowed[manifest.Target][capability]; !ok {
			result.addError(fmt.Sprintf("capability %q is not allowed for target %q", capability, manifest.Target))
		}
	}
	if len(manifest.Capabilities) > 0 && manifest.Effect == nil {
		result.addWarning("capabilities are declared but no sandbox effect uses the CampusStyleSDK bridge")
	}
}

func addRecommendedStructureWarnings(manifest Manifest, files map[string][]byte, result *ValidationResult) {
	recommended := map[string]string{
		"README.md":           "README.md is recommended",
		"templates/card.html": "templates/card.html is recommended for reusable card fragments",
		"preview.png":         "preview.png is recommended for style pack previews",
		"config.schema.json":  "config.schema.json is recommended for configurable style packs",
	}
	for name, message := range recommended {
		if _, ok := files[name]; !ok {
			result.addWarning(message)
		}
	}
	if len(manifest.Templates) == 0 {
		result.addWarning("style.yaml templates list is recommended")
	}
	hasAssetFiles := false
	for name := range files {
		if strings.HasPrefix(name, "assets/") {
			hasAssetFiles = true
			break
		}
	}
	if hasAssetFiles && len(manifest.Assets) == 0 {
		result.addWarning("style.yaml assets list is recommended when assets/ files are present")
	}
}

func validateFilePath(value string) error {
	raw := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if strings.HasPrefix(raw, "/") || path.IsAbs(raw) {
		return fmt.Errorf("%q must be a safe relative path", value)
	}
	clean := cleanPackPath(raw)
	if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return fmt.Errorf("%q must be a safe relative path", value)
	}
	return nil
}

func cleanPackPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return ""
	}
	return path.Clean(strings.TrimPrefix(value, "/"))
}

func stripArchiveRoot(raw map[string][]byte) map[string][]byte {
	if _, ok := raw["style.yaml"]; ok {
		return raw
	}
	if _, ok := raw["style.yml"]; ok {
		return raw
	}
	var roots []string
	for name := range raw {
		if strings.HasSuffix(name, "/style.yaml") || strings.HasSuffix(name, "/style.yml") {
			roots = append(roots, strings.TrimSuffix(strings.TrimSuffix(name, "style.yaml"), "style.yml"))
		}
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		return raw
	}
	root := roots[0]
	stripped := map[string][]byte{}
	for name, data := range raw {
		if strings.HasPrefix(name, root) {
			stripped[strings.TrimPrefix(name, root)] = data
		}
	}
	return stripped
}

func normalizePaths(values []string, limit int) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = cleanPackPath(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
		if len(normalized) >= limit {
			break
		}
	}
	return normalized
}

func normalizeTemplates(values []Template, limit int) []Template {
	seen := map[string]struct{}{}
	normalized := make([]Template, 0, len(values))
	for _, value := range values {
		value.Path = cleanPackPath(value.Path)
		value.Name = normalizePackName(value.Name)
		if value.Path == "" {
			continue
		}
		if value.Name == "" {
			base := strings.TrimSuffix(path.Base(value.Path), path.Ext(value.Path))
			value.Name = normalizePackName(base)
		}
		key := value.Name + "\x00" + value.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, value)
		if len(normalized) >= limit {
			break
		}
	}
	return normalized
}

func normalizeList(values []string, limit int) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
		if len(normalized) >= limit {
			break
		}
	}
	return normalized
}

func normalizeTokens(tokens map[string]string) map[string]string {
	if len(tokens) == 0 {
		return nil
	}
	normalized := map[string]string{}
	for key, value := range tokens {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			normalized[key] = value
		}
	}
	return normalized
}

func normalizePackName(value string) string {
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
	return strings.Trim(builder.String(), "-")
}

func allowedFileExtension(name string) bool {
	if name == "config.schema.json" {
		return true
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".yaml", ".yml", ".md", ".html", ".css", ".js", ".ts", ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

func allowedImageExtension(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp":
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

func fileType(name string) string {
	if name == "config.schema.json" {
		return "application/schema+json"
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "text/javascript"
	case ".ts":
		return "text/typescript"
	case ".md":
		return "text/markdown"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func (r *ValidationResult) finish() ValidationResult {
	r.Valid = len(r.Errors) == 0
	if r.Errors == nil {
		r.Errors = []string{}
	}
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	return *r
}

func (r *ValidationResult) addError(message string) {
	r.Errors = append(r.Errors, message)
}

func (r *ValidationResult) addWarning(message string) {
	r.Warnings = append(r.Warnings, message)
}

func (r *ValidationResult) addSafeHTMLPrefixed(prefix string, result safehtml.ValidationResult) {
	for _, message := range result.Errors {
		r.addError(prefix + message)
	}
	for _, message := range result.Warnings {
		r.addWarning(prefix + message)
	}
}

func (r *ValidationResult) addPrefixed(prefix string, result ValidationResult) {
	for _, message := range result.Errors {
		r.addError(prefix + message)
	}
	for _, message := range result.Warnings {
		r.addWarning(prefix + message)
	}
}

func escapeHTML(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")
	return replacer.Replace(value)
}
