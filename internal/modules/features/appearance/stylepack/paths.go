package stylepack

import (
	"os"
	"path/filepath"
	"strings"
)

const DefaultResourceDir = "data/resources"

func ResourceDir() string {
	root := strings.TrimSpace(os.Getenv("RESOURCE_DIR"))
	if root == "" {
		return DefaultResourceDir
	}
	return root
}

func SourceDir(pluginName, packName string) string {
	return filepath.Join(SourceRoot(pluginName), packName)
}

func SourceRoot(pluginName string) string {
	directory := map[string]string{
		"homepage-customizer": "homepage-packs",
		"personal-space":      "space-style-packs",
		"web-theme":           "themes",
	}[pluginName]
	if directory == "" {
		directory = pluginName
	}
	return filepath.Join(ResourceDir(), directory)
}
