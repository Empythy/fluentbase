package server

import (
	"bytes"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/buntdb"
	"github.com/tidwall/resp"
	"github.com/gvsafronov/fluentbase/internal/endpoint"
	"github.com/gvsafronov/fluentbase/internal/glob"
	"github.com/gvsafronov/fluentbase/internal/log"
)

var hookLogSetDefaults = &buntdb.SetOptions{
	Expires: true, // automatically delete after 30 seconds
	TTL:     time.Second * 30,
}

type hooksByName []*Hook

func (a hooksByName) Len() int {
	return len(a)
}

func (a hooksByName) Less(i, j int) bool {
	return a[i].Name < a[j].Name
}

func (a hooksByName) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (c *Server) cmdSetHook(msg *Message, chanCmd bool) (
	res resp.Value, d commandDetails, err error,
) {
	start := time.Now()
	vs := msg.Args[1:]
	var name, urls, cmd string
	var ok bool
	if vs, name, ok = tokenval(vs); !ok || name == "" {
		return NOMessage, d, errInvalidNumberOfArguments
	}
	var endpoints []string
	if chanCmd {
		endpoints = []string{"local://" + name}
	} else {
		if vs, urls, ok = tokenval(vs); !ok || urls == "" {
			return NOMessage, d, errInvalidNumberOfArguments
		}
		for _, url := range strings.Split(urls, ",") {
			url = strings.TrimSpace(url)
			err := c.epc.Validate(url)
			if err != nil {
				log.Errorf("sethook: %v", err)
				return resp.SimpleStringValue(""), d, errInvalidArgument(url)
			}
			endpoints = append(endpoints, url)
		}
	}
	var commandvs []string
	var cmdlc string
	var types []string
	var expires float64
	var expiresSet bool
	metaMap := make(map[string]string)
	for {
		commandvs = vs
		if vs, cmd, ok = tokenval(vs); !ok || cmd == "" {
			return NOMessage, d, errInvalidNumberOfArguments
		}
		cmdlc = strings.ToLower(cmd)
		switch cmdlc {
		default:
			return NOMessage, d, errInvalidArgument(cmd)
		case "meta":
			var metakey string
			var metaval string
			if vs, metakey, ok = tokenval(vs); !ok || metakey == "" {
				return NOMessage, d, errInvalidNumberOfArguments
			}
			if vs, metaval, ok = tokenval(vs); !ok || metaval == "" {
				return NOMessage, d, errInvalidNumberOfArguments
			}
			metaMap[metakey] = metaval
			continue
		case "ex":
			var s string
			if vs, s, ok = tokenval(vs); !ok || s == "" {
				return NOMessage, d, errInvalidNumberOfArguments
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return NOMessage, d, errInvalidArgument(s)
			}
			expires = v
			expiresSet = true
			continue
		case "nearby":
			types = nearbyTypes
		case "within", "intersects":
			types = withinOrIntersectsTypes
		}
		break
	}
	s, err := c.cmdSearchArgs(true, cmdlc, vs, types)
	defer s.Close()
	if err != nil {
		return NOMessage, d, err
	}
	if !s.fence {
		return NOMessage, d, errors.New("missing FENCE argument")
	}
	s.cmd = cmdlc
	cmsg := &Message{}
	*cmsg = *msg
	cmsg.Args = make([]string, len(commandvs))
	for i := 0; i < len(commandvs); i++ {
		cmsg.Args[i] = commandvs[i]
	}
	metas := make([]FenceMeta, 0, len(metaMap))
	for key, val := range metaMap {
		metas = append(metas, FenceMeta{key, val})
	}
	sort.Sort(hookMetaByName(metas))

	hook := &Hook{
		Key:       s.key,
		Name:      name,
		Endpoints: endpoints,
		Fence:     &s,
		Message:   cmsg,
		epm:       c.epc,
		Metas:     metas,
		channel:   chanCmd,
		cond:      sync.NewCond(&sync.Mutex{}),
	}
	if expiresSet {
		hook.expires =
			time.Now().Add(time.Duration(expires * float64(time.Second)))
	}
	if !chanCmd {
		hook.db = c.qdb
	}
	var wr bytes.Buffer
	hook.ScanWriter, err = c.newScanWriter(
		&wr, cmsg, s.key, s.output, s.precision, s.glob, false,
		s.cursor, s.limit, s.wheres, s.whereins, s.whereevals, s.nofields)
	if err != nil {

		return NOMessage, d, err
	}
	prevHook := c.hooks[name]
	if prevHook != nil {
		if prevHook.channel != chanCmd {
			return NOMessage, d,
				errors.New("hooks and channels cannot share the same name")
		}
		if prevHook.Equals(hook) {
			// it was a match so we do nothing. But let's signal just
			// for good measure.
			prevHook.Signal()
			if !hook.expires.IsZero() {
				c.hookex.Push(hook)
			}
			switch msg.OutputType {
			case JSON:
				return OKMessage(msg, start), d, nil
			case RESP:
				return resp.IntegerValue(0), d, nil
			}
		}
		prevHook.Close()
		delete(c.hooks, name)
		delete(c.hooksOut, name)
	}

	d.updated = true
	d.timestamp = time.Now()

	c.hooks[name] = hook
	if hook.Fence.detect == nil || hook.Fence.detect["outside"] {
		c.hooksOut[name] = hook
	}

	// remove previous hook from spatial index
	if prevHook != nil && prevHook.Fence != nil && prevHook.Fence.obj != nil {
		rect := prevHook.Fence.obj.Rect()
		c.hookTree.Delete(
			[]float64{rect.Min.X, rect.Min.Y},
			[]float64{rect.Max.X, rect.Max.Y},
			prevHook)
	}
	// add hook to spatial index
	if hook != nil && hook.Fence != nil && hook.Fence.obj != nil {
		rect := hook.Fence.obj.Rect()
		c.hookTree.Insert(
			[]float64{rect.Min.X, rect.Min.Y},
			[]float64{rect.Max.X, rect.Max.Y},
			hook)
	}

	hook.Open() // Opens a goroutine to notify the hook
	if !hook.expires.IsZero() {
		c.hookex.Push(hook)
	}
	switch msg.OutputType {
	case JSON:
		return OKMessage(msg, start), d, nil
	case RESP:
		return resp.IntegerValue(1), d, nil
	}
	return NOMessage, d, nil
}

