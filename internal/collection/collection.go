package collection

import (
	ifbtree "github.com/tidwall/btree"
	"github.com/tidwall/geojson"
	"github.com/tidwall/geojson/geo"
	"github.com/tidwall/geojson/geometry"
	"github.com/tidwall/tile38/internal/collection/btree"
	"github.com/tidwall/tile38/internal/collection/item"
	"github.com/tidwall/tile38/internal/collection/rtree"
)

// Cursor allows for quickly paging through Scan, Within, Intersects, and Nearby
type Cursor interface {
	Offset() uint64
	Step(count uint64)
}

// Collection represents a collection of geojson objects.
type Collection struct {
	items    btree.BTree    // items sorted by keys
	index    rtree.BoxTree  // items geospatially indexed
	values   *ifbtree.BTree // items sorted by value+key
	fieldMap map[string]int
	weight   int
	points   int
	objects  int // geometry count
	nobjects int // non-geometry count
}

var counter uint64

// New creates an empty collection
func New() *Collection {
	col := &Collection{
		values:   ifbtree.New(16, nil),
		fieldMap: make(map[string]int),
	}
	return col
}

// Count returns the number of objects in collection.
func (c *Collection) Count() int {
	return c.objects + c.nobjects
}

// StringCount returns the number of string values.
func (c *Collection) StringCount() int {
	return c.nobjects
}

// PointCount returns the number of points (lat/lon coordinates) in collection.
func (c *Collection) PointCount() int {
	return c.points
}

// TotalWeight calculates the in-memory cost of the collection in bytes.
func (c *Collection) TotalWeight() int {
	return c.weight
}

// Bounds returns the bounds of all the items in the collection.
func (c *Collection) Bounds() (minX, minY, maxX, maxY float64) {
	min, max := c.index.Bounds()
	if len(min) >= 2 && len(max) >= 2 {
		return min[0], min[1], max[0], max[1]
	}
	return
}

func objIsSpatial(obj geojson.Object) bool {
	_, ok := obj.(geojson.Spatial)
	return ok
}

func (c *Collection) addItem(item *item.Item) {
	if objIsSpatial(item.Obj()) {
		if !item.Obj().Empty() {
			rect := item.Obj().Rect()
			c.index.Insert(
				[]float64{rect.Min.X, rect.Min.Y},
				[]float64{rect.Max.X, rect.Max.Y},
				item)
		}
		c.objects++
	} else {
		c.values.ReplaceOrInsert(item)
		c.nobjects++
	}
	weight, points := item.WeightAndPoints()
	c.weight += weight
	c.points += points
}

func (c *Collection) delItem(item *item.Item) {
	if objIsSpatial(item.Obj()) {
		if !item.Obj().Empty() {
			rect := item.Obj().Rect()
			c.index.Delete(
				[]float64{rect.Min.X, rect.Min.Y},
				[]float64{rect.Max.X, rect.Max.Y},
				item)
		}
		c.objects--
	} else {
		c.values.Delete(item)
		c.nobjects--
	}
	weight, points := item.WeightAndPoints()
	c.weight -= weight
	c.points -= points
}

// Set adds or replaces an object in the collection and returns the fields
// array. If an item with the same id is already in the collection then the
// new item will adopt the old item's fields.
// The fields argument is optional.
// The return values are the old object, the old fields, and the new fields
func (c *Collection) Set(
	id string, obj geojson.Object, fields []string, values []float64,
) (
	oldObj geojson.Object, oldFields []float64, newFields []float64,
) {
	// create the new item
	item := item.New(id, obj)

	// add the new item to main btree and remove the old one if needed
	oldItemV, ok := c.items.Set(item)
	if ok {
		oldItem := oldItemV
		oldObj = oldItem.Obj()

		// remove old item from indexes
		c.delItem(oldItem)
		if len(oldItem.Fields()) > 0 {
			// merge old and new fields
			oldFields = oldItem.Fields()
			item.CopyOverFields(oldFields)
		}
	}

	if fields == nil && len(values) > 0 {
		// directly set the field values, from copy
		item.CopyOverFields(values)
	} else if len(fields) > 0 {
		// add new field to new item
		c.setFields(item, fields, values, false)
	}

	// add new item to indexes
	c.addItem(item)
	// fmt.Printf("!!! %#v\n", oldObj)

	return oldObj, oldFields, item.Fields()
}

