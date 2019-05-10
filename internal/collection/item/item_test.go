package item

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/tidwall/geojson"
	"github.com/tidwall/geojson/geometry"
)

func testRandItem(t *testing.T) {
	keyb := make([]byte, rand.Int()%16)
	rand.Read(keyb)
	key := string(keyb)
	values := make([]float64, rand.Int()%1024)
	for i := range values {
		values[i] = rand.Float64()
	}
	item := New(key, geojson.NewPoint(geometry.Point{X: 1, Y: 2}))
	if item.ID() != key {
		t.Fatalf("expected '%v', got '%v'", key, item.ID())
	}

	var setValues []int
	for _, i := range rand.Perm(len(values)) {
		if !item.SetField(i, values[i]) {
			t.Fatal("expected true")
		}
		setValues = append(setValues, i)
		if item.ID() != key {
			t.Fatalf("expected '%v', got '%v'", key, item.ID())
		}
		for _, i := range setValues {
			if item.GetField(i) != values[i] {
				t.Fatalf("expected '%v', got '%v'", values[i], item.GetField(i))
			}
		}
		fields := item.Fields()
		for i := 0; i < len(fields); i++ {
			for _, j := range setValues {
				if i == j {
					if fields[i] != values[i] {
						t.Fatalf("expected '%v', got '%v'", values[i], fields[i])
					}
					break
				}
			}
		}
		weight, points := item.WeightAndPoints()
		if weight != len(fields)*8+len(key)+points*16 {
			t.Fatalf("expected '%v', got '%v'", len(fields)*8+len(key)+points*16, weight)
		}
		if points != 1 {
			t.Fatalf("expected '%v', got '%v'", 1, points)
		}
	}
	if item.GetField(len(values)) != 0 {
		t.Fatalf("expected '%v', got '%v'", 0, item.GetField(len(values)))
	}
	for _, i := range rand.Perm(len(values)) {
		if item.SetField(i, values[i]) {
			t.Fatal("expected false")
		}
	}
	if item.ID() != key {
		t.Fatalf("expected '%v', got '%v'", key, item.ID())
	}
	if item.Obj().NumPoints() != 1 {
		t.Fatalf("expected '%v', got '%v'", 1, item.Obj().NumPoints())
	}
	item.CopyOverFields(values)
	weight, points := item.WeightAndPoints()
	if weight != len(values)*8+len(key)+points*16 {
		t.Fatalf("expected '%v', got '%v'", len(values)*8+len(key)+points*16, weight)
	}
	if points != 1 {
		t.Fatalf("expected '%v', got '%v'", 1, points)
	}
	if !reflect.DeepEqual(item.Fields(), values) {
		t.Fatalf("expected '%v', got '%v'", values, item.Fields())
	}

	item.CopyOverFields(nil)
	weight, points = item.WeightAndPoints()
	if weight != len(key)+points*16 {
		t.Fatalf("expected '%v', got '%v'", len(key)+points*16, weight)
	}
	if points != 1 {
		t.Fatalf("expected '%v', got '%v'", 1, points)
	}
	if len(item.Fields()) != 0 {
		t.Fatalf("expected '%#v', got '%#v'", 0, len(item.Fields()))
	}
	if item.ID() != key {
		t.Fatalf("expected '%v', got '%v'", key, item.ID())
	}
}

func TestItem(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	start := time.Now()
	for time.Since(start) < time.Second {
		testRandItem(t)
	}
}

func TestItemLess(t *testing.T) {
	item0 := New("0", testString("0"))
	item1 := New("1", testString("1"))
	item2 := New("1", testString("2"))
	item3 := New("3", testString("2"))
	if !item0.Less(item1, nil) {
		t.Fatal("expected true")
	}
	if item1.Less(item0, nil) {
		t.Fatal("expected false")
	}
	if !item1.Less(item2, nil) {
		t.Fatal("expected true")
	}
	if item2.Less(item1, nil) {
		t.Fatal("expected false")
	}
	if !item2.Less(item3, nil) {
		t.Fatal("expected true")
	}
	if item3.Less(item2, nil) {
		t.Fatal("expected false")
	}
	weight, points := item0.WeightAndPoints()
	if weight != 2 {
		t.Fatalf("expected '%v', got '%v'", 2, weight)
	}
	if points != 0 {
		t.Fatalf("expected '%v', got '%v'", 0, points)
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
