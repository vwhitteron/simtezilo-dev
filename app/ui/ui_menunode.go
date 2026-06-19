package ui

import "github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"

// PageContext defines when a menu page should be visible.
type PageContext string

const (
	PageContextAlways       PageContext = "always"
	PageContextDevTools     PageContext = "devtools"
	PageContextExperimental PageContext = "experimental"
	PageContextBluetooth    PageContext = "bluetooth"
)

// NodeType defines whether a menu node is a branch or leaf.
type NodeType int

const (
	NodeTypeBranch NodeType = iota
	NodeTypeLeaf
)

// NodeKind categorises a leaf for layout and navigation. It is independent of
// nodeType: an info page and a setting are both leaves but render differently
// and navigate differently. Branches ignore it.
type NodeKind int

const (
	KindSetting NodeKind = iota // editable leaf -> LayoutSetting (zero value)
	KindInfo                    // read-only info leaf -> LayoutInfo
	KindLive                    // live-view leaf
)

// MenuNode represents a node in the hierarchical menu tree.
type MenuNode struct {
	name     languagedb.Key
	nodeType NodeType
	kind     NodeKind
	context  PageContext
	children []*MenuNode
	parent   *MenuNode

	// noReturn tells build not to inject a Return child. It is set only on the
	// injected Return node itself, to stop it from recursively gaining its own.
	noReturn bool
}

// setVisibility marks a node visible only in the given page context. build
// inherits the context to the node's descendants (including the injected Return).
func (node *MenuNode) setVisibility(ctx PageContext) *MenuNode {
	node.context = ctx

	return node
}

// find returns the first node with the given name in depth-first order, or nil.
func (node *MenuNode) find(name languagedb.Key) *MenuNode {
	if node.name == name {
		return node
	}

	for _, child := range node.children {
		if found := child.find(name); found != nil {
			return found
		}
	}

	return nil
}