func (c *Server) cmdDelHook(msg *Message, chanCmd bool) (
	res resp.Value, d commandDetails, err error,
) {
	start := time.Now()
	vs := msg.Args[1:]

	var name string
	var ok bool
	if vs, name, ok = tokenval(vs); !ok || name == "" {
		return NOMessage, d, errInvalidNumberOfArguments
	}
	if len(vs) != 0 {
		return NOMessage, d, errInvalidNumberOfArguments
	}
	if hook, ok := c.hooks[name]; ok && hook.channel == chanCmd {
		hook.Close()
		// remove hook from maps
		delete(c.hooks, hook.Name)
		delete(c.hooksOut, hook.Name)
		// remove hook from spatial index
		if hook != nil && hook.Fence != nil && hook.Fence.obj != nil {
			rect := hook.Fence.obj.Rect()
			c.hookTree.Delete(
				[]float64{rect.Min.X, rect.Min.Y},
				[]float64{rect.Max.X, rect.Max.Y},
				hook)
		}
		d.updated = true
	}
	d.timestamp = time.Now()

	switch msg.OutputType {
	case JSON:
		return OKMessage(msg, start), d, nil
	case RESP:
		if d.updated {
			return resp.IntegerValue(1), d, nil
		}
		return resp.IntegerValue(0), d, nil
	}
	return
}

func (c *Server) cmdPDelHook(msg *Message, channel bool) (
	res resp.Value, d commandDetails, err error,
) {
	start := time.Now()
	vs := msg.Args[1:]

	var pattern string
	var ok bool
	if vs, pattern, ok = tokenval(vs); !ok || pattern == "" {
		return NOMessage, d, errInvalidNumberOfArguments
	}
	if len(vs) != 0 {
		return NOMessage, d, errInvalidNumberOfArguments
	}

	count := 0
	for name, hook := range c.hooks {
		if hook.channel != channel {
			continue
		}
		match, _ := glob.Match(pattern, name)
		if !match {
			continue
		}
		hook.Close()
		// remove hook from maps
		delete(c.hooks, hook.Name)
		delete(c.hooksOut, hook.Name)
		// remove hook from spatial index
		if hook != nil && hook.Fence != nil && hook.Fence.obj != nil {
			rect := hook.Fence.obj.Rect()
			c.hookTree.Delete(
				[]float64{rect.Min.X, rect.Min.Y},
				[]float64{rect.Max.X, rect.Max.Y},
				hook)
		}
		d.updated = true
		count++
	}
	d.timestamp = time.Now()

	switch msg.OutputType {
	case JSON:
		return OKMessage(msg, start), d, nil
	case RESP:
		return resp.IntegerValue(count), d, nil
	}
	return
}

