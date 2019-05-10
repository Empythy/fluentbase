package item

import (
	"reflect"
	"unsafe"

	"github.com/tidwall/btree"
	"github.com/tidwall/geojson"
)

// Item is a item for Tile38 collections
type Item struct {
	point     bool           // true: Item is pointItem, false: Item is objItem
	fieldsLen uint16         // fields block size in bytes, not num of fields
	idLen     uint32         // id block size in bytes
	data      unsafe.Pointer // pointer to raw block of bytes, fields+id
}
type objItem struct {
	_   bool
	_   uint16
	_   uint32
	_   unsafe.Pointer
	obj geojson.Object
}
type pointItem struct {
	_  bool
	_  uint16
	_  uint32
	_  unsafe.Pointer
	pt geojson.SimplePoint
}

// ID returns the items ID as a string
func (item *Item) ID() string {
	return *(*string)((unsafe.Pointer)(&reflect.StringHeader{
		Data: uintptr(unsafe.Pointer(item.data)) + uintptr(item.fieldsLen),
		Len:  int(item.idLen),
	}))
}

// Fields returns the field values
func (item *Item) Fields() []float64 {
	return *(*[]float64)((unsafe.Pointer)(&reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(item.data)),
		Len:  int(item.fieldsLen) / 8,
		Cap:  int(item.fieldsLen) / 8,
	}))
}

// Obj returns the geojson object
func (item *Item) Obj() geojson.Object {
	if item.point {
		return &(*pointItem)(unsafe.Pointer(item)).pt
	}
	return (*objItem)(unsafe.Pointer(item)).obj
}

// New returns a newly allocated Item
func New(id string, obj geojson.Object) *Item {
	var item *Item
	if pt, ok := obj.(*geojson.SimplePoint); ok {
		pitem := new(pointItem)
		pitem.pt = *pt
		item = (*Item)(unsafe.Pointer(pitem))
		item.point = true
	} else {
		oitem := new(objItem)
		oitem.obj = obj
		item = (*Item)(unsafe.Pointer(oitem))
	}
	item.idLen = uint32(len(id))
	if len(id) > 0 {
		data := make([]byte, len(id))
		copy(data, id)
		item.data = unsafe.Pointer(&data[0])
	}
	return item
}

// WeightAndPoints returns the memory weight and number of points for Item.
func (item *Item) WeightAndPoints() (weight, points int) {
	_, objIsSpatial := item.Obj().(geojson.Spatial)
	if objIsSpatial {
		points = item.Obj().NumPoints()
		weight = points * 16
	} else if item.Obj() != nil {
		weight = len(item.Obj().String())
	}
	weight += int(item.fieldsLen) + int(item.idLen)
	return weight, points
}

// Less is a btree interface that compares if item is less than other item.
func (item *Item) Less(other btree.Item, ctx interface{}) bool {
	value1 := item.Obj().String()
	value2 := other.(*Item).Obj().String()
	if value1 < value2 {
		return true
	}
	if value1 > value2 {
		return false
	}
	// the values match so we'll compare IDs, which are always unique.
	return item.ID() < other.(*Item).ID()
}

// CopyOverFields overwriting previous fields
func (item *Item) CopyOverFields(values []float64) {
	fieldBytes := floatsToBytes(values)
	oldData := item.dataBytes()
	newData := make([]byte, len(fieldBytes)+int(item.idLen))
	copy(newData, fieldBytes)
	copy(newData[len(fieldBytes):], oldData[item.fieldsLen:])
	item.fieldsLen = uint16(len(fieldBytes))
	if len(newData) > 0 {
		item.data = unsafe.Pointer(&newData[0])
	} else {
		item.data = nil
	}
}

func getFieldAt(data unsafe.Pointer, index int) float64 {
	return *(*float64)(unsafe.Pointer(uintptr(data) + uintptr(index*8)))
}

func setFieldAt(data unsafe.Pointer, index int, value float64) {
	*(*float64)(unsafe.Pointer(uintptr(data) + uintptr(index*8))) = value
}

// SetField set a field value at specified index.
func (item *Item) SetField(index int, value float64) (updated bool) {
	numFields := int(item.fieldsLen / 8)
	if index < numFields {
		// field exists
		if getFieldAt(item.data, index) == value {
			return false
		}
	} else {
		// make room for new field
		oldBytes := item.dataBytes()
		newData := make([]byte, (index+1)*8+int(item.idLen))
		// copy the existing fields
		copy(newData, oldBytes[:item.fieldsLen])
		// copy the id
		copy(newData[(index+1)*8:], oldBytes[item.fieldsLen:])
		// update the fields length
		item.fieldsLen = uint16((index + 1) * 8)
		// update the raw data
		item.data = unsafe.Pointer(&newData[0])
	}
	// set the new field
	setFieldAt(item.data, index, value)
	return true
}

// GetField returns the value for a field at index.
func (item *Item) GetField(index int) float64 {
	numFields := int(item.fieldsLen / 8)
	if index < numFields {
		return getFieldAt(item.data, index)
	}
	return 0
}

func (item *Item) dataBytes() []byte {
	return *(*[]byte)((unsafe.Pointer)(&reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(item.data)),
		Len:  int(item.fieldsLen) + int(item.idLen),
		Cap:  int(item.fieldsLen) + int(item.idLen),
	}))
}

func floatsToBytes(f []float64) []byte {
	return *(*[]byte)((unsafe.Pointer)(&reflect.SliceHeader{
		Data: ((*reflect.SliceHeader)(unsafe.Pointer(&f))).Data,
		Len:  len(f) * 8,
		Cap:  len(f) * 8,
	}))
}
