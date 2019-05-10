package btree

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/geojson"
	"github.com/tidwall/geojson/geometry"
	"github.com/tidwall/tile38/internal/collection/item"
)

func init() {
	seed := time.Now().UnixNano()
	fmt.Printf("seed: %d\n", seed)
	rand.Seed(seed)
}

func randKeys(N int) (keys []string) {
	format := fmt.Sprintf("%%0%dd", len(fmt.Sprintf("%d", N-1)))
	for _, i := range rand.Perm(N) {
		keys = append(keys, fmt.Sprintf(format, i))
	}
	return
}

const flatLeaf = true

func (tr *BTree) print() {
	tr.root.print(0, tr.height)
}

func (n *node) print(level, height int) {
	if n == nil {
		println("NIL")
		return
	}
	if height == 0 && flatLeaf {
		fmt.Printf("%s", strings.Repeat("  ", level))
	}
	for i := 0; i < n.numItems; i++ {
		if height > 0 {
			n.children[i].print(level+1, height-1)
		}
		if height > 0 || (height == 0 && !flatLeaf) {
			fmt.Printf("%s%v\n", strings.Repeat("  ", level), n.items[i].ID())
		} else {
			if i > 0 {
				fmt.Printf(",")
			}
			fmt.Printf("%s", n.items[i].ID())
		}
	}
	if height == 0 && flatLeaf {
		fmt.Printf("\n")
	}
	if height > 0 {
		n.children[n.numItems].print(level+1, height-1)
	}
}

func (tr *BTree) deepPrint() {
	fmt.Printf("%#v\n", tr)
	tr.root.deepPrint(0, tr.height)
}

func (n *node) deepPrint(level, height int) {
	if n == nil {
		fmt.Printf("%s %#v\n", strings.Repeat("  ", level), n)
		return
	}
	fmt.Printf("%s count: %v\n", strings.Repeat("  ", level), n.numItems)
	fmt.Printf("%s items: %v\n", strings.Repeat("  ", level), n.items)
	if height > 0 {
		fmt.Printf("%s child: %v\n", strings.Repeat("  ", level), n.children)
	}
	if height > 0 {
		for i := 0; i < n.numItems; i++ {
			n.children[i].deepPrint(level+1, height-1)
		}
		n.children[n.numItems].deepPrint(level+1, height-1)
	}
}

