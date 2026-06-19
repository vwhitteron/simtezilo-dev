package ui //nolint:testpackage // white-box: asserts unexported MenuNode tree internals

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"
)

func dumpMenuNode(n *MenuNode, depth int, b *strings.Builder) {
	fmt.Fprintf(b, "%s%s [type=%d kind=%d ctx=%s]\n",
		strings.Repeat("  ", depth), n.name, n.nodeType, n.kind, n.context)

	for _, c := range n.children {
		dumpMenuNode(c, depth+1, b)
	}
}

// TestMenuTreeMatchesGolden asserts the declaratively-built tree is byte-for-byte
// identical to the recorded structure (names, types, kinds, contexts, order).
// The golden was captured from the previous imperative construction.
func TestMenuTreeMatchesGolden(t *testing.T) {
	t.Parallel()

	var builder strings.Builder

	dumpMenuNode(NewMenuSystem().root, 0, &builder)

	want, err := os.ReadFile("testdata/menu_tree.golden")
	if err != nil {
		t.Fatal(err)
	}

	if builder.String() != string(want) {
		t.Errorf("menu tree differs from golden.\n--- got ---\n%s", builder.String())
	}
}

// TestMenuParentLinks asserts every child points back to its parent.
func TestMenuParentLinks(t *testing.T) {
	t.Parallel()

	var check func(n *MenuNode)

	check = func(n *MenuNode) {
		for _, c := range n.children {
			if c.parent != n {
				t.Errorf("node %q parent mismatch: got %v want %q", c.name, c.parent, n.name)
			}

			check(c)
		}
	}

	check(NewMenuSystem().root)
}

// TestMenuStartsOnLiveView asserts the initial cursor is the live view leaf.
func TestMenuStartsOnLiveView(t *testing.T) {
	t.Parallel()

	if got := NewMenuSystem().currentNode.name; got != languagedb.UIMenuLivePred {
		t.Fatalf("currentNode = %q, want %q", got, languagedb.UIMenuLivePred)
	}
}
