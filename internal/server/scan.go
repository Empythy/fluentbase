package server

import (
	"bytes"
	"errors"
	"time"

	"github.com/tidwall/geojson"
	"github.com/tidwall/resp"
	"github.com/gvsafronov/fluentbase/internal/glob"
)

func (c *Server) cmdScanArgs(vs []string) (
	s liveFenceSwitches, err error,
) {
	var t searchScanBaseTokens
	vs, t, err = c.parseSearchScanBaseTokens("scan", t, vs)
	if err != nil {
		return
	}
	s.searchScanBaseTokens = t
	if len(vs) != 0 {
		err = errInvalidNumberOfArguments
		return
	}
	return
}

func (c *Server) cmdScan(msg *Message) (res resp.Value, err error) {
	start := time.Now()
	vs := msg.Args[1:]

	s, err := c.cmdScanArgs(vs)
	if s.usingLua() {
		defer s.Close()
		defer func() {
			if r := recover(); r != nil {
				res = NOMessage
				err = errors.New(r.(string))
				return
			}
		}()
	}
	if err != nil {
		return NOMessage, err
	}
	wr := &bytes.Buffer{}
	sw, err := c.newScanWriter(
		wr, msg, s.key, s.output, s.precision, s.glob, false,
		s.cursor, s.limit, s.wheres, s.whereins, s.whereevals, s.nofields)
	if err != nil {
		return NOMessage, err
	}
	if msg.OutputType == JSON {
		wr.WriteString(`{"ok":true`)
	}
	sw.writeHead()
	if sw.col != nil {
		if sw.output == outputCount && len(sw.wheres) == 0 &&
			len(sw.whereins) == 0 && sw.globEverything == true {
			count := sw.col.Count() - int(s.cursor)
			if count < 0 {
				count = 0
			}
			sw.count = uint64(count)
		} else {
			g := glob.Parse(sw.globPattern, s.desc)
			if g.Limits[0] == "" && g.Limits[1] == "" {
				sw.col.Scan(s.desc, sw,
					func(id string, o geojson.Object, fields []float64) bool {
						return sw.writeObject(ScanWriterParams{
							id:     id,
							o:      o,
							fields: fields,
						})
					},
				)
			} else {
				sw.col.ScanRange(g.Limits[0], g.Limits[1], s.desc, sw,
					func(id string, o geojson.Object, fields []float64) bool {
						return sw.writeObject(ScanWriterParams{
							id:     id,
							o:      o,
							fields: fields,
						})
					},
				)
			}
		}
	}
	sw.writeFoot()
	if msg.OutputType == JSON {
		wr.WriteString(`,"elapsed":"` + time.Now().Sub(start).String() + "\"}")
		return resp.BytesValue(wr.Bytes()), nil
	}
	return sw.respOut, nil
}

// type tCursor struct {
// 	offset func() uint64
// 	iter   func(n uint64)
// }

// func (cursor *tCursor) Offset() uint64 {
// 	return cursor.offset()
// }

// func (cursor *tCursor) Step(n uint64) {
// 	cursor.iter(n)
// }

// func newCursor(offset func() uint64, iter func(n uint64)) *tCursor {
// 	return &tCursor{offset, iter}
// }