// possiblyExpireHook will evaluate a hook by it's name for expiration and
// purge it from the database if needed. This operation is called from an
// independent goroutine
func (c *Server) possiblyExpireHook(name string) {
	c.mu.Lock()
	if h, ok := c.hooks[name]; ok {
		if !h.expires.IsZero() && time.Now().After(h.expires) {
			// purge from database
			msg := &Message{}
			if h.channel {
				msg.Args = []string{"delchan", h.Name}
			} else {
				msg.Args = []string{"delhook", h.Name}
			}
			_, d, err := c.cmdDelHook(msg, h.channel)
			if err != nil {
				c.mu.Unlock()
				panic(err)
			}
			if err := c.writeAOF(msg.Args, &d); err != nil {
				c.mu.Unlock()
				panic(err)
			}
			log.Debugf("purged hook %v", h.Name)
		}
	}
	c.mu.Unlock()
}

func (c *Server) cmdHooks(msg *Message, channel bool) (
	res resp.Value, err error,
) {
	start := time.Now()
	vs := msg.Args[1:]

	var pattern string
	var ok bool

	if vs, pattern, ok = tokenval(vs); !ok || pattern == "" {
		return NOMessage, errInvalidNumberOfArguments
	}
	if len(vs) != 0 {
		return NOMessage, errInvalidNumberOfArguments
	}

	var hooks []*Hook
	for name, hook := range c.hooks {
		if hook.channel != channel {
			continue
		}
		match, _ := glob.Match(pattern, name)
		if match {
			hooks = append(hooks, hook)
		}
	}
	sort.Sort(hooksByName(hooks))

	switch msg.OutputType {
	case JSON:
		buf := &bytes.Buffer{}
		buf.WriteString(`{"ok":true,`)
		if channel {
			buf.WriteString(`"chans":[`)
		} else {
			buf.WriteString(`"hooks":[`)
		}
		for i, hook := range hooks {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(`{`)
			buf.WriteString(`"name":` + jsonString(hook.Name))
			buf.WriteString(`,"key":` + jsonString(hook.Key))
			if !channel {
				buf.WriteString(`,"endpoints":[`)
				for i, endpoint := range hook.Endpoints {
					if i > 0 {
						buf.WriteByte(',')
					}
					buf.WriteString(jsonString(endpoint))
				}
			}
			buf.WriteString(`],"command":[`)
			for i, v := range hook.Message.Args {
				if i > 0 {
					buf.WriteString(`,`)
				}
				buf.WriteString(jsonString(v))
			}
			buf.WriteString(`],"meta":{`)
			for i, meta := range hook.Metas {
				if i > 0 {
					buf.WriteString(`,`)
				}
				buf.WriteString(jsonString(meta.Name))
				buf.WriteString(`:`)
				buf.WriteString(jsonString(meta.Value))
			}
			buf.WriteString(`}}`)
		}
		buf.WriteString(`],"elapsed":"` +
			time.Now().Sub(start).String() + "\"}")
		return resp.StringValue(buf.String()), nil
	case RESP:
		var vals []resp.Value
		for _, hook := range hooks {
			var hvals []resp.Value
			hvals = append(hvals, resp.StringValue(hook.Name))
			hvals = append(hvals, resp.StringValue(hook.Key))
			var evals []resp.Value
			for _, endpoint := range hook.Endpoints {
				evals = append(evals, resp.StringValue(endpoint))
			}
			hvals = append(hvals, resp.ArrayValue(evals))
			avals := make([]resp.Value, len(hook.Message.Args))
			for i := 0; i < len(hook.Message.Args); i++ {
				avals[i] = resp.StringValue(hook.Message.Args[i])
			}
			hvals = append(hvals, resp.ArrayValue(avals))
			var metas []resp.Value
			for _, meta := range hook.Metas {
				metas = append(metas, resp.StringValue(meta.Name))
				metas = append(metas, resp.StringValue(meta.Value))
			}
			hvals = append(hvals, resp.ArrayValue(metas))
			vals = append(vals, resp.ArrayValue(hvals))
		}
		return resp.ArrayValue(vals), nil
	}
	return resp.SimpleStringValue(""), nil
}

// Hook represents a hook.
type Hook struct {
	cond       *sync.Cond
	Key        string
	Name       string
	Endpoints  []string
	Message    *Message
	Fence      *liveFenceSwitches
	ScanWriter *scanWriter
	Metas      []FenceMeta
	db         *buntdb.DB
	channel    bool
	closed     bool
	opened     bool
	query      string
	epm        *endpoint.Manager
	expires    time.Time
}

// Expires returns when the hook expires. Required by the expire.Item interface.
func (h *Hook) Expires() time.Time {
	return h.expires
}

