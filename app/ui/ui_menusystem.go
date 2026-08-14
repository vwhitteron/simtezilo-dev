package ui

import "github.com/vwhitteron/simtezilo-dev/app/i18n/languagedb"

const (
	setupModeCountdownStart = 5
	menuNodeRoot            = languagedb.Key("root")
)

type MenuSystem struct {
	root                *MenuNode
	currentNode         *MenuNode
	setupModeCountdown  int
	devToolsEnabled     func() bool
	experimentalEnabled func() bool
	bluetoothAvailable  func() bool
}

// NewMenuSystem builds the menu tree (declared in newMenuTree) and starts the
// cursor on the live view leaf. hapticsChannels is the number of haptic output
// channels; it determines how many routing toggle leaves are created.
func NewMenuSystem(hapticsChannels int) *MenuSystem {
	root := newMenuTree(hapticsChannels)

	return &MenuSystem{
		root:               root,
		currentNode:        root.find(languagedb.UIMenuLivePred), // Start on the first live view leaf
		setupModeCountdown: setupModeCountdownStart,
	}
}

// NavigateLeft moves to the previous sibling or is used for value adjustment on leaves.
func (m *MenuSystem) NavigateLeft() (*MenuNode, string) {
	// All nodes navigate left to previous sibling
	return m.previousSibling(), "navigate"
}

// NavigateRight moves to the next sibling.
func (m *MenuSystem) NavigateRight() (*MenuNode, string) {
	// All nodes navigate right to next sibling
	return m.nextSibling(), "navigate"
}

// NavigateEnter activates whichever of the enter or exit actions the current page
// offers: a branch is entered, the Return item exits to the parent menu, and a
// live view is exited. Regular leaves have no enter/exit action, so their value is
// left alone — those are adjusted with up/down.
func (m *MenuSystem) NavigateEnter() (*MenuNode, string) {
	if m.currentNode.name == languagedb.UIMenuReturn {
		return m.exitToParent()
	}

	if m.currentNode.nodeType == NodeTypeBranch {
		return m.enterBranch()
	}

	if m.currentNode.kind == KindLive {
		return m.exitToParent()
	}

	return m.currentNode, actionNone
}

// NavigateDown enters a branch node or toggles adjust mode on leaf nodes.
func (m *MenuSystem) NavigateDown() (*MenuNode, string) {
	// The Return item is an exit page: leaving is Up only, so Down does nothing.
	if m.currentNode.name == languagedb.UIMenuReturn {
		return m.currentNode, actionNone
	}

	if node, action := m.enterBranch(); action == actionEnter {
		return node, action
	}

	// For leaves, decrease value. Live leaves return to their parent; info leaves
	// are read-only and exit via the Return item, so up/down does nothing.
	if m.currentNode.nodeType == NodeTypeLeaf {
		if m.currentNode.parent != nil && m.currentNode.kind == KindLive {
			m.currentNode = m.currentNode.parent

			return m.currentNode, actionExit
		}

		if m.currentNode.kind == KindInfo {
			return m.currentNode, actionNone
		}

		return m.currentNode, actionDecrease
	}

	return m.currentNode, actionNone
}

// NavigateUp exits to parent on return node or increases value on regular leaves.
func (m *MenuSystem) NavigateUp() (*MenuNode, string) {
	// Special case: on "return" node, navigate up to parent
	if m.currentNode.name == languagedb.UIMenuReturn {
		if node, action := m.exitToParent(); action == actionExit {
			return node, action
		}
	}

	// For leaves, increase value. Live leaves return to their parent; info leaves
	// are read-only and exit via the Return item, so up/down does nothing.
	if m.currentNode.nodeType == NodeTypeLeaf {
		if m.currentNode.parent != nil && m.currentNode.kind == KindLive {
			m.currentNode = m.currentNode.parent

			return m.currentNode, actionExit
		}

		if m.currentNode.kind == KindInfo {
			return m.currentNode, actionNone
		}

		return m.currentNode, actionIncrease
	}

	// For branches, do nothing
	return m.currentNode, actionNone
}

