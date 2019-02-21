package server

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/resp"
	"github.com/gvsafronov/fluentbase/core"
	"github.com/gvsafronov/fluentbase/internal/log"
)

var errNoLongerFollowing = errors.New("no longer following")

const checksumsz = 512 * 1024

func (c *Server) cmdFollow(msg *Message) (res resp.Value, err error) {
	start := time.Now()
	vs := msg.Args[1:]
	var ok bool
	var host, sport string

	if vs, host, ok = tokenval(vs); !ok || host == "" {
		return NOMessage, errInvalidNumberOfArguments
	}
	if vs, sport, ok = tokenval(vs); !ok || sport == "" {
		return NOMessage, errInvalidNumberOfArguments
	}
	if len(vs) != 0 {
		return NOMessage, errInvalidNumberOfArguments
	}
	host = strings.ToLower(host)
	sport = strings.ToLower(sport)
	var update bool
	if host == "no" && sport == "one" {
		update = c.config.followHost() != "" || c.config.followPort() != 0
		c.config.setFollowHost("")
		c.config.setFollowPort(0)
	} else {
		n, err := strconv.ParseUint(sport, 10, 64)
		if err != nil {
			return NOMessage, errInvalidArgument(sport)
		}
		port := int(n)
		update = c.config.followHost() != host || c.config.followPort() != port
		auth := c.config.leaderAuth()
		if update {
			c.mu.Unlock()
			conn, err := DialTimeout(fmt.Sprintf("%s:%d", host, port), time.Second*2)
			if err != nil {
				c.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow: %v", err)
			}
			defer conn.Close()
			if auth != "" {
				if err := c.followDoLeaderAuth(conn, auth); err != nil {
					return NOMessage, fmt.Errorf("cannot follow: %v", err)
				}
			}
			m, err := doServer(conn)
			if err != nil {
				c.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow: %v", err)
			}
			if m["id"] == "" {
				c.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow: invalid id")
			}
			if m["id"] == c.config.serverID() {
				c.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow self")
			}
			if m["following"] != "" {
				c.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow a follower")
			}
			c.mu.Lock()
		}
		c.config.setFollowHost(host)
		c.config.setFollowPort(port)
	}
	c.config.write(false)
	if update {
		c.followc.add(1)
		if c.config.followHost() != "" {
			log.Infof("following new host '%s' '%s'.", host, sport)
			go c.follow(c.config.followHost(), c.config.followPort(), c.followc.get())
		} else {
			log.Infof("following no one")
		}
	}
	return OKMessage(msg, start), nil
}

// cmdReplConf is a command handler that sets replication configuration info
func (c *Server) cmdReplConf(msg *Message, client *Client) (res resp.Value, err error) {
	start := time.Now()
	vs := msg.Args[1:]
	var ok bool
	var cmd, val string

	// Parse the message
	if vs, cmd, ok = tokenval(vs); !ok || cmd == "" {
		return NOMessage, errInvalidNumberOfArguments
	}
	if vs, val, ok = tokenval(vs); !ok || val == "" {
		return NOMessage, errInvalidNumberOfArguments
	}

	// Switch on the command received
	switch cmd {
	case "listening-port":
		// Parse the port as an integer
		port, err := strconv.Atoi(val)
		if err != nil {
			return NOMessage, errInvalidArgument(val)
		}

		// Apply the replication port to the client and return
		for _, c := range c.conns {
			if c.remoteAddr == client.remoteAddr {
				c.replPort = port
				return OKMessage(msg, start), nil
			}
		}
	}
	return NOMessage, fmt.Errorf("cannot find follower")
}

func doServer(conn *RESPConn) (map[string]string, error) {
	v, err := conn.Do("server")
	if err != nil {
		return nil, err
	}
	if v.Error() != nil {
		return nil, v.Error()
	}
	arr := v.Array()
	m := make(map[string]string)
	for i := 0; i < len(arr)/2; i++ {
		m[arr[i*2+0].String()] = arr[i*2+1].String()
	}
	return m, err
}

