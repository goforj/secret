package gen

import (
	"encoding/json"
	"encoding/xml"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCanonicalExamples ensures snippets and curated API links come from executable source.
func TestCanonicalExamples(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	index, examples, err := ExampleSections(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range Examples {
		if !strings.Contains(examples, `id="`+example.ID+`"`) {
			t.Fatal("missing example anchor")
		}
		for _, symbol := range example.Symbols {
			if !strings.Contains(index, "["+symbol+"](#"+example.ID+")") {
				t.Fatal("broken API target")
			}
		}
	}
	if symbolExists(root, "ImaginaryAPI") {
		t.Fatal("stale API accepted")
	}
}

// TestSnippetFailures requires one authoritative snippet and immediate output expectations.
func TestSnippetFailures(t *testing.T) {
	root := t.TempDir()
	for _, source := range []string{"", "// snippet:start\nx\n// snippet:end", "// snippet:start\n// Output: x\n// snippet:start\n// snippet:end"} {
		os.WriteFile(filepath.Join(root, "example.go"), []byte(source), 0600)
		if _, err := Snippet(root, "example.go"); err == nil {
			t.Fatal("invalid snippet accepted")
		}
	}
	if _, err := Snippet(root, "missing.go"); err == nil {
		t.Fatal("missing example accepted")
	}
}

// TestFingerprintAndEvidence prevents stale badges and verifies runtime-only source ownership.
func TestFingerprintAndEvidence(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs/reports"), 0755)
	os.MkdirAll(filepath.Join(root, "integration"), 0755)
	os.WriteFile(filepath.Join(root, "secret.go"), []byte("package secret"), 0600)
	first, err := Fingerprint(root, false)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "integration/test.go"), []byte("package integration"), 0600)
	second, _ := Fingerprint(root, false)
	all, _ := Fingerprint(root, true)
	if first != second || all == first {
		t.Fatal("incorrect fingerprint boundaries")
	}
	evidence := Evidence{Source: first, Tests: 1, Total: 2, Covered: 2}
	data, _ := json.Marshal(evidence)
	target := filepath.Join(root, "docs/reports/unit.json")
	os.WriteFile(target, data, 0600)
	if _, err := LoadEvidence(root, "unit", false); err != nil {
		t.Fatal(err)
	}
	evidence.Covered = 1
	data, _ = json.Marshal(evidence)
	os.WriteFile(target, data, 0600)
	if _, err := LoadEvidence(root, "unit", false); err == nil {
		t.Fatal("partial unit coverage accepted")
	}
	evidence.Covered = 2
	evidence.Source = "stale"
	data, _ = json.Marshal(evidence)
	os.WriteFile(target, data, 0600)
	if _, err := LoadEvidence(root, "unit", false); err == nil {
		t.Fatal("stale evidence accepted")
	}
}

// TestPresentation keeps native text searchable and names outside decorative image links.
func TestPresentation(t *testing.T) {
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	directory := filepath.Join(root, "docs/assets/driver-icons")
	os.MkdirAll(directory, 0755)
	for _, name := range []string{"aws", "azure", "google", "vault"} {
		data, err := os.ReadFile(filepath.Join(source, "docs/assets/driver-icons", name+".svg"))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(directory, name+".svg"), data, 0600)
	}
	features, drivers, err := Presentation(root)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(features, "<strong>") != 6 || strings.Count(drivers, "<strong>") != 5 || strings.Contains(drivers, `<a href="driver/awssecret"><img`) {
		t.Fatal("missing searchable labels or linked icon spacing")
	}
	for _, asset := range []string{"feature-icon-simple.svg", "driver-icon-aws.svg"} {
		data, err := os.ReadFile(filepath.Join(root, "docs/assets/badges", asset))
		if err != nil {
			t.Fatal(err)
		}
		var value struct {
			Width  int `xml:"width,attr"`
			Height int `xml:"height,attr"`
		}
		if err := xml.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if value.Width != 22 || value.Height != 22 {
			t.Fatal("icon dimensions changed")
		}
		if strings.Contains(string(data), "<text") {
			t.Fatal("icon contains baked label")
		}
	}
}

// TestLogoTransparency protects the family artwork's transparent silhouette.
func TestLogoTransparency(t *testing.T) {
	file, err := os.Open("../assets/logo.png")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	logo, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	transparent, opaque := false, false
	for y := logo.Bounds().Min.Y; y < logo.Bounds().Max.Y; y++ {
		for x := logo.Bounds().Min.X; x < logo.Bounds().Max.X; x++ {
			_, _, _, alpha := logo.At(x, y).RGBA()
			transparent = transparent || alpha == 0
			opaque = opaque || alpha == 65535
		}
	}
	if !transparent || !opaque {
		t.Fatal("logo lost its transparent silhouette")
	}
}
