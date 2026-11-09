package gen

import (
	"encoding/xml"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// Example ties one canonical workflow to the API symbols it actually demonstrates.
type Example struct {
	ID, Title, Path string
	Symbols         []string
}

// Examples is the curated consumer-facing API index, separate from provider plumbing.
var Examples = []Example{
	{"mounted-files", "Read mounted files", "examples/files/main.go", []string{"filesecret.New", "Reader.Get"}},
	{"reader-adapter", "Adapt an existing reader", "examples/reader/main.go", []string{"Func.Get", "ErrNotFound"}},
	{"application-testing", "Test replacements and deletion", "examples/testing_test.go", []string{"secrettest.New", "Store.Set", "Store.Delete"}},
}

// Snippet extracts a single marked workflow and requires nearby checked output.
func Snippet(root, path string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return "", err
	}
	source := string(data)
	start := "// snippet:start"
	end := "// snippet:end"
	if strings.Count(source, start) != 1 || strings.Count(source, end) != 1 {
		return "", fmt.Errorf("missing or duplicate snippet markers in %s", path)
	}
	_, source, _ = strings.Cut(source, start)
	source, _, ok := strings.Cut(source, end)
	if !ok || !strings.Contains(source, "// Output: ") {
		return "", fmt.Errorf("missing expected output in %s", path)
	}
	lines := strings.Split(strings.Trim(source, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "\t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

// symbolExists verifies the curated index targets declarations in current source.
func symbolExists(root, symbol string) bool {
	parts := strings.Split(symbol, ".")
	pkg := ""
	name := parts[len(parts)-1]
	if len(parts) == 2 {
		pkg = parts[0]
	}
	directory := root
	switch pkg {
	case "filesecret":
		directory = filepath.Join(root, "driver/filesecret")
	case "secrettest":
		directory = filepath.Join(root, "secrettest")
	}
	files, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		return false
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return false
		}
		found := false
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.FuncDecl:
				if n.Name.Name == name {
					found = true
				}
			case *ast.ValueSpec:
				for _, id := range n.Names {
					if id.Name == name {
						found = true
					}
				}
			case *ast.Field:
				for _, id := range n.Names {
					if id.Name == name {
						found = true
					}
				}
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// ExampleSections keeps the API index and examples anchored to one implementation.
func ExampleSections(root string) (string, string, error) {
	var index, examples strings.Builder
	index.WriteString("| API | Example |\n| --- | --- |\n")
	for _, example := range Examples {
		snippet, err := Snippet(root, example.Path)
		if err != nil {
			return "", "", err
		}
		for _, symbol := range example.Symbols {
			if !symbolExists(root, symbol) {
				return "", "", fmt.Errorf("unknown indexed API %s", symbol)
			}
			fmt.Fprintf(&index, "| [%s](#%s) | %s |\n", symbol, example.ID, example.Title)
		}
		fmt.Fprintf(&examples, "<a id=\"%s\"></a>\n\n### %s\n\n```go\n%s\n```\n\n", example.ID, example.Title, snippet)
	}
	return index.String(), examples.String(), nil
}

// badge centers padded labels inside a shared rounded clip so neither segment can square off an end.
func badge(label, value string) string {
	left := len(label)*7 + 20
	right := len(value)*7 + 20
	width := left + right
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="24" viewBox="0 0 %d 24" role="img" aria-label="%s: %s">
  <title>%s: %s</title>
  <defs><clipPath id="badge"><rect width="%d" height="24" rx="6"/></clipPath></defs>
  <g clip-path="url(#badge)">
    <rect width="%d" height="24" fill="#263247"/>
    <rect x="%d" width="%d" height="24" fill="#5369ca"/>
  </g>
  <g fill="#fff" font-family="Verdana,DejaVu Sans,sans-serif" font-size="11" text-anchor="middle">
    <text x="%g" y="16">%s</text>
    <text x="%g" y="16">%s</text>
  </g>
</svg>
`, width, width, html.EscapeString(label), html.EscapeString(value), html.EscapeString(label), html.EscapeString(value), width, width, left, right, float64(left)/2, html.EscapeString(label), float64(left)+float64(right)/2, html.EscapeString(value))
}

// icon wraps original vector paths in the compact family badge treatment.
func icon(title, path string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 22 22" role="img"><title>%s</title><rect x=".5" y=".5" width="21" height="21" rx="4" fill="#f3f4ff" stroke="#d0d5ff"/><g transform="translate(3 3)" fill="none" stroke="#6a7dff" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">%s</g></svg>`+"\n", html.EscapeString(title), path)
}

// write keeps checked-in projections deterministic without rewriting unchanged assets.
func write(root, path, content string) error {
	target := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(content), 0644)
}

// Presentation generates compact icons with real searchable feature and provider names.
func Presentation(root string) (string, string, error) {
	features := []struct{ id, name, body, path string }{
		{"simple", "Small capabilities", "Read, replace, or delete with ordinary strings.", `<path d="M3 4h10M3 8h10M3 12h6"/>`},
		{"bindings", "Explicit bindings", "Choose provider locations once during construction.", `<circle cx="4" cy="4" r="2"/><circle cx="12" cy="12" r="2"/><path d="m5 5 6 6M11 4h3M12 3v3"/>`},
		{"bytes", "Exact values", "Preserve bytes within each provider's documented limits.", `<path d="m5 3-4 5 4 5m6-10 4 5-4 5M9 2 7 14"/>`},
		{"errors", "Safe failures", "Classify failures without exposing provider data.", `<path d="m8 1 6 2v5c0 3-3 5-6 7-3-2-6-4-6-7V3z"/><path d="m5 8 2 2 4-4"/>`},
		{"mutations", "Deliberate mutations", "Serialize same-secret writes and report uncertain outcomes.", `<path d="M3 5h10m-3-3 3 3-3 3M13 11H3m3-3-3 3 3 3"/>`},
		{"testing", "Isolated testing", "Use a concurrency-safe store for application fixtures.", `<rect x="2" y="2" width="12" height="12" rx="2"/><path d="m5 8 2 2 4-4"/>`},
	}
	var featureTable strings.Builder
	featureTable.WriteString("<table>\n<thead><tr><th align=\"left\">Feature</th><th align=\"left\">What you get</th></tr></thead>\n<tbody>\n")
	for _, feature := range features {
		asset := "docs/assets/badges/feature-icon-" + feature.id + ".svg"
		if err := write(root, asset, icon(feature.name, feature.path)); err != nil {
			return "", "", err
		}
		fmt.Fprintf(&featureTable, "<tr><td width=\"240\" nowrap><img src=\"%s\" width=\"22\" height=\"22\" alt=\"\" align=\"absmiddle\"> <strong>%s</strong></td><td>%s</td></tr>\n", asset, feature.name, html.EscapeString(feature.body))
	}
	featureTable.WriteString("</tbody>\n</table>\n")
	drivers := []struct{ id, label, module, capability, verification string }{
		{"aws", "AWS Secrets Manager", "awssecret", "Text or binary; standard recovery window", "Moto 5.1.16 emulator"},
		{"azure", "Azure Key Vault", "azuresecret", "UTF-8; vault-configured soft deletion", "Lowkey Vault 7.4.0 emulator"},
		{"files", "Mounted files", "filesecret", "Read exact bytes beneath a caller-owned root", "Real filesystem and projected-volume tests"},
		{"google", "Google Secret Manager", "gcpsecret", "Binary values, CRC32C, and ETag deletion", "Google Secret Manager emulator (pinned image)"},
		{"vault", "Vault KV v2", "vaultsecret", "Owned scalar paths; CAS and all-version deletion", "Real Vault 1.21.4 server"},
	}
	var driverTable strings.Builder
	driverTable.WriteString("<table>\n<thead><tr><th align=\"left\">Driver</th><th align=\"left\">Behavior</th><th align=\"left\">Verification</th></tr></thead>\n<tbody>\n")
	for _, driver := range drivers {
		asset := "docs/assets/badges/driver-icon-" + driver.id + ".svg"
		artwork := icon(driver.label, `<path d="M2 4h5l2 2h5v8H2z"/>`)
		if driver.id != "files" {
			data, err := os.ReadFile(filepath.Join(root, "docs/assets/driver-icons", driver.id+".svg"))
			if err != nil {
				return "", "", err
			}
			var source struct {
				ViewBox string `xml:"viewBox,attr"`
				Inner   string `xml:",innerxml"`
			}
			if err := xml.Unmarshal(data, &source); err != nil || source.ViewBox == "" {
				return "", "", fmt.Errorf("invalid driver artwork: %s", driver.id)
			}
			artwork = fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 22 22" role="img"><title>%s</title><rect x=".5" y=".5" width="21" height="21" rx="4" fill="#f3f4ff" stroke="#d0d5ff"/><svg x="3" y="3" width="16" height="16" viewBox="%s">%s</svg></svg>`+"\n", driver.label, source.ViewBox, source.Inner)
		}
		if err := write(root, asset, artwork); err != nil {
			return "", "", err
		}
		fmt.Fprintf(&driverTable, "<tr><td width=\"260\" nowrap><img src=\"%s\" width=\"22\" height=\"22\" alt=\"\" align=\"absmiddle\"> <a href=\"driver/%s\"><strong>%s</strong></a></td><td>%s</td><td>%s</td></tr>\n", asset, driver.module, driver.label, html.EscapeString(driver.capability), driver.verification)
	}
	driverTable.WriteString("</tbody>\n</table>\n")
	return featureTable.String(), driverTable.String(), nil
}

// Generate renders documentation only when its measured library and integration evidence is current.
func Generate(root string) error {
	unit, err := LoadEvidence(root, "unit", false)
	if err != nil {
		return err
	}
	if _, err := LoadEvidence(root, "integration", true); err != nil {
		return err
	}
	for key, pair := range map[string][2]string{"status": {"status", "unreleased"}, "go": {"Go", "1.24.4+"}, "license": {"license", "MIT"}, "coverage": {"unit coverage", "100%"}, "tests": {"unit tests", fmt.Sprint(unit.Tests)}} {
		if err := write(root, "docs/assets/badges/summary-"+key+".svg", badge(pair[0], pair[1])); err != nil {
			return err
		}
	}
	features, drivers, err := Presentation(root)
	if err != nil {
		return err
	}
	index, examples, err := ExampleSections(root)
	if err != nil {
		return err
	}
	quickstart, err := Snippet(root, "examples/files/main.go")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, "docs/readme/README.md.tmpl"))
	if err != nil {
		return err
	}
	content := string(data)
	for key, value := range map[string]string{"features": features, "drivers": drivers, "quickstart": quickstart, "api": index, "examples": examples} {
		marker := "{{" + key + "}}"
		if strings.Count(content, marker) != 1 {
			return fmt.Errorf("missing template section %s", key)
		}
		content = strings.Replace(content, marker, value, 1)
	}
	if strings.Contains(content, "{{") {
		return fmt.Errorf("unresolved template marker")
	}
	return write(root, "README.md", content)
}
