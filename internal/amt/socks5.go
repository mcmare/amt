package amt

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

func socksDestination(conn net.Conn) (string, error) {
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return "", err
	}
	if header[0] != 5 || header[1] == 0 {
		return "", errors.New("not SOCKS5")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", err
	}
	supported := false
	for _, m := range methods {
		if m == 0 {
			supported = true
			break
		}
	}
	if !supported {
		_, _ = conn.Write([]byte{5, 255})
		return "", errors.New("SOCKS client does not offer no-auth")
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return "", err
	}
	var cmd [4]byte
	if _, err := io.ReadFull(conn, cmd[:]); err != nil {
		return "", err
	}
	if cmd[0] != 5 || cmd[1] != 1 || cmd[2] != 0 {
		socksReply(conn, 7)
		return "", errors.New("only SOCKS5 CONNECT supported")
	}
	var host string
	switch cmd[3] {
	case 1:
		a := make([]byte, 4)
		if _, err := io.ReadFull(conn, a); err != nil {
			return "", err
		}
		host = net.IP(a).String()
	case 4:
		a := make([]byte, 16)
		if _, err := io.ReadFull(conn, a); err != nil {
			return "", err
		}
		host = net.IP(a).String()
	case 3:
		var size [1]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return "", err
		}
		if size[0] == 0 {
			return "", errors.New("empty domain")
		}
		a := make([]byte, int(size[0]))
		if _, err := io.ReadFull(conn, a); err != nil {
			return "", err
		}
		host = string(a)
	default:
		socksReply(conn, 8)
		return "", errors.New("unsupported address type")
	}
	var port [2]byte
	if _, err := io.ReadFull(conn, port[:]); err != nil {
		return "", err
	}
	p := int(port[0])<<8 | int(port[1])
	if p == 0 {
		return "", fmt.Errorf("invalid port: %d", p)
	}
	return net.JoinHostPort(host, strconv.Itoa(p)), nil
}

func socksReply(conn net.Conn, code byte) {
	// BND.ADDR is 0.0.0.0:0, because binding is performed at the exit.
	_, _ = conn.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0})
}
