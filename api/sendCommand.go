package api

import (
	"errors"
	"fmt"
	"github.com/dsorm/yeelight2mqtt/console"
	"net"
	"strings"
	"time"
)

// for debug only, temporary
var successfulSends uint64
var unsuccessfulSends uint64

type sendCommandCtx struct {
	tries    uint64
	maxTries uint64
	command  string
	l        *Light
}

func (l *Light) SendCommand(command string, maxTries int) (response []byte, err error) {
	ctx := &sendCommandCtx{
		maxTries: uint64(maxTries),
		command:  command,
		l:        l,
	}
	return ctx.sendCommandWithCtx()
}

// this code is trash, and it probably doesn't even work properly
func (ctx *sendCommandCtx) sendCommandWithCtx() (response []byte, err error) {
	ctx.tries++
	if ctx.tries >= ctx.maxTries {
		return nil, errors.New("max tries exceeded")
	}

	ctx.l.connMutex.Lock()

	// a very dirty way to ratelimit the number of requests to the Yeelight, but it works :)
	done := make(chan bool)
	unlockMutex := func() {
		done <- true
		time.Sleep(time.Second / 2)
		ctx.l.connMutex.Unlock()
	}

	// this function serves as a force-unlock in case everything goes wrong (it will, trust me)
	go func() {
		timeoutSeconds := 40
		ticker := time.NewTicker(time.Second * time.Duration(timeoutSeconds))

		select {
		case <-ticker.C:
			console.Logf("sendCommand was stuck for %v seconds, force-unlocking mutex and closing connection\n", timeoutSeconds)
			err = ctx.l.conn.Close()
			if err != nil {
				console.Logf("error closing net connection: %v\n", err)
			}

			unlockMutex()
			return
		case <-done:
			ticker.Stop()
			return
		}

	}()

	if ctx.l.conn == nil {
		ctx.l.conn, err = net.DialTimeout("tcp", fmt.Sprintf("%s:55443", ctx.l.Host), 5*time.Second)
		if err != nil {
			unlockMutex()
			return ctx.sendCommandWithCtx()
		}
	}

	// wait a while before writing command, since yeelights are a bit slow,
	// they might write the output of last command to this command or just crap out
	time.Sleep(time.Second / 4)

	// if ctx.command is empty, just read from the connection
	if ctx.command != "" {
		_, err = ctx.l.conn.Write([]byte(ctx.command + "\r\n"))
		if err != nil {
			err = ctx.l.conn.Close()
			if err != nil {
				console.Logf("error closing net connection: %v\n", err)
			}
			ctx.l.conn = nil
			unlockMutex()
			ctx.tries++
			return ctx.sendCommandWithCtx()
		}

	}

	err = ctx.l.conn.SetDeadline(time.Now().Add(time.Second * 5))
	if err != nil {
		err = ctx.l.conn.Close()
		if err != nil {
			console.Logf("error closing net connection: %v\n", err)
		}
		ctx.l.conn = nil
		unlockMutex()
		return ctx.sendCommandWithCtx()
	}

	resp := []byte{}
	// read a line from response
	for {
		buf := make([]byte, 1024)
		n, err := ctx.l.conn.Read(buf)
		if err != nil {
			reason := "unknown reason"
			if strings.Contains(err.Error(), "timeout") {
				reason = "timeout"
			}
			console.Logf("sendCommand() to %v failed due to %v, retrying... (%v/%v)\n", ctx.l.Host, reason, ctx.tries, ctx.maxTries)

			unsuccessfulSends++
			unlockMutex()
			return ctx.sendCommandWithCtx()
		}
		resp = append(resp, buf[:n]...)
		if string(resp[len(resp)-2:]) == "\r\n" {
			successfulSends++
			break
		}
	}

	// detect if the yeelight is vomiting the (currently) unnecessary output about prop change
	if strings.HasPrefix(string(resp), "{\"method\":\"props\",\"params\":{") {
		// TODO make it work
		// ctx.l.refreshMsg <- string(resp)

		// read again to get the requested response
		ctx.tries -= 2
		ctx.command = ""
		unlockMutex()
		return ctx.sendCommandWithCtx()
	}

	go unlockMutex()
	return resp, err
}