// GetCurrentNode returns the current menu node.
func (m *MenuSystem) GetCurrentNode() *MenuNode {
	return m.currentNode
}

// GetCurrentMenuPage returns the name of the current menu node (for compatibility).
func (m *MenuSystem) GetCurrentMenuPage() languagedb.Key {
	return m.currentNode.name
}

// GetBreadcrumb returns the navigation path from root to current node.
func (m *MenuSystem) GetBreadcrumb() []string {
	path := make([]string, 0)
	node := m.currentNode

	for node != nil && node.name != menuNodeRoot {
		path = append([]string{string(node.name)}, path...)
		node = node.parent
	}

	return path
}

// IsCurrentNodeLeaf returns true if the current node is a leaf.
func (m *MenuSystem) IsCurrentNodeLeaf() bool {
	return m.currentNode.nodeType == NodeTypeLeaf
}

// IsCurrentNodeBranch returns true if the current node is a branch.
func (m *MenuSystem) IsCurrentNodeBranch() bool {
	return m.currentNode.nodeType == NodeTypeBranch
}

// IsCurrentNodeInfo reports whether the current node is a read-only info leaf.
func (m *MenuSystem) IsCurrentNodeInfo() bool {
	return m.currentNode.nodeType == NodeTypeLeaf && m.currentNode.kind == KindInfo
}

// IsCurrentNodeLive reports whether the current node is a live-view leaf.
func (m *MenuSystem) IsCurrentNodeLive() bool {
	return m.currentNode.nodeType == NodeTypeLeaf && m.currentNode.kind == KindLive
}

// NextMenuPage navigates right (for compatibility with existing code).
func (m *MenuSystem) NextMenuPage() languagedb.Key {
	node, _ := m.NavigateRight()

	return node.name
}

// PreviousMenuPage navigates left (for compatibility with existing code).
func (m *MenuSystem) PreviousMenuPage() languagedb.Key {
	node, _ := m.NavigateLeft()

	return node.name
}

// GetSetupModeCountdown returns the current setup countdown value.
func (m *MenuSystem) GetSetupModeCountdown() int {
	return m.setupModeCountdown
}

// ResetSetupModeCountdown resets the setup countdown to 5.
func (m *MenuSystem) ResetSetupModeCountdown() int {
	m.setupModeCountdown = setupModeCountdownStart

	return m.setupModeCountdown
}

// DecrementSetupModeCountdown decrements the setup countdown by 1.
func (m *MenuSystem) DecrementSetupModeCountdown() int {
	if m.setupModeCountdown > 0 {
		m.setupModeCountdown--
	}

	return m.setupModeCountdown
}

// IsSetupModeCountdownZero returns true if the setup countdown is zero.
func (m *MenuSystem) IsSetupModeCountdownZero() bool {
	return m.setupModeCountdown == 0
}

// SetDevToolsEnabledCallback sets the callback function to check if dev tools are enabled.
func (m *MenuSystem) SetDevToolsEnabledCallback(callback func() bool) {
	m.devToolsEnabled = callback
}

// SetExperimentalEnabledCallback sets the callback function to check if experimental features are enabled.
func (m *MenuSystem) SetExperimentalEnabledCallback(callback func() bool) {
	m.experimentalEnabled = callback
}

// SetBluetoothAvailableCallback sets the callback used to decide whether the
// Bluetooth menu branch should be shown.
func (m *MenuSystem) SetBluetoothAvailableCallback(callback func() bool) {
	m.bluetoothAvailable = callback
}

