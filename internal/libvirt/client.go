package libvirt

import (
	"fmt"
	"net"
	"net/url"

	golibvirt "github.com/digitalocean/go-libvirt"
)

// Client wraps a connected go-libvirt RPC client.
type Client struct {
	*golibvirt.Libvirt
	conn net.Conn
}

// Connect dials libvirtd at the given URI (e.g. "qemu:///system") over its
// local Unix socket and returns a connected Client.
func Connect(uri string) (*Client, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("parsing libvirt uri %q: %w", uri, err)
	}
	if u.Scheme != "qemu" {
		return nil, fmt.Errorf("unsupported libvirt scheme %q, only qemu:// is supported", u.Scheme)
	}

	socketPath := "/var/run/libvirt/libvirt-sock"
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("dialing libvirt socket %s: %w", socketPath, err)
	}

	l := golibvirt.New(conn)
	if err := l.Connect(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("connecting to libvirt: %w", err)
	}

	return &Client{Libvirt: l, conn: conn}, nil
}

func (c *Client) Close() error {
	if err := c.Libvirt.Disconnect(); err != nil {
		return fmt.Errorf("disconnecting from libvirt: %w", err)
	}
	return c.conn.Close()
}