func (c *Collection) setFields(
	item *item.Item, fieldNames []string, fieldValues []float64, updateWeight bool,
) (updatedCount int) {
	for i, fieldName := range fieldNames {
		var fieldValue float64
		if i < len(fieldValues) {
			fieldValue = fieldValues[i]
		}
		if c.setField(item, fieldName, fieldValue, updateWeight) {
			updatedCount++
		}
	}
	return updatedCount
}

func (c *Collection) setField(
	item *item.Item, fieldName string, fieldValue float64, updateWeight bool,
) (updated bool) {
	idx, ok := c.fieldMap[fieldName]
	if !ok {
		idx = len(c.fieldMap)
		c.fieldMap[fieldName] = idx
	}
	var pweight int
	if updateWeight {
		pweight, _ = item.WeightAndPoints()
	}
	updated = item.SetField(idx, fieldValue)
	if updateWeight && updated {
		nweight, _ := item.WeightAndPoints()
		c.weight = c.weight - pweight + nweight
	}
	return updated
}

// Delete removes an object and returns it.
// If the object does not exist then the 'ok' return value will be false.
func (c *Collection) Delete(id string) (
	obj geojson.Object, fields []float64, ok bool,
) {
	oldItemV, ok := c.items.Delete(id)
	if !ok {
		return nil, nil, false
	}
	oldItem := oldItemV

	c.delItem(oldItem)

	return oldItem.Obj(), oldItem.Fields(), true
}

// Get returns an object.
// If the object does not exist then the 'ok' return value will be false.
func (c *Collection) Get(id string) (
	obj geojson.Object, fields []float64, ok bool,
) {
	itemV, ok := c.items.Get(id)
	if !ok {
		return nil, nil, false
	}
	item := itemV

	return item.Obj(), item.Fields(), true
}

// SetField set a field value for an object and returns that object.
// If the object does not exist then the 'ok' return value will be false.
func (c *Collection) SetField(id, fieldName string, fieldValue float64) (
	obj geojson.Object, fields []float64, updated bool, ok bool,
) {
	itemV, ok := c.items.Get(id)
	if !ok {
		return nil, nil, false, false
	}
	item := itemV
	updated = c.setField(item, fieldName, fieldValue, true)
	return item.Obj(), item.Fields(), updated, true
}

// SetFields is similar to SetField, just setting multiple fields at once
func (c *Collection) SetFields(
	id string, fieldNames []string, fieldValues []float64,
) (obj geojson.Object, fields []float64, updatedCount int, ok bool) {
	itemV, ok := c.items.Get(id)
	if !ok {
		return nil, nil, 0, false
	}
	item := itemV

	updatedCount = c.setFields(item, fieldNames, fieldValues, true)

	return item.Obj(), item.Fields(), updatedCount, true
}

// FieldMap return a maps of the field names.
func (c *Collection) FieldMap() map[string]int {
	return c.fieldMap
}

// FieldArr return an array representation of the field names.
func (c *Collection) FieldArr() []string {
	arr := make([]string, len(c.fieldMap))
	for field, i := range c.fieldMap {
		arr[i] = field
	}
	return arr
}

// Scan iterates though the collection ids.
func (c *Collection) Scan(desc bool, cursor Cursor,
	iterator func(id string, obj geojson.Object, fields []float64) bool,
) bool {
	var keepon = true
	var count uint64
	var offset uint64
	if cursor != nil {
		offset = cursor.Offset()
		cursor.Step(offset)
	}
	iter := func(item *item.Item) bool {
		count++
		if count <= offset {
			return true
		}
		if cursor != nil {
			cursor.Step(1)
		}
		keepon = iterator(item.ID(), item.Obj(), item.Fields())
		return keepon
	}
	if desc {
		c.items.Reverse(iter)
	} else {
		c.items.Scan(iter)
	}
	return keepon
}

// ScanRange iterates though the collection starting with specified id.
func (c *Collection) ScanRange(start, end string, desc bool, cursor Cursor,
	iterator func(id string, obj geojson.Object, fields []float64) bool,
) bool {
	var keepon = true
	var count uint64
	var offset uint64
	if cursor != nil {
		offset = cursor.Offset()
		cursor.Step(offset)
	}
	iter := func(item *item.Item) bool {
		count++
		if count <= offset {
			return true
		}
		if cursor != nil {
			cursor.Step(1)
		}
		if !desc {
			if item.ID() >= end {
				return false
			}
		} else {
			if item.ID() <= end {
				return false
			}
		}
		keepon = iterator(item.ID(), item.Obj(), item.Fields())
		return keepon
	}

	if desc {
		c.items.Descend(start, iter)
	} else {
		c.items.Ascend(start, iter)
	}
	return keepon
}

