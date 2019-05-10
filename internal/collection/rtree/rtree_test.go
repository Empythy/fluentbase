package rtree

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/geojson"
	"github.com/tidwall/geojson/geometry"
	"github.com/tidwall/tile38/internal/collection/item"
)

type tBox struct {
	min [dims]float64
	max [dims]float64
}

var boxes []*item.Item
var points []*item.Item

func init() {
	seed := time.Now().UnixNano()
	// seed = 1532132365683340889
	println("seed:", seed)
	rand.Seed(seed)
}

func boxMin(box *item.Item) []float64 {
	return box.Obj().(*tBox).min[:]
}
func boxMax(box *item.Item) []float64 {
	return box.Obj().(*tBox).max[:]
}

func randPoints(N int) []*item.Item {
	boxes := make([]*item.Item, N)
	for i := 0; i < N; i++ {
		box := new(tBox)
		box.min[0] = rand.Float64()*360 - 180
		box.min[1] = rand.Float64()*180 - 90
		for j := 2; j < dims; j++ {
			box.min[j] = rand.Float64()
		}
		box.max = box.min
		boxes[i] = item.New(fmt.Sprintf("%d", i), box)
	}
	return boxes
}

func randBoxes(N int) []*item.Item {
	boxes := make([]*item.Item, N)
	for i := 0; i < N; i++ {
		box := new(tBox)
		box.min[0] = rand.Float64()*360 - 180
		box.min[1] = rand.Float64()*180 - 90
		for j := 2; j < dims; j++ {
			box.min[j] = rand.Float64() * 100
		}
		box.max[0] = box.min[0] + rand.Float64()
		box.max[1] = box.min[1] + rand.Float64()
		for j := 2; j < dims; j++ {
			box.max[j] = box.min[j] + rand.Float64()
		}
		if box.max[0] > 180 || box.max[1] > 90 {
			i--
		}
		boxes[i] = item.New(fmt.Sprintf("%d", i), box)
	}
	return boxes
}

func sortBoxes(boxes []*item.Item) {
	sort.Slice(boxes, func(i, j int) bool {
		iMin, iMax := boxMin(boxes[i]), boxMax(boxes[i])
		jMin, jMax := boxMin(boxes[j]), boxMax(boxes[j])
		for k := 0; k < len(iMin); k++ {
			if iMin[k] < jMin[k] {
				return true
			}
			if iMin[k] > jMin[k] {
				return false
			}
			if iMax[k] < jMax[k] {
				return true
			}
			if iMax[k] > jMax[k] {
				return false
			}
		}
		return i < j
	})
}

func sortBoxesNearby(boxes []*item.Item, min, max []float64) {
	sort.Slice(boxes, func(i, j int) bool {
		return testBoxDist(boxMin(boxes[i]), boxMax(boxes[i]), min, max) <
			testBoxDist(boxMin(boxes[j]), boxMax(boxes[j]), min, max)
	})
}

func testBoxDist(amin, amax, bmin, bmax []float64) float64 {
	var dist float64
	for i := 0; i < len(amin); i++ {
		var min, max float64
		if amin[i] > bmin[i] {
			min = amin[i]
		} else {
			min = bmin[i]
		}
		if amax[i] < bmax[i] {
			max = amax[i]
		} else {
			max = bmax[i]
		}
		squared := min - max
		if squared > 0 {
			dist += squared * squared
		}
	}
	return dist
}

