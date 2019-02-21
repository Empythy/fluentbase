package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/resp"
	"github.com/gvsafronov/fluentbase/core"
	"github.com/gvsafronov/fluentbase/internal/collection"
)

func (c *Server) cmdStats(msg *Message) (res resp.Value, err error) {
	start := time.Now()
	vs := msg.Args[1:]
	var ms = []map[string]interface{}{}

	if len(vs) == 0 {
		return NOMessage, errInvalidNumberOfArguments
	}
	var vals []resp.Value
	var key string
	var ok bool
	for {
		vs, key, ok = tokenval(vs)
		if !ok {
			break
		}
		col := c.getCol(key)
		if col != nil {
			m := make(map[string]interface{})
			m["num_points"] = col.PointCount()
			m["in_memory_size"] = col.TotalWeight()
			m["num_objects"] = col.Count()
			m["num_strings"] = col.StringCount()
			switch msg.OutputType {
			case JSON:
				ms = append(ms, m)
			case RESP:
				vals = append(vals, resp.ArrayValue(respValuesSimpleMap(m)))
			}
		} else {
			switch msg.OutputType {
			case JSON:
				ms = append(ms, nil)
			case RESP:
				vals = append(vals, resp.NullValue())
			}
		}
	}
	switch msg.OutputType {
	case JSON:

		data, err := json.Marshal(ms)
		if err != nil {
			return NOMessage, err
		}
		res = resp.StringValue(`{"ok":true,"stats":` + string(data) + `,"elapsed":"` + time.Now().Sub(start).String() + "\"}")
	case RESP:
		res = resp.ArrayValue(vals)
	}
	return res, nil
}

func (c *Server) cmdServer(msg *Message) (res resp.Value, err error) {
	start := time.Now()
	m := make(map[string]interface{})
	args := msg.Args[1:]

	// Switch on the type of stats requested
	switch len(args) {
	case 0:
		c.basicStats(m)
	case 1:
		if strings.ToLower(args[0]) == "ext" {
			c.extStats(m)
		}
	default:
		return NOMessage, errInvalidNumberOfArguments
	}

	switch msg.OutputType {
	case JSON:
		data, err := json.Marshal(m)
		if err != nil {
			return NOMessage, err
		}
		res = resp.StringValue(`{"ok":true,"stats":` + string(data) + `,"elapsed":"` + time.Since(start).String() + "\"}")
	case RESP:
		vals := respValuesSimpleMap(m)
		res = resp.ArrayValue(vals)
	}
	return res, nil
}

// basicStats populates the passed map with basic system/go/tile38 statistics
func (c *Server) basicStats(m map[string]interface{}) {
	m["id"] = c.config.serverID()
	if c.config.followHost() != "" {
		m["following"] = fmt.Sprintf("%s:%d", c.config.followHost(),
			c.config.followPort())
		m["caught_up"] = c.fcup
		m["caught_up_once"] = c.fcuponce
	}
	m["http_transport"] = c.http
	m["pid"] = os.Getpid()
	m["aof_size"] = c.aofsz
	m["num_collections"] = c.cols.Len()
	m["num_hooks"] = len(c.hooks)
	sz := 0
	c.cols.Scan(func(key string, value interface{}) bool {
		col := value.(*collection.Collection)
		sz += col.TotalWeight()
		return true
	})
	m["in_memory_size"] = sz
	points := 0
	objects := 0
	strings := 0
	c.cols.Scan(func(key string, value interface{}) bool {
		col := value.(*collection.Collection)
		points += col.PointCount()
		objects += col.Count()
		strings += col.StringCount()
		return true
	})
	m["num_points"] = points
	m["num_objects"] = objects
	m["num_strings"] = strings
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	avgsz := 0
	if points != 0 {
		avgsz = int(mem.HeapAlloc) / points
	}
	m["mem_alloc"] = mem.Alloc
	m["heap_size"] = mem.HeapAlloc
	m["heap_released"] = mem.HeapReleased
	m["max_heap_size"] = c.config.maxMemory()
	m["avg_item_size"] = avgsz
	m["version"] = core.Version
	m["pointer_size"] = (32 << uintptr(uint64(^uintptr(0))>>63)) / 8
	m["read_only"] = c.config.readOnly()
	m["cpus"] = runtime.NumCPU()
	n, _ := runtime.ThreadCreateProfile(nil)
	m["threads"] = float64(n)
}