// enterBranch moves into the current branch's first visible child. It reports
// actionNone, leaving the current node untouched, when the node is not a branch
// or has nothing visible to enter.
func (m *MenuSystem) enterBranch() (*MenuNode, string) {
	if m.currentNode.nodeType != NodeTypeBranch {
		return m.currentNode, actionNone
	}

	firstVisible := m.getFirstVisibleChild(m.currentNode)
	if firstVisible == nil {
		return m.currentNode, actionNone
	}

	m.currentNode = firstVisible

	return m.currentNode, actionEnter
}

// exitToParent moves out to the parent menu. It reports actionNone, leaving the
// current node untouched, when there is no parent menu to exit to.
func (m *MenuSystem) exitToParent() (*MenuNode, string) {
	if m.currentNode.parent == nil || m.currentNode.parent.name == menuNodeRoot {
		return m.currentNode, actionNone
	}

	m.currentNode = m.currentNode.parent

	return m.currentNode, actionExit
}

// previousSibling navigates to the previous visible sibling with wrapping.
func (m *MenuSystem) previousSibling() *MenuNode {
	if m.currentNode.parent == nil {
		return m.currentNode
	}

	siblings := m.getVisibleChildren(m.currentNode.parent)
	if len(siblings) <= 1 {
		return m.currentNode
	}

	// Find current index
	currentIndex := -1

	for idx, sibling := range siblings {
		if sibling == m.currentNode {
			currentIndex = idx

			break
		}
	}

	if currentIndex == -1 {
		return m.currentNode
	}

	// Move to previous with wrapping
	prevIndex := (currentIndex - 1 + len(siblings)) % len(siblings)
	m.currentNode = siblings[prevIndex]

	return m.currentNode
}

// nextSibling navigates to the next visible sibling with wrapping.
func (m *MenuSystem) nextSibling() *MenuNode {
	if m.currentNode.parent == nil {
		return m.currentNode
	}

	siblings := m.getVisibleChildren(m.currentNode.parent)
	if len(siblings) <= 1 {
		return m.currentNode
	}

	// Find current index
	currentIndex := -1

	for idx, sibling := range siblings {
		if sibling == m.currentNode {
			currentIndex = idx

			break
		}
	}

	if currentIndex == -1 {
		return m.currentNode
	}

	// Move to next with wrapping
	nextIndex := (currentIndex + 1) % len(siblings)
	m.currentNode = siblings[nextIndex]

	return m.currentNode
}

// getVisibleChildren returns only the children that should be visible based on context.
func (m *MenuSystem) getVisibleChildren(node *MenuNode) []*MenuNode {
	visible := make([]*MenuNode, 0)

	for _, child := range node.children {
		if m.isNodeVisible(child) {
			visible = append(visible, child)
		}
	}

	return visible
}

// getFirstVisibleChild returns the first visible child of a node.
func (m *MenuSystem) getFirstVisibleChild(node *MenuNode) *MenuNode {
	for _, child := range node.children {
		if m.isNodeVisible(child) {
			return child
		}
	}

	return nil
}

// isNodeVisible checks if a node should be visible based on its context.
func (m *MenuSystem) isNodeVisible(node *MenuNode) bool {
	switch node.context {
	case PageContextAlways:
		return true
	case PageContextDevTools:
		return m.isDevToolsEnabled()
	case PageContextExperimental:
		return m.isExperimentalEnabled()
	case PageContextBluetooth:
		return m.isBluetoothAvailable()
	default:
		return true
	}
}

// isDevToolsEnabled returns true if dev tools are enabled.
func (m *MenuSystem) isDevToolsEnabled() bool {
	if m.devToolsEnabled == nil {
		return false
	}

	return m.devToolsEnabled()
}

// isExperimentalEnabled returns true if experimental features are enabled.
func (m *MenuSystem) isExperimentalEnabled() bool {
	if m.experimentalEnabled == nil {
		return false
	}

	return m.experimentalEnabled()
}

// isBluetoothAvailable returns true if Bluetooth management is available.
func (m *MenuSystem) isBluetoothAvailable() bool {
	if m.bluetoothAvailable == nil {
		return false
	}

	return m.bluetoothAvailable()
}