func testBoxesVarious(t *testing.T, items []*item.Item, label string) {
	N := len(boxes)

	var tr BoxTree

	// N := 10000
	// boxes := randPoints(N)

	/////////////////////////////////////////
	// insert
	/////////////////////////////////////////
	for i := 0; i < N; i++ {
		tr.Insert(boxMin(boxes[i]), boxMax(boxes[i]), boxes[i])
	}
	if tr.Count() != N {
		t.Fatalf("expected %d, got %d", N, tr.Count())
	}
	// area := tr.TotalOverlapArea()
	// fmt.Printf("overlap:    %.0f, %.1f/item\n", area, area/float64(N))

	//	ioutil.WriteFile(label+".svg", []byte(rtreetools.SVG(&tr)), 0600)

	/////////////////////////////////////////
	// scan all items and count one-by-one
	/////////////////////////////////////////
	var count int
	tr.Scan(func(min, max []float64, _ *item.Item) bool {
		count++
		return true
	})
	if count != N {
		t.Fatalf("expected %d, got %d", N, count)
	}

	/////////////////////////////////////////
	// check every point for correctness
	/////////////////////////////////////////
	var tboxes1 []*item.Item
	tr.Scan(func(min, max []float64, item *item.Item) bool {
		tboxes1 = append(tboxes1, item)
		return true
	})
	tboxes2 := make([]*item.Item, len(boxes))
	copy(tboxes2, boxes)
	sortBoxes(tboxes1)
	sortBoxes(tboxes2)
	for i := 0; i < len(tboxes1); i++ {
		if tboxes1[i] != tboxes2[i] {
			t.Fatalf("expected '%v', got '%v'", tboxes2[i], tboxes1[i])
		}
	}

	/////////////////////////////////////////
	// search for each item one-by-one
	/////////////////////////////////////////
	for i := 0; i < N; i++ {
		var found bool
		tr.Search(boxMin(boxes[i]), boxMax(boxes[i]),
			func(min, max []float64, v *item.Item) bool {
				if v == boxes[i] {
					found = true
					return false
				}
				return true
			})
		if !found {
			t.Fatalf("did not find item %d", i)
		}
	}

	centerMin, centerMax := []float64{-18, -9}, []float64{18, 9}
	for j := 2; j < dims; j++ {
		centerMin = append(centerMin, -10)
		centerMax = append(centerMax, 10)
	}

	/////////////////////////////////////////
	// search for 10% of the items
	/////////////////////////////////////////
	for i := 0; i < N/5; i++ {
		var count int
		tr.Search(centerMin, centerMax,
			func(min, max []float64, _ *item.Item) bool {
				count++
				return true
			},
		)
	}

	/////////////////////////////////////////
	// delete every other item
	/////////////////////////////////////////
	for i := 0; i < N/2; i++ {
		j := i * 2
		tr.Delete(boxMin(boxes[j]), boxMax(boxes[j]), boxes[j])
	}

	/////////////////////////////////////////
	// count all items. should be half of N
	/////////////////////////////////////////
	count = 0
	tr.Scan(func(min, max []float64, _ *item.Item) bool {
		count++
		return true
	})
	if count != N/2 {
		t.Fatalf("expected %d, got %d", N/2, count)
	}

	///////////////////////////////////////////////////
	// reinsert every other item, but in random order
	///////////////////////////////////////////////////
	var ij []int
	for i := 0; i < N/2; i++ {
		j := i * 2
		ij = append(ij, j)
	}
	rand.Shuffle(len(ij), func(i, j int) {
		ij[i], ij[j] = ij[j], ij[i]
	})
	for i := 0; i < N/2; i++ {
		j := ij[i]
		tr.Insert(boxMin(boxes[j]), boxMax(boxes[j]), boxes[j])
	}

	//////////////////////////////////////////////////////
	// replace each item with an item that is very close
	//////////////////////////////////////////////////////
	var nboxes = make([]*item.Item, N)
	for i := 0; i < N; i++ {
		box := boxes[i].Obj().(*tBox)
		nbox := new(tBox)
		for j := 0; j < len(box.min); j++ {
			nbox.min[j] = box.min[j] + (rand.Float64() - 0.5)
			if box.min == box.max {
				nbox.max[j] = nbox.min[j]
			} else {
				nbox.max[j] = box.max[j] + (rand.Float64() - 0.5)
			}
		}
		nboxes[i] = item.New(fmt.Sprintf("%d", i), nbox)
	}
	for i := 0; i < N; i++ {
		tr.Insert(boxMin(nboxes[i]), boxMax(nboxes[i]), nboxes[i])
		tr.Delete(boxMin(boxes[i]), boxMax(boxes[i]), boxes[i])
	}
	if tr.Count() != N {
		t.Fatalf("expected %d, got %d", N, tr.Count())
	}
	// area = tr.TotalOverlapArea()
	// fmt.Fprintf(wr, "overlap:    %.0f, %.1f/item\n", area, area/float64(N))

	/////////////////////////////////////////
	// check every point for correctness
	/////////////////////////////////////////
	tboxes1 = nil
	tr.Scan(func(min, max []float64, value *item.Item) bool {
		tboxes1 = append(tboxes1, value)
		return true
	})
	tboxes2 = make([]*item.Item, len(nboxes))
	copy(tboxes2, nboxes)
	sortBoxes(tboxes1)
	sortBoxes(tboxes2)
	for i := 0; i < len(tboxes1); i++ {
		if tboxes1[i] != tboxes2[i] {
			t.Fatalf("expected '%v', got '%v'", tboxes2[i], tboxes1[i])
		}
	}

	/////////////////////////////////////////
	// search for 10% of the items
	/////////////////////////////////////////
	for i := 0; i < N/5; i++ {
		var count int
		tr.Search(centerMin, centerMax,
			func(min, max []float64, value *item.Item) bool {
				count++
				return true
			},
		)
	}

	var boxes3 []*item.Item
	tr.Nearby(centerMin, centerMax,
		func(min, max []float64, value *item.Item) bool {
			boxes3 = append(boxes3, value)
			return true
		},
	)
	if len(boxes3) != len(nboxes) {
		t.Fatalf("expected %d, got %d", len(nboxes), len(boxes3))
	}
	if len(boxes3) != tr.Count() {
		t.Fatalf("expected %d, got %d", tr.Count(), len(boxes3))
	}
	var ldist float64
	for i, box := range boxes3 {
		dist := testBoxDist(boxMin(box), boxMax(box), centerMin, centerMax)
		if i > 0 && dist < ldist {
			t.Fatalf("out of order")
		}
		ldist = dist
	}
}