func stringsEquals(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDescend(t *testing.T) {
	var tr BTree
	var count int
	tr.Descend("1", func(item *item.Item) bool {
		count++
		return true
	})
	if count > 0 {
		t.Fatalf("expected 0, got %v", count)
	}
	var keys []string
	for i := 0; i < 1000; i += 10 {
		keys = append(keys, fmt.Sprintf("%03d", i))
		tr.Set(item.New(keys[len(keys)-1], nil))
	}
	var exp []string
	tr.Reverse(func(item *item.Item) bool {
		exp = append(exp, item.ID())
		return true
	})
	for i := 999; i >= 0; i-- {
		var key string
		key = fmt.Sprintf("%03d", i)
		var all []string
		tr.Descend(key, func(item *item.Item) bool {
			all = append(all, item.ID())
			return true
		})
		for len(exp) > 0 && key < exp[0] {
			exp = exp[1:]
		}
		var count int
		tr.Descend(key, func(item *item.Item) bool {
			if count == (i+1)%maxItems {
				return false
			}
			count++
			return true
		})
		if count > len(exp) {
			t.Fatalf("expected 1, got %v", count)
		}

		if !stringsEquals(exp, all) {
			fmt.Printf("exp: %v\n", exp)
			fmt.Printf("all: %v\n", all)
			t.Fatal("mismatch")
		}
	}
}

func TestAscend(t *testing.T) {
	var tr BTree
	var count int
	tr.Ascend("1", func(item *item.Item) bool {
		count++
		return true
	})
	if count > 0 {
		t.Fatalf("expected 0, got %v", count)
	}
	var keys []string
	for i := 0; i < 1000; i += 10 {
		keys = append(keys, fmt.Sprintf("%03d", i))
		tr.Set(item.New(keys[len(keys)-1], nil))
	}
	exp := keys
	for i := -1; i < 1000; i++ {
		var key string
		if i == -1 {
			key = ""
		} else {
			key = fmt.Sprintf("%03d", i)
		}
		var all []string
		tr.Ascend(key, func(item *item.Item) bool {
			all = append(all, item.ID())
			return true
		})

		for len(exp) > 0 && key > exp[0] {
			exp = exp[1:]
		}
		var count int
		tr.Ascend(key, func(item *item.Item) bool {
			if count == (i+1)%maxItems {
				return false
			}
			count++
			return true
		})
		if count > len(exp) {
			t.Fatalf("expected 1, got %v", count)
		}
		if !stringsEquals(exp, all) {
			t.Fatal("mismatch")
		}
	}
}

func TestBTree(t *testing.T) {
	N := 10000
	var tr BTree
	keys := randKeys(N)

	// insert all items
	for _, key := range keys {
		value, replaced := tr.Set(item.New(key, testString(key)))
		if replaced {
			t.Fatal("expected false")
		}
		if value != nil {
			t.Fatal("expected nil")
		}
	}

	// check length
	if tr.Len() != len(keys) {
		t.Fatalf("expected %v, got %v", len(keys), tr.Len())
	}

	// get each value
	for _, key := range keys {
		value, gotten := tr.Get(key)
		if !gotten {
			t.Fatal("expected true")
		}
		if value == nil || value.Obj().String() != key {
			t.Fatalf("expected '%v', got '%v'", key, value)
		}
	}

	// scan all items
	var last string
	all := make(map[string]interface{})
	tr.Scan(func(item *item.Item) bool {
		if item.ID() <= last {
			t.Fatal("out of order")
		}
		if item.Obj().String() != item.ID() {
			t.Fatalf("mismatch")
		}
		last = item.ID()
		all[item.ID()] = item.Obj().String()
		return true
	})
	if len(all) != len(keys) {
		t.Fatalf("expected '%v', got '%v'", len(keys), len(all))
	}

	// reverse all items
	var prev string
	all = make(map[string]interface{})
	tr.Reverse(func(item *item.Item) bool {
		if prev != "" && item.ID() >= prev {
			t.Fatal("out of order")
		}
		if item.Obj().String() != item.ID() {
			t.Fatalf("mismatch")
		}
		prev = item.ID()
		all[item.ID()] = item.Obj().String()
		return true
	})
	if len(all) != len(keys) {
		t.Fatalf("expected '%v', got '%v'", len(keys), len(all))
	}

	// try to get an invalid item
	value, gotten := tr.Get("invalid")
	if gotten {
		t.Fatal("expected false")
	}
	if value != nil {
		t.Fatal("expected nil")
	}

	// scan and quit at various steps
	for i := 0; i < 100; i++ {
		var j int
		tr.Scan(func(item *item.Item) bool {
			if j == i {
				return false
			}
			j++
			return true
		})
	}

	// reverse and quit at various steps
	for i := 0; i < 100; i++ {
		var j int
		tr.Reverse(func(item *item.Item) bool {
			if j == i {
				return false
			}
			j++
			return true
		})
	}

	// delete half the items
	for _, key := range keys[:len(keys)/2] {
		value, deleted := tr.Delete(key)
		if !deleted {
			t.Fatal("expected true")
		}
		if value == nil || value.Obj().String() != key {
			t.Fatalf("expected '%v', got '%v'", key, value)
		}
	}

	// check length
	if tr.Len() != len(keys)/2 {
		t.Fatalf("expected %v, got %v", len(keys)/2, tr.Len())
	}

	// try delete half again
	for _, key := range keys[:len(keys)/2] {
		value, deleted := tr.Delete(key)
		if deleted {
			t.Fatal("expected false")
		}
		if value != nil {
			t.Fatalf("expected nil")
		}
	}

	// try delete half again
	for _, key := range keys[:len(keys)/2] {
		value, deleted := tr.Delete(key)
		if deleted {
			t.Fatal("expected false")
		}
		if value != nil {
			t.Fatalf("expected nil")
		}
	}

	// check length
	if tr.Len() != len(keys)/2 {
		t.Fatalf("expected %v, got %v", len(keys)/2, tr.Len())
	}

	// scan items
	last = ""
	all = make(map[string]interface{})
	tr.Scan(func(item *item.Item) bool {
		if item.ID() <= last {
			t.Fatal("out of order")
		}
		if item.Obj().String() != item.ID() {
			t.Fatalf("mismatch")
		}
		last = item.ID()
		all[item.ID()] = item.Obj().String()
		return true
	})
	if len(all) != len(keys)/2 {
		t.Fatalf("expected '%v', got '%v'", len(keys), len(all))
	}

	// replace second half
	for _, key := range keys[len(keys)/2:] {
		value, replaced := tr.Set(item.New(key, testString(key)))
		if !replaced {
			t.Fatal("expected true")
		}
		if value == nil || value.Obj().String() != key {
			t.Fatalf("expected '%v', got '%v'", key, value)
		}
	}

	// delete next half the items
	for _, key := range keys[len(keys)/2:] {
		value, deleted := tr.Delete(key)
		if !deleted {
			t.Fatal("expected true")
		}
		if value == nil || value.Obj().String() != key {
			t.Fatalf("expected '%v', got '%v'", key, value)
		}
	}

	// check length
	if tr.Len() != 0 {
		t.Fatalf("expected %v, got %v", 0, tr.Len())
	}

	// do some stuff on an empty tree
	value, gotten = tr.Get(keys[0])
	if gotten {
		t.Fatal("expected false")
	}
	if value != nil {
		t.Fatal("expected nil")
	}
	tr.Scan(func(item *item.Item) bool {
		t.Fatal("should not be reached")
		return true
	})
	tr.Reverse(func(item *item.Item) bool {
		t.Fatal("should not be reached")
		return true
	})

	var deleted bool
	value, deleted = tr.Delete("invalid")
	if deleted {
		t.Fatal("expected false")
	}
	if value != nil {
		t.Fatal("expected nil")
	}
}

func BenchmarkTidwallSequentialSet(b *testing.B) {
	var tr BTree
	keys := randKeys(b.N)
	sort.Strings(keys)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.Set(item.New(keys[i], nil))
	}
}

