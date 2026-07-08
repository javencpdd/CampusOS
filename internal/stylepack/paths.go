package stylepack

import (
	"os"
	"path/filepath"
	"strings"
)

const DefaultPluginDataDir = "data/plugin_data"

func PluginDataDir() string {
	root := strings.TrimSpace(os.Getenv("PLUGIN_DATA_DIR"))
	if root == "" {
		return DefaultPluginDataDir
	}
	return root
}

func SourceDir(pluginName, packName string) string {
	return filepath.Join(PluginDataDir(), pluginName, "style-packs", packName)
}