// SearchValues iterates though the collection values.
func (c *Collection) SearchValues(desc bool, cursor Cursor,
	iterator func(id string, obj geojson.Object, fields []float64) bool,
) bool {
	var keepon = true
	var count uint64
	var offset uint64
	if cursor != nil {
		offset = cursor.Offset()
		cursor.Step(offset)
	}
	iter := func(v ifbtree.Item) bool {
		count++
		if count <= offset {
			return true
		}
		if cursor != nil {
			cursor.Step(1)
		}
		iitm := v.(*item.Item)
		keepon = iterator(iitm.ID(), iitm.Obj(), iitm.Fields())
		return keepon
	}
	if desc {
		c.values.Descend(iter)
	} else {
		c.values.Ascend(iter)
	}
	return keepon
}

// SearchValuesRange iterates though the collection values.
func (c *Collection) SearchValuesRange(start, end string, desc bool,
	cursor Cursor,
	iterator func(id string, obj geojson.Object, fields []float64) bool,
) bool {
	var keepon = true
	var count uint64
	var offset uint64
	if cursor != nil {
		offset = cursor.Offset()
		cursor.Step(offset)
	}
	iter := func(v ifbtree.Item) bool {
		count++
		if count <= offset {
			return true
		}
		if cursor != nil {
			cursor.Step(1)
		}
		iitm := v.(*item.Item)
		keepon = iterator(iitm.ID(), iitm.Obj(), iitm.Fields())
		return keepon
	}
	if desc {
		c.values.DescendRange(
			item.New("", String(start)), item.New("", String(end)), iter,
		)
	} else {
		c.values.AscendRange(
			item.New("", String(start)), item.New("", String(end)), iter,
		)
	}
	return keepon
}

// ScanGreaterOrEqual iterates though the collection starting with specified id.
func (c *Collection) ScanGreaterOrEqual(id string, desc bool,
	cursor Cursor,
	iterator func(id string, obj geojson.Object, fields []float64) bool,
) bool {
	var keepon = true
	var count uint64
	var offset uint64
	if cursor != nil {
		offset = cursor.Offset()
		cursor.Step(offset)
	}
	iter := func(item *item.Item) bool {
		count++
		if count <= offset {
			return true
		}
		if cursor != nil {
			cursor.Step(1)
		}
		keepon = iterator(item.ID(), item.Obj(), item.Fields())
		return keepon
	}
	if desc {
		c.items.Descend(id, iter)
	} else {
		c.items.Ascend(id, iter)
	}
	return keepon
}

func (c *Collection) geoSearch(
	rect geometry.Rect,
	iter func(id string, obj geojson.Object, fields []float64) bool,
) bool {
	alive := true
	c.index.Search(
		[]float64{rect.Min.X, rect.Min.Y},
		[]float64{rect.Max.X, rect.Max.Y},
		func(_, _ []float64, itemv *item.Item) bool {
			item := itemv
			alive = iter(item.ID(), item.Obj(), item.Fields())
			return alive
		},
	)
	return alive
}

func (c *Collection) geoSparse(
	obj geojson.Object, sparse uint8,
	iter func(id string, obj geojson.Object, fields []float64) (match, ok bool),
) bool {
	matches := make(map[string]bool)
	alive := true
	c.geoSparseInner(obj.Rect(), sparse,
		func(id string, o geojson.Object, fields []float64) (
			match, ok bool,
		) {
			ok = true
			if !matches[id] {
				match, ok = iter(id, o, fields)
				if match {
					matches[id] = true
				}
			}
			return match, ok
		},
	)
	return alive
}
func (c *Collection) geoSparseInner(
	rect geometry.Rect, sparse uint8,
	iter func(id string, obj geojson.Object, fields []float64) (match, ok bool),
) bool {
	if sparse > 0 {
		w := rect.Max.X - rect.Min.X
		h := rect.Max.Y - rect.Min.Y
		quads := [4]geometry.Rect{
			geometry.Rect{
				Min: geometry.Point{X: rect.Min.X, Y: rect.Min.Y + h/2},
				Max: geometry.Point{X: rect.Min.X + w/2, Y: rect.Max.Y},
			},
			geometry.Rect{
				Min: geometry.Point{X: rect.Min.X + w/2, Y: rect.Min.Y + h/2},
				Max: geometry.Point{X: rect.Max.X, Y: rect.Max.Y},
			},
			geometry.Rect{
				Min: geometry.Point{X: rect.Min.X, Y: rect.Min.Y},
				Max: geometry.Point{X: rect.Min.X + w/2, Y: rect.Min.Y + h/2},
			},
			geometry.Rect{
				Min: geometry.Point{X: rect.Min.X + w/2, Y: rect.Min.Y},
				Max: geometry.Point{X: rect.Max.X, Y: rect.Min.Y + h/2},
			},
		}
		for _, quad := range quads {
			if !c.geoSparseInner(quad, sparse-1, iter) {
				return false
			}
		}
		return true
	}
	alive := true
	c.geoSearch(rect,
		func(id string, obj geojson.Object, fields []float64) bool {
			match, ok := iter(id, obj, fields)
			if !ok {
				alive = false
				return false
			}
			return !match
		},
	)
	return alive
}