func BenchmarkTidwallSequentialGet(b *testing.B) {
	var tr BTree
	keys := randKeys(b.N)
	sort.Strings(keys)
	for i := 0; i < b.N; i++ {
		tr.Set(item.New(keys[i], nil))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.Get(keys[i])
	}
}

func BenchmarkTidwallRandomSet(b *testing.B) {
	var tr BTree
	keys := randKeys(b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.Set(item.New(keys[i], nil))
	}
}

func BenchmarkTidwallRandomGet(b *testing.B) {
	var tr BTree
	keys := randKeys(b.N)
	for i := 0; i < b.N; i++ {
		tr.Set(item.New(keys[i], nil))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.Get(keys[i])
	}
}

// type googleKind struct {
// 	key string
// }

// func (a *googleKind) Less(b btree.Item) bool {
// 	return a.key < b.(*googleKind).key
// }

// func BenchmarkGoogleSequentialSet(b *testing.B) {
// 	tr := btree.New(32)
// 	keys := randKeys(b.N)
// 	sort.Strings(keys)
// 	gkeys := make([]*googleKind, len(keys))
// 	for i := 0; i < b.N; i++ {
// 		gkeys[i] = &googleKind{keys[i]}
// 	}
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		tr.ReplaceOrInsert(gkeys[i])
// 	}
// }

