package server

import (
	"bytes"
	"strings"
	"time"

	"github.com/tidwall/resp"
	"github.com/gvsafronov/fluentbase/internal/glob"
)

func (c *Server) cmdKeys(msg *Message) (res resp.Value, err error) {
	var start = time.Now()
	vs := msg.Args[1:]

	var pattern string
	var ok bool
	if vs, pattern, ok = tokenval(vs); !ok || pattern == "" {
		return NOMessage, errInvalidNumberOfArguments
	}
	if len(vs) != 0 {
		return NOMessage, errInvalidNumberOfArguments
	}

	var wr = &bytes.Buffer{}
	var once bool
	if msg.OutputType == JSON {
		wr.WriteString(`{"ok":true,"keys":[`)
	}
	var wild bool
	if strings.Contains(pattern, "*") {
		wild = true
	}
	var everything bool
	var greater bool
	var greaterPivot string
	var vals []resp.Value

	iterator := func(key string, value interface{}) bool {
		var match bool
		if everything {
			match = true
		} else if greater {
			if !strings.HasPrefix(key, greaterPivot) {
				return false
			}
			match = true
		} else {
			match, _ = glob.Match(pattern, key)
		}
		if match {
			if once {
				if msg.OutputType == JSON {
					wr.WriteByte(',')
				}
			} else {
				once = true
			}
			switch msg.OutputType {
			case JSON:
				wr.WriteString(jsonString(key))
			case RESP:
				vals = append(vals, resp.StringValue(key))
			}

			// If no more than one match is expected, stop searching
			if !wild {
				return false
			}
		}
		return true
	}

	// TODO: This can be further optimized by using glob.Parse and limits
	if pattern == "*" {
		everything = true
		c.cols.Scan(iterator)
	} else if strings.HasSuffix(pattern, "*") {
		greaterPivot = pattern[:len(pattern)-1]
		if glob.IsGlob(greaterPivot) {
			c.cols.Scan(iterator)
		} else {
			greater = true
			c.cols.Ascend(greaterPivot, iterator)
		}
	} else {
		c.cols.Scan(iterator)
	}
	if msg.OutputType == JSON {
		wr.WriteString(`],"elapsed":"` + time.Now().Sub(start).String() + "\"}")
		return resp.StringValue(wr.String()), nil
	}
	return resp.ArrayValue(vals), nil
}