// extStats populates the passed map with extended system/go/tile38 statistics
func (c *Server) extStats(m map[string]interface{}) {
	var mem runtime.MemStats
	n, _ := runtime.ThreadCreateProfile(nil)
	runtime.ReadMemStats(&mem)

	// Go/Memory Stats

	// Number of goroutines that currently exist
	m["go_goroutines"] = runtime.NumGoroutine()
	// Number of OS threads created
	m["go_threads"] = float64(n)
	// A summary of the GC invocation durations
	m["go_version"] = runtime.Version()
	// Number of bytes allocated and still in use
	m["alloc_bytes"] = mem.Alloc
	// Total number of bytes allocated, even if freed
	m["alloc_bytes_total"] = mem.TotalAlloc
	// Number of CPUS available on the system
	m["sys_cpus"] = runtime.NumCPU()
	// Number of bytes obtained from system
	m["sys_bytes"] = mem.Sys
	// Total number of pointer lookups
	m["lookups_total"] = mem.Lookups
	// Total number of mallocs
	m["mallocs_total"] = mem.Mallocs
	// Total number of frees
	m["frees_total"] = mem.Frees
	// Number of heap bytes allocated and still in use
	m["heap_alloc_bytes"] = mem.HeapAlloc
	// Number of heap bytes obtained from system
	m["heap_sys_bytes"] = mem.HeapSys
	// Number of heap bytes waiting to be used
	m["heap_idle_bytes"] = mem.HeapIdle
	// Number of heap bytes that are in use
	m["heap_inuse_bytes"] = mem.HeapInuse
	// Number of heap bytes released to OS
	m["heap_released_bytes"] = mem.HeapReleased
	// Number of allocated objects
	m["heap_objects"] = mem.HeapObjects
	// Number of bytes in use by the stack allocator
	m["stack_inuse_bytes"] = mem.StackInuse
	// Number of bytes obtained from system for stack allocator
	m["stack_sys_bytes"] = mem.StackSys
	// Number of bytes in use by mspan structures
	m["mspan_inuse_bytes"] = mem.MSpanInuse
	// Number of bytes used for mspan structures obtained from system
	m["mspan_sys_bytes"] = mem.MSpanSys
	// Number of bytes in use by mcache structures
	m["mcache_inuse_bytes"] = mem.MCacheInuse
	// Number of bytes used for mcache structures obtained from system
	m["mcache_sys_bytes"] = mem.MCacheSys
	// Number of bytes used by the profiling bucket hash table
	m["buck_hash_sys_bytes"] = mem.BuckHashSys
	// Number of bytes used for garbage collection system metadata
	m["gc_sys_bytes"] = mem.GCSys
	// Number of bytes used for other system allocations
	m["other_sys_bytes"] = mem.OtherSys
	// Number of heap bytes when next garbage collection will take place
	m["next_gc_bytes"] = mem.NextGC
	// Number of seconds since 1970 of last garbage collection
	m["last_gc_time_seconds"] = float64(mem.LastGC) / 1e9
	// The fraction of this program's available CPU time used by the GC since
	// the program started
	m["gc_cpu_fraction"] = mem.GCCPUFraction

	// Tile38 Stats

	// ID of the server
	m["tile38_id"] = c.config.serverID()
	// The process ID of the server
	m["tile38_pid"] = os.Getpid()
	// Version of Tile38 running
	m["tile38_version"] = core.Version
	// Maximum heap size allowed
	m["tile38_max_heap_size"] = c.config.maxMemory()
	// Type of instance running
	if c.config.followHost() == "" {
		m["tile38_type"] = "leader"
	} else {
		m["tile38_type"] = "follower"
	}
	// Whether or not the server is read-only
	m["tile38_read_only"] = c.config.readOnly()
	// Size of pointer
	m["tile38_pointer_size"] = (32 << uintptr(uint64(^uintptr(0))>>63)) / 8
	// Uptime of the Tile38 server in seconds
	m["tile38_uptime_in_seconds"] = time.Since(c.started).Seconds()
	// Number of currently connected Tile38 clients
	c.connsmu.RLock()
	m["tile38_connected_clients"] = len(c.conns)
	c.connsmu.RUnlock()
	// Whether or not a cluster is enabled
	m["tile38_cluster_enabled"] = false
	// Whether or not the Tile38 AOF is enabled
	m["tile38_aof_enabled"] = core.AppendOnly
	// Whether or not an AOF shrink is currently in progress
	m["tile38_aof_rewrite_in_progress"] = c.shrinking
	// Length of time the last AOF shrink took
	m["tile38_aof_last_rewrite_time_sec"] = c.lastShrinkDuration.get() / int(time.Second)
	// Duration of the on-going AOF rewrite operation if any
	var currentShrinkStart time.Time
	if currentShrinkStart.IsZero() {
		m["tile38_aof_current_rewrite_time_sec"] = 0
	} else {
		m["tile38_aof_current_rewrite_time_sec"] = time.Since(currentShrinkStart).Seconds()
	}
	// Total size of the AOF in bytes
	m["tile38_aof_size"] = c.aofsz
	// Whether or no the HTTP transport is being served
	m["tile38_http_transport"] = c.http
	// Number of connections accepted by the server
	m["tile38_total_connections_received"] = c.statsTotalConns.get()
	// Number of commands processed by the server
	m["tile38_total_commands_processed"] = c.statsTotalCommands.get()
	// Number of key expiration events
	m["tile38_expired_keys"] = c.statsExpired.get()
	// Number of connected slaves
	m["tile38_connected_slaves"] = len(c.aofconnM)

	points := 0
	objects := 0
	strings := 0
	c.cols.Scan(func(key string, value interface{}) bool {
		col := value.(*collection.Collection)
		points += col.PointCount()
		objects += col.Count()
		strings += col.StringCount()
		return true
	})

	// Number of points in the database
	m["tile38_num_points"] = points
	// Number of objects in the database
	m["tile38_num_objects"] = objects
	// Number of string in the database
	m["tile38_num_strings"] = strings
	// Number of collections in the database
	m["tile38_num_collections"] = c.cols.Len()
	// Number of hooks in the database
	m["tile38_num_hooks"] = len(c.hooks)

	avgsz := 0
	if points != 0 {
		avgsz = int(mem.HeapAlloc) / points
	}

	// Average point size in bytes
	m["tile38_avg_point_size"] = avgsz

	sz := 0
	c.cols.Scan(func(key string, value interface{}) bool {
		col := value.(*collection.Collection)
		sz += col.TotalWeight()
		return true
	})

	// Total in memory size of all collections
	m["tile38_in_memory_size"] = sz
}