// Within returns all object that are fully contained within an object or
// bounding box. Set obj to nil in order to use the bounding box.
func (c *Collection) Within(
	obj geojson.Object,
	sparse uint8,
	cursor Cursor,
	iter func(id string, obj geojson.Object, fields []float64) bool,
) bool {
	var count uint64
	var offset uint64
	if cursor != nil {
		offset = cursor.Offset()
		cursor.Step(offset)
	}
	if sparse > 0 {
		return c.geoSparse(obj, sparse,
			func(id string, o geojson.Object, fields []float64) (
				match, ok bool,
			) {
				count++
				if count <= offset {
					return false, true
				}
				if cursor != nil {
					cursor.Step(1)
				}
				if match = o.Within(obj); match {
					ok = iter(id, o, fields)
				}
				return match, ok
			},
		)
	}
	return c.geoSearch(obj.Rect(),
		func(id string, o geojson.Object, fields []float64) bool {
			count++
			if count <= offset {
				return true
			}
			if cursor != nil {
				cursor.Step(1)
			}
			if o.Within(obj) {
				return iter(id, o, fields)
			}
			return true
		},
	)
}

// Intersects returns all object that are intersect an object or bounding box.
// Set obj to nil in order to use the bounding box.
func (c *Collection) Intersects(
	obj geojson.Object,
	sparse uint8,
	cursor Cursor,
	iter func(id string, obj geojson.Object, fields []float64) bool,
) bool {
	var count uint64
	var offset uint64
	if cursor != nil {
		offset = cursor.Offset()
		cursor.Step(offset)
	}
	if sparse > 0 {
		return c.geoSparse(obj, sparse,
			func(id string, o geojson.Object, fields []float64) (
				match, ok bool,
			) {
				count++
				if count <= offset {
					return false, true
				}
				if cursor != nil {
					cursor.Step(1)
				}
				if match = o.Intersects(obj); match {
					ok = iter(id, o, fields)
				}
				return match, ok
			},
		)
	}
	return c.geoSearch(obj.Rect(),
		func(id string, o geojson.Object, fields []float64) bool {
			count++
			if count <= offset {
				return true
			}
			if cursor != nil {
				cursor.Step(1)
			}
			if o.Intersects(obj) {
				return iter(id, o, fields)
			}
			return true
		},
	)
}

// Nearby returns the nearest neighbors
func (c *Collection) Nearby(
	target geojson.Object,
	cursor Cursor,
	iter func(id string, obj geojson.Object, fields []float64) bool,
) bool {
	// First look to see if there's at least one candidate in the circle's
	// outer rectangle. This is a fast-fail operation.
	if circle, ok := target.(*geojson.Circle); ok {
		meters := circle.Meters()
		if meters > 0 {
			center := circle.Center()
			minLat, minLon, maxLat, maxLon :=
				geo.RectFromCenter(center.Y, center.X, meters)
			var exists bool
			c.index.Search(
				[]float64{minLon, minLat},
				[]float64{maxLon, maxLat},
				func(_, _ []float64, itemv *item.Item) bool {
					exists = true
					return false
				},
			)
			if !exists {
				// no candidates
				return true
			}
		}
	}
	// do the kNN operation
	alive := true
	center := target.Center()
	var count uint64
	var offset uint64
	if cursor != nil {
		offset = cursor.Offset()
		cursor.Step(offset)
	}
	c.index.Nearby(
		[]float64{center.X, center.Y},
		[]float64{center.X, center.Y},
		func(_, _ []float64, itemv *item.Item) bool {
			count++
			if count <= offset {
				return true
			}
			if cursor != nil {
				cursor.Step(1)
			}
			item := itemv
			alive = iter(item.ID(), item.Obj(), item.Fields())
			return alive
		},
	)
	return alive
}
