package server

import (
	"strings"
	"time"

	"github.com/tidwall/resp"
	"github.com/gvsafronov/fluentbase/internal/log"
)

func (c *Server) cmdReadOnly(msg *Message) (res resp.Value, err error) {
	start := time.Now()
	vs := msg.Args[1:]
	var arg string
	var ok bool

	if vs, arg, ok = tokenval(vs); !ok || arg == "" {
		return NOMessage, errInvalidNumberOfArguments
	}
	if len(vs) != 0 {
		return NOMessage, errInvalidNumberOfArguments
	}
	update := false
	switch strings.ToLower(arg) {
	default:
		return NOMessage, errInvalidArgument(arg)
	case "yes":
		if !c.config.readOnly() {
			update = true
			c.config.setReadOnly(true)
			log.Info("read only")
		}
	case "no":
		if c.config.readOnly() {
			update = true
			c.config.setReadOnly(false)
			log.Info("read write")
		}
	}
	if update {
		c.config.write(false)
	}
	return OKMessage(msg, start), nil
}
