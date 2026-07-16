package space

import (
	"fmt"
	"strings"

	"github.com/campusos/CampusOS/internal/modules/features/appearance/stylepack"
)

type StylePackResult struct {
	Validation stylepack.ValidationResult `json:"validation"`
	Package    *stylepack.Package         `json:"package,omitempty"`
}

type StylePackApplySourceRequest struct {
	Name string `json:"name" binding:"required"`
}

func BuildStylePackPackage(owner Owner, space *Space, pack *stylepack.Package) StylePackage {
	manifest := baseStyleHTMLManifest(owner, space)
	manifest.Name = pack.Manifest.Name
	manifest.Version = pack.Manifest.Version
	manifest.Author = strings.TrimSpace(pack.Manifest.Author)
	if manifest.Author == "" {
		manifest.Author = exportAuthor(owner)
	}
	manifest.Description = strings.TrimSpace(pack.Manifest.Description)
	if manifest.Description == "" {
		manifest.Description = "CampusOS page style pack."
	}
	if len(pack.Manifest.CompatibleCampusOS) > 0 {
		manifest.CompatibleCampusOS = append([]string(nil), pack.Manifest.CompatibleCampusOS...)
	}
	if len(pack.Manifest.Tokens) > 0 {
		manifest.Tokens = copyStringMap(pack.Manifest.Tokens)
	}
	manifest.CustomHTMLEnabled = true
	manifest.CustomHTML = pack.HTML
	manifest.CustomCSS = pack.CSS
	manifest.CustomEffectJS = pack.EffectJS
	manifest.Capabilities = append([]string(nil), pack.Manifest.Capabilities...)
	manifest.SourceStylePack = &StylePackRef{
		Name:    pack.Manifest.Name,
		Version: pack.Manifest.Version,
		Target:  pack.Manifest.Target,
	}
	return StylePackage{Manifest: NormalizeStyleManifest(manifest)}
}

func BuildStylePackExample(owner Owner, space *Space) stylepack.FileBundle {
	title := "个人主页"
	subtitle := "CampusOS personal space style pack example."
	tokens := map[string]string{}
	if space != nil {
		if strings.TrimSpace(space.Title) != "" {
			title = strings.TrimSpace(space.Title)
		}
		if strings.TrimSpace(space.Bio) != "" {
			subtitle = strings.TrimSpace(space.Bio)
		}
		if space.StyleManifest != nil {
			tokens = space.StyleManifest.Tokens
		}
	}
	if title == "个人主页" && strings.TrimSpace(owner.Nickname) != "" {
		title = strings.TrimSpace(owner.Nickname) + "的个人主页"
	}
	name := slugStyleName(owner.Username + "-space-pack")
	if name == "" {
		name = "personal-space-example"
	}
	return stylepack.BuildExample(
		"personal-space",
		name,
		title,
		title,
		subtitle,
		stylePackToken(tokens, "color.primary", "#2563eb"),
		stylePackToken(tokens, "color.background", "#ffffff"),
		stylePackToken(tokens, "color.surface", "#f8fafc"),
	)
}

func loadPersonalSourceStylePack(name string) (*stylepack.Package, stylepack.ValidationResult) {
	name = strings.TrimSpace(name)
	if slugStyleName(name) != name {
		return nil, stylepack.ValidationResult{
			Valid:  false,
			Errors: []string{"source style pack name must use lowercase letters, numbers and hyphens"},
		}
	}
	return stylepack.LoadDir(stylepack.SourceDir("personal-space", name))
}

func listPersonalSourceStylePacks() (*stylepack.SourcePackList, error) {
	items, err := stylepack.ListSourcePacks("personal-space")
	if err != nil {
		return nil, err
	}
	for i := range items {
		ensureSourcePackInfoTarget(&items[i], "personal-space")
	}
	return &stylepack.SourcePackList{Items: items}, nil
}

func ensureStylePackTarget(pack *stylepack.Package, target string) stylepack.ValidationResult {
	if pack == nil {
		return stylepack.ValidationResult{Valid: false, Errors: []string{"style pack is empty"}}
	}
	if pack.Manifest.Target != target {
		return stylepack.ValidationResult{
			Valid:  false,
			Errors: []string{fmt.Sprintf("style pack target must be %s", target)},
		}
	}
	return stylepack.ValidationResult{Valid: true}
}

func ensureSourcePackInfoTarget(info *stylepack.SourcePackInfo, target string) {
	if info == nil || !info.Validation.Valid {
		return
	}
	if info.Target == target {
		return
	}
	info.Validation.Valid = false
	info.Validation.Errors = []string{fmt.Sprintf("style pack target must be %s", target)}
	if info.Validation.Warnings == nil {
		info.Validation.Warnings = []string{}
	}
}

func stylePackToken(tokens map[string]string, key, fallback string) string {
	if value := strings.TrimSpace(tokens[key]); value != "" {
		return value
	}
	return fallback
}

func cloneStylePackRef(ref *StylePackRef) *StylePackRef {
	if ref == nil {
		return nil
	}
	clone := *ref
	return &clone
}
