package languagedb

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"
)

// enumKeys extracts the string value of every `<Name> Key = "<value>"` constant
// declared in translation_keys.go, by parsing the source rather than requiring a
// hand-maintained slice (which would be the very thing that drifts).
func enumKeys(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "translation_keys.go", nil, 0)
	if err != nil {
		t.Fatalf("parse translation_keys.go: %v", err)
	}

	keys := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		ident, ok := spec.Type.(*ast.Ident)
		if !ok || ident.Name != "Key" {
			return true
		}
		for _, v := range spec.Values {
			lit, ok := v.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquote %s: %v", lit.Value, err)
			}
			keys[s] = true
		}
		return true
	})

	if len(keys) == 0 {
		t.Fatal("no Key constants found in translation_keys.go")
	}
	return keys
}

// jsonKeys reads the translation keys for one embedded language file.
func jsonKeys(t *testing.T, name string) map[string]bool {
	t.Helper()

	data, err := languageFiles.ReadFile("lang/" + name)
	if err != nil {
		t.Fatalf("read embedded lang/%s: %v", name, err)
	}
	var doc struct {
		Translations map[string]string `json:"translations"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal lang/%s: %v", name, err)
	}
	if len(doc.Translations) == 0 {
		t.Fatalf("lang/%s has no translations", name)
	}
	keys := make(map[string]bool, len(doc.Translations))
	for k := range doc.Translations {
		keys[k] = true
	}
	return keys
}

// diff returns the keys in a that are not in b.
func diff(a, b map[string]bool) []string {
	var only []string
	for k := range a {
		if !b[k] {
			only = append(only, k)
		}
	}
	sort.Strings(only)
	return only
}

// TestTranslationKeysInSync asserts that the Go Key enum, en.json and ja.json
// all declare exactly the same set of translation keys. Any key added to one
// source but not the others fails the test, pinpointing the missing entries.
func TestTranslationKeysInSync(t *testing.T) {
	enum := enumKeys(t)
	en := jsonKeys(t, "en.json")
	ja := jsonKeys(t, "ja.json")

	sets := []struct {
		name string
		keys map[string]bool
	}{
		{"Key enum (translation_keys.go)", enum},
		{"en.json", en},
		{"ja.json", ja},
	}

	for i := range sets {
		for j := range sets {
			if i == j {
				continue
			}
			if missing := diff(sets[i].keys, sets[j].keys); len(missing) > 0 {
				t.Errorf("%d key(s) in %s but missing from %s:\n\t%v",
					len(missing), sets[i].name, sets[j].name, missing)
			}
		}
	}
}