// func BenchmarkGoogleSequentialGet(b *testing.B) {
// 	tr := btree.New(32)
// 	keys := randKeys(b.N)
// 	gkeys := make([]*googleKind, len(keys))
// 	for i := 0; i < b.N; i++ {
// 		gkeys[i] = &googleKind{keys[i]}
// 	}
// 	for i := 0; i < b.N; i++ {
// 		tr.ReplaceOrInsert(gkeys[i])
// 	}
// 	sort.Strings(keys)
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		tr.Get(gkeys[i])
// 	}
// }

// func BenchmarkGoogleRandomSet(b *testing.B) {
// 	tr := btree.New(32)
// 	keys := randKeys(b.N)
// 	gkeys := make([]*googleKind, len(keys))
// 	for i := 0; i < b.N; i++ {
// 		gkeys[i] = &googleKind{keys[i]}
// 	}
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		tr.ReplaceOrInsert(gkeys[i])
// 	}
// }

// func BenchmarkGoogleRandomGet(b *testing.B) {
// 	tr := btree.New(32)
// 	keys := randKeys(b.N)
// 	gkeys := make([]*googleKind, len(keys))
// 	for i := 0; i < b.N; i++ {
// 		gkeys[i] = &googleKind{keys[i]}
// 	}
// 	for i := 0; i < b.N; i++ {
// 		tr.ReplaceOrInsert(gkeys[i])
// 	}
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		tr.Get(gkeys[i])
// 	}
// }

func TestBTreeOne(t *testing.T) {
	var tr BTree
	tr.Set(item.New("1", testString("1")))
	tr.Delete("1")
	tr.Set(item.New("1", testString("1")))
	tr.Delete("1")
	tr.Set(item.New("1", testString("1")))
	tr.Delete("1")
}

func TestBTree256(t *testing.T) {
	var tr BTree
	var n int
	for j := 0; j < 2; j++ {
		for _, i := range rand.Perm(256) {
			tr.Set(item.New(fmt.Sprintf("%d", i), testString(fmt.Sprintf("%d", i))))
			n++
			if tr.Len() != n {
				t.Fatalf("expected 256, got %d", n)
			}
		}
		for _, i := range rand.Perm(256) {
			v, ok := tr.Get(fmt.Sprintf("%d", i))
			if !ok {
				t.Fatal("expected true")
			}
			if v.Obj().String() != fmt.Sprintf("%d", i) {
				t.Fatalf("expected %d, got %s", i, v.Obj().String())
			}
		}
		for _, i := range rand.Perm(256) {
			tr.Delete(fmt.Sprintf("%d", i))
			n--
			if tr.Len() != n {
				t.Fatalf("expected 256, got %d", n)
			}
		}
		for _, i := range rand.Perm(256) {
			_, ok := tr.Get(fmt.Sprintf("%d", i))
			if ok {
				t.Fatal("expected false")
			}
		}
	}
}

type testString string

func (s testString) Spatial() geojson.Spatial {
	return geojson.EmptySpatial{}
}
func (s testString) ForEach(iter func(geom geojson.Object) bool) bool {
	return iter(s)
}
func (s testString) Empty() bool {
	return true
}
func (s testString) Valid() bool {
	return false
}
func (s testString) Rect() geometry.Rect {
	return geometry.Rect{}
}
func (s testString) Center() geometry.Point {
	return geometry.Point{}
}
func (s testString) AppendJSON(dst []byte) []byte {
	data, _ := json.Marshal(string(s))
	return append(dst, data...)
}
func (s testString) String() string {
	return string(s)
}
func (s testString) JSON() string {
	return string(s.AppendJSON(nil))
}
func (s testString) MarshalJSON() ([]byte, error) {
	return s.AppendJSON(nil), nil
}
func (s testString) Within(obj geojson.Object) bool {
	return false
}
func (s testString) Contains(obj geojson.Object) bool {
	return false
}
func (s testString) Intersects(obj geojson.Object) bool {
	return false
}
func (s testString) NumPoints() int {
	return 0
}
func (s testString) Distance(obj geojson.Object) float64 {
	return 0
}