func (c *Server) followHandleCommand(args []string, followc int, w io.Writer) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.followc.get() != followc {
		return c.aofsz, errNoLongerFollowing
	}
	msg := &Message{Args: args}

	_, d, err := c.command(msg, nil)
	if err != nil {
		if commandErrIsFatal(err) {
			return c.aofsz, err
		}
	}
	if err := c.writeAOF(args, &d); err != nil {
		return c.aofsz, err
	}
	if len(c.aofbuf) > 10240 {
		c.flushAOF()
	}
	return c.aofsz, nil
}

func (c *Server) followDoLeaderAuth(conn *RESPConn, auth string) error {
	v, err := conn.Do("auth", auth)
	if err != nil {
		return err
	}
	if v.Error() != nil {
		return v.Error()
	}
	if v.String() != "OK" {
		return errors.New("cannot follow: auth no ok")
	}
	return nil
}

func (c *Server) followStep(host string, port int, followc int) error {
	if c.followc.get() != followc {
		return errNoLongerFollowing
	}
	c.mu.Lock()
	c.fcup = false
	auth := c.config.leaderAuth()
	c.mu.Unlock()
	addr := fmt.Sprintf("%s:%d", host, port)

	// check if we are following self
	conn, err := DialTimeout(addr, time.Second*2)
	if err != nil {
		return fmt.Errorf("cannot follow: %v", err)
	}
	defer conn.Close()
	if auth != "" {
		if err := c.followDoLeaderAuth(conn, auth); err != nil {
			return fmt.Errorf("cannot follow: %v", err)
		}
	}
	m, err := doServer(conn)
	if err != nil {
		return fmt.Errorf("cannot follow: %v", err)
	}

	if m["id"] == "" {
		return fmt.Errorf("cannot follow: invalid id")
	}
	if m["id"] == c.config.serverID() {
		return fmt.Errorf("cannot follow self")
	}
	if m["following"] != "" {
		return fmt.Errorf("cannot follow a follower")
	}

	// verify checksum
	pos, err := c.followCheckSome(addr, followc)
	if err != nil {
		return err
	}

	// Send the replication port to the leader
	v, err := conn.Do("replconf", "listening-port", c.port)
	if err != nil {
		return err
	}
	if v.Error() != nil {
		return v.Error()
	}
	if v.String() != "OK" {
		return errors.New("invalid response to replconf request")
	}
	if core.ShowDebugMessages {
		log.Debug("follow:", addr, ":replconf")
	}

	v, err = conn.Do("aof", pos)
	if err != nil {
		return err
	}
	if v.Error() != nil {
		return v.Error()
	}
	if v.String() != "OK" {
		return errors.New("invalid response to aof live request")
	}
	if core.ShowDebugMessages {
		log.Debug("follow:", addr, ":read aof")
	}

	aofSize, err := strconv.ParseInt(m["aof_size"], 10, 64)
	if err != nil {
		return err
	}

	caughtUp := pos >= aofSize
	if caughtUp {
		c.mu.Lock()
		c.fcup = true
		c.fcuponce = true
		c.mu.Unlock()
		log.Info("caught up")
	}
	nullw := ioutil.Discard
	for {
		v, telnet, _, err := conn.rd.ReadMultiBulk()
		if err != nil {
			return err
		}
		vals := v.Array()
		if telnet || v.Type() != resp.Array {
			return errors.New("invalid multibulk")
		}
		svals := make([]string, len(vals))
		for i := 0; i < len(vals); i++ {
			svals[i] = vals[i].String()
		}

		aofsz, err := c.followHandleCommand(svals, followc, nullw)
		if err != nil {
			return err
		}
		if !caughtUp {
			if aofsz >= int(aofSize) {
				caughtUp = true
				c.mu.Lock()
				c.flushAOF()
				c.fcup = true
				c.fcuponce = true
				c.mu.Unlock()
				log.Info("caught up")
			}
		}

	}
}

func (c *Server) follow(host string, port int, followc int) {
	for {
		err := c.followStep(host, port, followc)
		if err == errNoLongerFollowing {
			return
		}
		if err != nil && err != io.EOF {
			log.Error("follow: " + err.Error())
		}
		time.Sleep(time.Second)
	}
}