func (c *Server) writeInfoServer(w *bytes.Buffer) {
	fmt.Fprintf(w, "tile38_version:%s\r\n", core.Version)
	fmt.Fprintf(w, "redis_version:%s\r\n", core.Version)                             // Version of the Redis server
	fmt.Fprintf(w, "uptime_in_seconds:%d\r\n", int(time.Since(c.started).Seconds())) // Number of seconds since Redis server start
}
func (c *Server) writeInfoClients(w *bytes.Buffer) {
	c.connsmu.RLock()
	fmt.Fprintf(w, "connected_clients:%d\r\n", len(c.conns)) // Number of client connections (excluding connections from slaves)
	c.connsmu.RUnlock()
}
func (c *Server) writeInfoMemory(w *bytes.Buffer) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	fmt.Fprintf(w, "used_memory:%d\r\n", mem.Alloc) // total number of bytes allocated by Redis using its allocator (either standard libc, jemalloc, or an alternative allocator such as tcmalloc
}
func boolInt(t bool) int {
	if t {
		return 1
	}
	return 0
}
func (c *Server) writeInfoPersistence(w *bytes.Buffer) {
	fmt.Fprintf(w, "aof_enabled:%d\r\n", boolInt(core.AppendOnly))
	fmt.Fprintf(w, "aof_rewrite_in_progress:%d\r\n", boolInt(c.shrinking))                          // Flag indicating a AOF rewrite operation is on-going
	fmt.Fprintf(w, "aof_last_rewrite_time_sec:%d\r\n", c.lastShrinkDuration.get()/int(time.Second)) // Duration of the last AOF rewrite operation in seconds

	var currentShrinkStart time.Time // c.currentShrinkStart.get()
	if currentShrinkStart.IsZero() {
		fmt.Fprintf(w, "aof_current_rewrite_time_sec:0\r\n") // Duration of the on-going AOF rewrite operation if any
	} else {
		fmt.Fprintf(w, "aof_current_rewrite_time_sec:%d\r\n", time.Now().Sub(currentShrinkStart)/time.Second) // Duration of the on-going AOF rewrite operation if any
	}
}

func (c *Server) writeInfoStats(w *bytes.Buffer) {
	fmt.Fprintf(w, "total_connections_received:%d\r\n", c.statsTotalConns.get())  // Total number of connections accepted by the server
	fmt.Fprintf(w, "total_commands_processed:%d\r\n", c.statsTotalCommands.get()) // Total number of commands processed by the server
	fmt.Fprintf(w, "expired_keys:%d\r\n", c.statsExpired.get())                   // Total number of key expiration events
}

