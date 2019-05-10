// Package btree is designed to work specifically with
// the Tile38 collection/item.Item type.
package btree

import "github.com/tidwall/tile38/internal/collection/item"

const maxItems = 31 // use an odd number
const minItems = maxItems * 40 / 100

type node struct {
	numItems int
	items    [maxItems]*item.Item
	children [maxItems + 1]*node
}

// BTree is an ordered set of key/value pairs where the key is a string
// and the value is an unsafe.Pointer
type BTree struct {
	height int
	root   *node
	length int
}

func (n *node) find(key string) (index int, found bool) {
	i, j := 0, n.numItems
	for i < j {
		h := i + (j-i)/2
		if key >= n.items[h].ID() {
			i = h + 1
		} else {
			j = h
		}
	}
	if i > 0 && n.items[i-1].ID() >= key {
		return i - 1, true
	}
	return i, false
}

// Set or replace a value for a key
func (tr *BTree) Set(item *item.Item) (prev *item.Item, replaced bool) {
	if tr.root == nil {
		tr.root = new(node)
		tr.root.items[0] = item
		tr.root.numItems = 1
		tr.length = 1
		return
	}
	prev, replaced = tr.root.set(item, tr.height)
	if replaced {
		return
	}
	if tr.root.numItems == maxItems {
		n := tr.root
		right, median := n.split(tr.height)
		tr.root = new(node)
		tr.root.children[0] = n
		tr.root.items[0] = median
		tr.root.children[1] = right
		tr.root.numItems = 1
		tr.height++
	}
	tr.length++
	return
}

func (n *node) split(height int) (right *node, median *item.Item) {
	right = new(node)
	median = n.items[maxItems/2]
	copy(right.items[:maxItems/2], n.items[maxItems/2+1:])
	if height > 0 {
		copy(right.children[:maxItems/2+1], n.children[maxItems/2+1:])
	}
	right.numItems = maxItems / 2
	if height > 0 {
		for i := maxItems/2 + 1; i < maxItems+1; i++ {
			n.children[i] = nil
		}
	}
	for i := maxItems / 2; i < maxItems; i++ {
		n.items[i] = nil
	}
	n.numItems = maxItems / 2
	return
}

func (n *node) set(newItem *item.Item, height int) (prev *item.Item, replaced bool) {
	i, found := n.find(newItem.ID())
	if found {
		prev = n.items[i]
		n.items[i] = newItem
		return prev, true
	}
	if height == 0 {
		for j := n.numItems; j > i; j-- {
			n.items[j] = n.items[j-1]
		}
		n.items[i] = newItem
		n.numItems++
		return nil, false
	}
	prev, replaced = n.children[i].set(newItem, height-1)
	if replaced {
		return
	}
	if n.children[i].numItems == maxItems {
		right, median := n.children[i].split(height - 1)
		copy(n.children[i+1:], n.children[i:])
		copy(n.items[i+1:], n.items[i:])
		n.items[i] = median
		n.children[i+1] = right
		n.numItems++
	}
	return
}

// Scan all items in tree
func (tr *BTree) Scan(iter func(item *item.Item) bool) {
	if tr.root != nil {
		tr.root.scan(iter, tr.height)
	}
}

func (n *node) scan(iter func(item *item.Item) bool, height int) bool {
	if height == 0 {
		for i := 0; i < n.numItems; i++ {
			if !iter(n.items[i]) {
				return false
			}
		}
		return true
	}
	for i := 0; i < n.numItems; i++ {
		if !n.children[i].scan(iter, height-1) {
			return false
		}
		if !iter(n.items[i]) {
			return false
		}
	}
	return n.children[n.numItems].scan(iter, height-1)
}

// Get a value for key
func (tr *BTree) Get(key string) (item *item.Item, gotten bool) {
	if tr.root == nil {
		return
	}
	return tr.root.get(key, tr.height)
}

func (n *node) get(key string, height int) (item *item.Item, gotten bool) {
	i, found := n.find(key)
	if found {
		return n.items[i], true
	}
	if height == 0 {
		return nil, false
	}
	return n.children[i].get(key, height-1)
}

// Len returns the number of items in the tree
func (tr *BTree) Len() int {
	return tr.length
}

// Delete a value for a key
func (tr *BTree) Delete(key string) (prev *item.Item, deleted bool) {
	if tr.root == nil {
		return
	}
	var prevItem *item.Item
	prevItem, deleted = tr.root.delete(false, key, tr.height)
	if !deleted {
		return
	}
	prev = prevItem
	if tr.root.numItems == 0 {
		tr.root = tr.root.children[0]
		tr.height--
	}
	tr.length--
	if tr.length == 0 {
		tr.root = nil
		tr.height = 0
	}
	return
}