func TestRandomBoxes(t *testing.T) {
	testBoxesVarious(t, randBoxes(10000), "boxes")
}

func TestRandomPoints(t *testing.T) {
	testBoxesVarious(t, randPoints(10000), "points")
}

func (r *box) boxstr() string {
	var b []byte
	b = append(b, '[', '[')
	for i := 0; i < len(r.min); i++ {
		if i != 0 {
			b = append(b, ' ')
		}
		b = strconv.AppendFloat(b, r.min[i], 'f', -1, 64)
	}
	b = append(b, ']', '[')
	for i := 0; i < len(r.max); i++ {
		if i != 0 {
			b = append(b, ' ')
		}
		b = strconv.AppendFloat(b, r.max[i], 'f', -1, 64)
	}
	b = append(b, ']', ']')
	return string(b)
}

func (r *box) print(height, indent int) {
	fmt.Printf("%s%s", strings.Repeat("  ", indent), r.boxstr())
	if height == 0 {
		fmt.Printf("\t'%v'\n", r.data)
	} else {
		fmt.Printf("\n")
		for i := 0; i < (*node)(r.data).count; i++ {
			(*node)(r.data).boxes[i].print(height-1, indent+1)
		}
	}

}

func (tr BoxTree) print() {
	if tr.root.data == nil {
		println("EMPTY TREE")
		return
	}
	tr.root.print(tr.height+1, 0)
}

func TestZeroPoints(t *testing.T) {
	N := 10000
	var tr BoxTree
	pt := make([]float64, dims)
	for i := 0; i < N; i++ {
		tr.Insert(pt, nil, nil)
	}
}

func BenchmarkRandomInsert(b *testing.B) {
	var tr BoxTree
	boxes := randBoxes(b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.Insert(boxMin(boxes[i]), boxMax(boxes[i]), nil)
	}
}

func (s *tBox) Spatial() geojson.Spatial {
	return geojson.EmptySpatial{}
}
func (s *tBox) ForEach(iter func(geom geojson.Object) bool) bool {
	return iter(s)
}
func (s *tBox) Empty() bool {
	return true
}
func (s *tBox) Valid() bool {
	return false
}
func (s *tBox) Rect() geometry.Rect {
	return geometry.Rect{}
}
func (s *tBox) Center() geometry.Point {
	return geometry.Point{}
}
func (s *tBox) AppendJSON(dst []byte) []byte {
	return nil
}
func (s *tBox) String() string {
	return ""
}
func (s *tBox) JSON() string {
	return string(s.AppendJSON(nil))
}
func (s *tBox) MarshalJSON() ([]byte, error) {
	return s.AppendJSON(nil), nil
}
func (s *tBox) Within(obj geojson.Object) bool {
	return false
}
func (s *tBox) Contains(obj geojson.Object) bool {
	return false
}
func (s *tBox) Intersects(obj geojson.Object) bool {
	return false
}
func (s *tBox) NumPoints() int {
	return 0
}
func (s *tBox) Distance(obj geojson.Object) float64 {
	return 0
}
