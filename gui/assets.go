package main

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"path"
	"strings"
)

// Site logos are loaded from the gui/logos/ folder at BUILD time. Drop image
// files there named by site (reddit.png, x.jpg, instagram.png, facebook.webp,
// tiktok.webp, youtube.webp, pinterest.webp, ...). Nothing needs to be committed
// but the files themselves — the build embeds whatever is present. Sites without
// a logo file fall back to the coloured monogram tile automatically.
//
//go:embed logos
var logoFS embed.FS

// logosJSON returns {"reddit":"data:image/png;base64,...", ...}, keyed by the
// file's base name (lowercased), for the UI to look up per site.
func logosJSON() string {
	out := map[string]string{}
	entries, err := logoFS.ReadDir("logos")
	if err != nil {
		return "{}"
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		var mime string
		switch strings.ToLower(path.Ext(name)) {
		case ".png":
			mime = "image/png"
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".webp":
			mime = "image/webp"
		case ".gif":
			mime = "image/gif"
		case ".svg":
			mime = "image/svg+xml"
		default:
			continue // skip README and anything non-image
		}
		b, err := logoFS.ReadFile("logos/" + name)
		if err != nil {
			continue
		}
		key := strings.ToLower(strings.TrimSuffix(name, path.Ext(name)))
		out[key] = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
	}
	j, _ := json.Marshal(out)
	return string(j)
}