// writeInfoReplication writes all replication data to the 'info' response
func (c *Server) writeInfoReplication(w *bytes.Buffer) {
	if c.config.followHost() != "" {
		fmt.Fprintf(w, "role:slave\r\n")
		fmt.Fprintf(w, "master_host:%s\r\n", c.config.followHost())
		fmt.Fprintf(w, "master_port:%v\r\n", c.config.followPort())
	} else {
		fmt.Fprintf(w, "role:master\r\n")
		var i int
		for _, cc := range c.conns {
			if cc.replPort != 0 {
				fmt.Fprintf(w, "slave%v:ip=%s,port=%v,state=online\r\n", i,
					strings.Split(cc.remoteAddr, ":")[0], cc.replPort)
				i++
			}
		}
	}
	fmt.Fprintf(w, "connected_slaves:%d\r\n", len(c.aofconnM)) // Number of connected slaves
}

func (c *Server) writeInfoCluster(w *bytes.Buffer) {
	fmt.Fprintf(w, "cluster_enabled:0\r\n")
}

func (c *Server) cmdInfo(msg *Message) (res resp.Value, err error) {
	start := time.Now()

	sections := []string{"server", "clients", "memory", "persistence", "stats", "replication", "cpu", "cluster", "keyspace"}
	switch len(msg.Args) {
	default:
		return NOMessage, errInvalidNumberOfArguments
	case 1:
	case 2:
		section := strings.ToLower(msg.Args[1])
		switch section {
		default:
			sections = []string{section}
		case "all":
			sections = []string{"server", "clients", "memory", "persistence", "stats", "replication", "cpu", "commandstats", "cluster", "keyspace"}
		case "default":
		}
	}

	w := &bytes.Buffer{}
	for i, section := range sections {
		if i > 0 {
			w.WriteString("\r\n")
		}
		switch strings.ToLower(section) {
		default:
			continue
		case "server":
			w.WriteString("# Server\r\n")
			c.writeInfoServer(w)
		case "clients":
			w.WriteString("# Clients\r\n")
			c.writeInfoClients(w)
		case "memory":
			w.WriteString("# Memory\r\n")
			c.writeInfoMemory(w)
		case "persistence":
			w.WriteString("# Persistence\r\n")
			c.writeInfoPersistence(w)
		case "stats":
			w.WriteString("# Stats\r\n")
			c.writeInfoStats(w)
		case "replication":
			w.WriteString("# Replication\r\n")
			c.writeInfoReplication(w)
		case "cpu":
			w.WriteString("# CPU\r\n")
			c.writeInfoCPU(w)
		case "cluster":
			w.WriteString("# Cluster\r\n")
			c.writeInfoCluster(w)
		}
	}

	switch msg.OutputType {
	case JSON:
		// Create a map of all key/value info fields
		m := make(map[string]interface{})
		for _, kv := range strings.Split(w.String(), "\r\n") {
			kv = strings.TrimSpace(kv)
			if !strings.HasPrefix(kv, "#") {
				if split := strings.SplitN(kv, ":", 2); len(split) == 2 {
					m[split[0]] = tryParseType(split[1])
				}
			}
		}

		// Marshal the map and use the output in the JSON response
		data, err := json.Marshal(m)
		if err != nil {
			return NOMessage, err
		}
		res = resp.StringValue(`{"ok":true,"info":` + string(data) + `,"elapsed":"` + time.Now().Sub(start).String() + "\"}")
	case RESP:
		res = resp.BytesValue(w.Bytes())
	}
	return res, nil
}

// tryParseType attempts to parse the passed string as an integer, float64 and
// a bool returning any successful parsed values. It returns the passed string
// if all tries fail
func tryParseType(str string) interface{} {
	if v, err := strconv.ParseInt(str, 10, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseFloat(str, 64); err == nil {
		return v
	}
	if v, err := strconv.ParseBool(str); err == nil {
		return v
	}
	return str
}

func respValuesSimpleMap(m map[string]interface{}) []resp.Value {
	var keys []string
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var vals []resp.Value
	for _, key := range keys {
		val := m[key]
		vals = append(vals, resp.StringValue(key))
		vals = append(vals, resp.StringValue(fmt.Sprintf("%v", val)))
	}
	return vals
}

func (c *Server) statsCollections(line string) (string, error) {
	start := time.Now()
	var key string
	var ms = []map[string]interface{}{}
	for len(line) > 0 {
		line, key = token(line)
		col := c.getCol(key)
		if col != nil {
			m := make(map[string]interface{})
			points := col.PointCount()
			m["num_points"] = points
			m["in_memory_size"] = col.TotalWeight()
			m["num_objects"] = col.Count()
			ms = append(ms, m)
		} else {
			ms = append(ms, nil)
		}
	}
	data, err := json.Marshal(ms)
	if err != nil {
		return "", err
	}
	return `{"ok":true,"stats":` + string(data) + `,"elapsed":"` + time.Now().Sub(start).String() + "\"}", nil
}