// Equals returns true if two hooks are equal
func (h *Hook) Equals(hook *Hook) bool {
	if h.Key != hook.Key ||
		h.Name != hook.Name ||
		len(h.Endpoints) != len(hook.Endpoints) ||
		len(h.Metas) != len(hook.Metas) {
		return false
	}
	if !h.expires.Equal(hook.expires) {
		return false
	}
	for i, endpoint := range h.Endpoints {
		if endpoint != hook.Endpoints[i] {
			return false
		}
	}
	for i, meta := range h.Metas {
		if meta.Name != hook.Metas[i].Name ||
			meta.Value != hook.Metas[i].Value {
			return false
		}
	}
	if len(h.Message.Args) != len(hook.Message.Args) {
		return false
	}
	for i := 0; i < len(h.Message.Args); i++ {
		if h.Message.Args[i] != hook.Message.Args[i] {
			return false
		}
	}
	return true
}

// FenceMeta is a meta key/value pair for fences
type FenceMeta struct {
	Name, Value string
}

type hookMetaByName []FenceMeta

func (arr hookMetaByName) Len() int {
	return len(arr)
}

func (arr hookMetaByName) Less(a, b int) bool {
	return arr[a].Name < arr[b].Name
}

func (arr hookMetaByName) Swap(a, b int) {
	arr[a], arr[b] = arr[b], arr[a]
}

// Open is called when a hook is first created. It calls the manager
// function in a goroutine
func (h *Hook) Open() {
	if h.channel {
		// nothing to open for channels
		return
	}
	h.cond.L.Lock()
	defer h.cond.L.Unlock()
	if h.opened {
		return
	}
	h.opened = true
	h.query = `{"hook":` + jsonString(h.Name) + `}`
	go h.manager()
}

// Close closed the hook and stop the manager function
func (h *Hook) Close() {
	if h.channel {
		// nothing to close for channels
		return
	}
	h.cond.L.Lock()
	defer h.cond.L.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	h.cond.Broadcast()
}

// Signal can be called at any point to wake up the hook and
// notify the manager that there may be something new in the queue.
func (h *Hook) Signal() {
	if h.channel {
		// nothing to signal for channels
		return
	}
	h.cond.L.Lock()
	h.cond.Broadcast()
	h.cond.L.Unlock()
}

// the manager is a forever loop that calls proc whenever there's a signal.
// it ends when the "closed" flag is set.
func (h *Hook) manager() {
	for {
		h.cond.L.Lock()
		for {
			if h.closed {
				h.cond.L.Unlock()
				return
			}
			if h.proc() {
				break
			}
			h.cond.L.Unlock()
			// proc failed. wait half a second and try again
			time.Sleep(time.Second / 2)
			h.cond.L.Lock()
		}
		h.cond.Wait()
		h.cond.L.Unlock()
	}
}

// proc processes queued hook logs.
// returning true will indicate that all log entries have been
// successfully handled.
func (h *Hook) proc() (ok bool) {
	var keys, vals []string
	var ttls []time.Duration
	start := time.Now()
	err := h.db.Update(func(tx *buntdb.Tx) error {
		// get keys and vals
		err := tx.AscendGreaterOrEqual("hooks",
			h.query, func(key, val string) bool {
				if strings.HasPrefix(key, hookLogPrefix) {
					keys = append(keys, key)
					vals = append(vals, val)
				}
				return true
			},
		)
		if err != nil {
			return err
		}

		// delete the keys
		for _, key := range keys {
			ttl, err := tx.TTL(key)
			if err != nil {
				if err != buntdb.ErrNotFound {
					return err
				}
			}
			ttls = append(ttls, ttl)
			_, err = tx.Delete(key)
			if err != nil {
				if err != buntdb.ErrNotFound {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		log.Error(err)
		return false
	}

	// send each val. on failure reinsert that one and all of the following
	for i, key := range keys {
		val := vals[i]
		idx := stringToUint64(key[len(hookLogPrefix):])
		var sent bool
		for _, endpoint := range h.Endpoints {
			err := h.epm.Send(endpoint, val)
			if err != nil {
				log.Debugf("Endpoint connect/send error: %v: %v: %v",
					idx, endpoint, err)
				continue
			}
			log.Debugf("Endpoint send ok: %v: %v: %v", idx, endpoint, err)
			sent = true
			break
		}
		if !sent {
			// failed to send. try to reinsert the remaining.
			// if this fails we lose log entries.
			keys = keys[i:]
			vals = vals[i:]
			ttls = ttls[i:]
			h.db.Update(func(tx *buntdb.Tx) error {
				for i, key := range keys {
					val := vals[i]
					ttl := ttls[i] - time.Since(start)
					if ttl > 0 {
						opts := &buntdb.SetOptions{
							Expires: true,
							TTL:     ttl,
						}
						_, _, err := tx.Set(key, val, opts)
						if err != nil {
							return err
						}
					}
				}
				return nil
			})
			return false
		}
	}
	return true
}
