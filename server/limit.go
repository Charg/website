package main

import (
	"net"
	"net/netip"
	"sync"
)

// ipLimiter caps open connections per client: per address for IPv4, per /64
// for IPv6. A nil *ipLimiter allows everything.
type ipLimiter struct {
	max int
	mu  sync.Mutex
	n   map[netip.Prefix]int
}

// newIPLimiter returns a limiter allowing max connections per client, or nil
// if max is 0 or less.
func newIPLimiter(max int) *ipLimiter {
	if max <= 0 {
		return nil
	}
	return &ipLimiter{max: max, n: map[netip.Prefix]int{}}
}

// acquire takes a slot for addr's client. It returns false if the client is
// at its limit, else a func that gives the slot back.
func (l *ipLimiter) acquire(addr net.Addr) (release func(), ok bool) {
	if l == nil {
		return func() {}, true
	}
	k := clientKey(addr)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n[k] >= l.max {
		return nil, false
	}
	l.n[k]++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.n[k]--; l.n[k] <= 0 {
			delete(l.n, k)
		}
	}, true
}

// open returns the number of slots held.
func (l *ipLimiter) open() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	total := 0
	for _, n := range l.n {
		total += n
	}
	return total
}

// clientKey is the address for IPv4 and the /64 for IPv6. Addresses that
// are not IP share the zero key.
func clientKey(addr net.Addr) netip.Prefix {
	var ip netip.Addr
	if a, ok := addr.(*net.TCPAddr); ok {
		ip, _ = netip.AddrFromSlice(a.IP)
	} else if ap, err := netip.ParseAddrPort(addr.String()); err == nil {
		ip = ap.Addr()
	}
	ip = ip.Unmap().WithZone("")
	bits := 32
	if ip.Is6() {
		bits = 64
	}
	p, _ := ip.Prefix(bits)
	return p
}

// limitedConn gives its slot back the first time it is closed.
type limitedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