func (n *node) delete(max bool, key string, height int) (
	prev *item.Item, deleted bool,
) {
	i, found := 0, false
	if max {
		i, found = n.numItems-1, true
	} else {
		i, found = n.find(key)
	}
	if height == 0 {
		if found {
			prev = n.items[i]
			// found the items at the leaf, remove it and return.
			copy(n.items[i:], n.items[i+1:n.numItems])
			n.items[n.numItems-1] = nil
			n.children[n.numItems] = nil
			n.numItems--
			return prev, true
		}
		return nil, false
	}

	if found {
		if max {
			i++
			prev, deleted = n.children[i].delete(true, "", height-1)
		} else {
			prev = n.items[i]
			maxItem, _ := n.children[i].delete(true, "", height-1)
			n.items[i] = maxItem
			deleted = true
		}
	} else {
		prev, deleted = n.children[i].delete(max, key, height-1)
	}
	if !deleted {
		return
	}
	if n.children[i].numItems < minItems {
		if i == n.numItems {
			i--
		}
		if n.children[i].numItems+n.children[i+1].numItems+1 < maxItems {
			// merge left + *item.Item + right
			n.children[i].items[n.children[i].numItems] = n.items[i]
			copy(n.children[i].items[n.children[i].numItems+1:],
				n.children[i+1].items[:n.children[i+1].numItems])
			if height > 1 {
				copy(n.children[i].children[n.children[i].numItems+1:],
					n.children[i+1].children[:n.children[i+1].numItems+1])
			}
			n.children[i].numItems += n.children[i+1].numItems + 1
			copy(n.items[i:], n.items[i+1:n.numItems])
			copy(n.children[i+1:], n.children[i+2:n.numItems+1])
			n.items[n.numItems] = nil
			n.children[n.numItems+1] = nil
			n.numItems--
		} else if n.children[i].numItems > n.children[i+1].numItems {
			// move left -> right
			copy(n.children[i+1].items[1:],
				n.children[i+1].items[:n.children[i+1].numItems])
			if height > 1 {
				copy(n.children[i+1].children[1:],
					n.children[i+1].children[:n.children[i+1].numItems+1])
			}
			n.children[i+1].items[0] = n.items[i]
			if height > 1 {
				n.children[i+1].children[0] =
					n.children[i].children[n.children[i].numItems]
			}
			n.children[i+1].numItems++
			n.items[i] = n.children[i].items[n.children[i].numItems-1]
			n.children[i].items[n.children[i].numItems-1] = nil
			if height > 1 {
				n.children[i].children[n.children[i].numItems] = nil
			}
			n.children[i].numItems--
		} else {
			// move right -> left
			n.children[i].items[n.children[i].numItems] = n.items[i]
			if height > 1 {
				n.children[i].children[n.children[i].numItems+1] =
					n.children[i+1].children[0]
			}
			n.children[i].numItems++
			n.items[i] = n.children[i+1].items[0]
			copy(n.children[i+1].items[:],
				n.children[i+1].items[1:n.children[i+1].numItems])
			if height > 1 {
				copy(n.children[i+1].children[:],
					n.children[i+1].children[1:n.children[i+1].numItems+1])
			}
			n.children[i+1].numItems--
		}
	}
	return
}

// Ascend the tree within the range [pivot, last]
func (tr *BTree) Ascend(pivot string, iter func(item *item.Item) bool) {
	if tr.root != nil {
		tr.root.ascend(pivot, iter, tr.height)
	}
}

func (n *node) ascend(pivot string, iter func(item *item.Item) bool, height int) bool {
	i, found := n.find(pivot)
	if !found {
		if height > 0 {
			if !n.children[i].ascend(pivot, iter, height-1) {
				return false
			}
		}
	}
	for ; i < n.numItems; i++ {
		if !iter(n.items[i]) {
			return false
		}
		if height > 0 {
			if !n.children[i+1].scan(iter, height-1) {
				return false
			}
		}
	}
	return true
}

// Reverse all items in tree
func (tr *BTree) Reverse(iter func(item *item.Item) bool) {
	if tr.root != nil {
		tr.root.reverse(iter, tr.height)
	}
}

func (n *node) reverse(iter func(item *item.Item) bool, height int) bool {
	if height == 0 {
		for i := n.numItems - 1; i >= 0; i-- {
			if !iter(n.items[i]) {
				return false
			}
		}
		return true
	}
	if !n.children[n.numItems].reverse(iter, height-1) {
		return false
	}
	for i := n.numItems - 1; i >= 0; i-- {
		if !iter(n.items[i]) {
			return false
		}
		if !n.children[i].reverse(iter, height-1) {
			return false
		}
	}
	return true
}

// Descend the tree within the range [pivot, first]
func (tr *BTree) Descend(
	pivot string,
	iter func(item *item.Item) bool,
) {
	if tr.root != nil {
		tr.root.descend(pivot, iter, tr.height)
	}
}

func (n *node) descend(pivot string, iter func(item *item.Item) bool, height int) bool {
	i, found := n.find(pivot)
	if !found {
		if height > 0 {
			if !n.children[i].descend(pivot, iter, height-1) {
				return false
			}
		}
		i--
	}
	for ; i >= 0; i-- {
		if !iter(n.items[i]) {
			return false
		}
		if height > 0 {
			if !n.children[i].reverse(iter, height-1) {
				return false
			}
		}
	}
	return true
}
