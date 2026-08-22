package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*s = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

type pluginManifest struct {
	Name    string `json:"name"`
	Exports struct {
		Raw               stringList        `json:"raw"`
		JavaScript        stringList        `json:"javascript"`
		KnockoutTemplates map[string]string `json:"knockoutTemplates"`
		CSS               stringList        `json:"css"`
	} `json:"exports"`
}

func compileBuiltins(assetFS fs.FS, rootPath string) (string, error) {
	entries, err := fs.ReadDir(assetFS, "components")
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := fs.Stat(assetFS, path.Join("components", entry.Name(), "ungit-plugin.json")); err == nil {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	var out strings.Builder
	for _, dir := range names {
		manifestPath := path.Join("components", dir, "ungit-plugin.json")
		data, err := fs.ReadFile(assetFS, manifestPath)
		if err != nil {
			return "", err
		}
		var manifest pluginManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return "", fmt.Errorf("parse %s: %w", manifestPath, err)
		}
		name := manifest.Name
		if name == "" {
			name = dir
		}
		fmt.Fprintf(&out, "<!-- Component: %s -->\n", name)

		for _, raw := range manifest.Exports.Raw {
			text, err := fs.ReadFile(assetFS, path.Join("components", dir, raw))
			if err != nil {
				return "", err
			}
			out.Write(text)
			out.WriteString("\n")
		}

		for _, js := range manifest.Exports.JavaScript {
			fmt.Fprintf(&out, `<script type="text/javascript" src="%s/plugins/%s/%s"></script>`+"\n", rootPath, name, js)
		}

		templateNames := make([]string, 0, len(manifest.Exports.KnockoutTemplates))
		for templateName := range manifest.Exports.KnockoutTemplates {
			templateNames = append(templateNames, templateName)
		}
		sort.Strings(templateNames)
		for _, templateName := range templateNames {
			filename := manifest.Exports.KnockoutTemplates[templateName]
			text, err := fs.ReadFile(assetFS, path.Join("components", dir, filename))
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&out, "<script type=\"text/html\" id=\"%s\">\n%s\n</script>\n", templateName, text)
		}

		for _, css := range manifest.Exports.CSS {
			lightCSS := strings.TrimSuffix(css, ".css") + "-light.css"
			_, lightErr := fs.Stat(assetFS, path.Join("components", dir, lightCSS))
			if lightErr != nil || lightCSS == css {
				fmt.Fprintf(&out, `<link rel="stylesheet" type="text/css" href="%s/plugins/%s/%s" />`+"\n", rootPath, name, css)
				continue
			}
			fmt.Fprintf(&out, `<link rel="stylesheet" type="text/css" href="%s/plugins/%s/%s" data-ungit-theme="dark" media="not all" />`+"\n", rootPath, name, css)
			fmt.Fprintf(&out, `<link rel="stylesheet" type="text/css" href="%s/plugins/%s/%s" data-ungit-theme="light" media="not all" />`+"\n", rootPath, name, lightCSS)
		}
	}
	return out.String(), nil
}
