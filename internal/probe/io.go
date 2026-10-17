package probe

import (
	"context"
	"net"
	"time"
)

// 每次 socket I/O 都重新計時，不把持續有資料的請求截在十秒。
type idleConn struct {
	net.Conn
	ctx     context.Context
	timeout time.Duration
	stop    func() bool
}

func guardIO(ctx context.Context, conn net.Conn, seconds int) net.Conn {
	c := &idleConn{Conn: conn, ctx: ctx, timeout: time.Duration(seconds) * time.Second}
	c.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	return c
}

func (c *idleConn) deadline() time.Time {
	d := time.Now().Add(c.timeout)
	if parent, ok := c.ctx.Deadline(); ok && parent.Before(d) {
		return parent
	}
	return d
}
func (c *idleConn) Read(b []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(c.deadline())
	return c.Conn.Read(b)
}
func (c *idleConn) Write(b []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(c.deadline())
	return c.Conn.Write(b)
}
func (c *idleConn) Close() error { c.stop(); return c.Conn.Close() }
