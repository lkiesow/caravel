package httpapi

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The frontend names the categories in one module, web/js/categories.js; this
// pins that list to the one the API validates against, and keeps the copies
// it replaced from growing back.
func TestCategoriesModuleMatchesTheServer(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "web", "js", "categories.js"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`export const CATEGORIES = \[([^\]]*)\]`).FindSubmatch(body)
	if m == nil {
		t.Fatal("web/js/categories.js no longer declares `export const CATEGORIES = [...]` on one line")
	}
	var got []string
	for _, q := range regexp.MustCompile(`"([^"]*)"`).FindAllSubmatch(m[1], -1) {
		got = append(got, string(q[1]))
	}
	want := slices.Sorted(maps.Keys(validCategories))
	if sorted := slices.Sorted(slices.Values(got)); !slices.Equal(sorted, want) {
		t.Errorf("categories.js lists %v, the API validates %v", got, want)
	}

	// Any of these outside categories.js is a copy that nothing keeps in step.
	copies := regexp.MustCompile(`const (CATEGORIES|CATEGORY_COLORS)\b|#71717a`)
	web := os.DirFS(filepath.Join("..", "..", "web"))
	err = fs.WalkDir(web, "js", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && p == "js/vendor" {
			return fs.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".js") || p == "js/categories.js" {
			return nil
		}
		src, err := fs.ReadFile(web, p)
		if err != nil {
			return err
		}
		if loc := copies.Find(src); loc != nil {
			t.Errorf("web/%s has its own %q; import it from categories.js", p, loc)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
