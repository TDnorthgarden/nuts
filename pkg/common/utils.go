package common

import (
	"fmt"
	"net"
	"os"
	"strings"
)

type Addr struct {
	Scheme string // e.g. "tcp", "udp", "unix"
	Host   string // e.g. "0.0.0.0:8080"
}

// String returns the original format, e.g. "tcp://0.0.0.0:8080".
func (a Addr) String() string {
	return fmt.Sprintf("%s://%s", a.Scheme, a.Host)
}

func ParseAddr(raw string) (Addr, error) {
	scheme, host, found := strings.Cut(raw, "://")
	if !found {
		return Addr{}, fmt.Errorf("invalid address %q: missing \"://\" separator", raw)
	}
	if scheme == "" {
		return Addr{}, fmt.Errorf("invalid address %q: missing scheme", raw)
	}
	if host == "" {
		return Addr{}, fmt.Errorf("invalid address %q: missing host after \"://\"", raw)
	}
	// Validate: reject query/fragment for all schemes.
	if strings.ContainsAny(host, "?#") {
		return Addr{}, fmt.Errorf("invalid address %q: host contains unexpected query/fragment characters", raw)
	}
	// For non-unix schemes, also reject path separators.
	// Unix sockets use the host as a filesystem path, so "/" is allowed.
	if scheme != "unix" && strings.Contains(host, "/") {
		return Addr{}, fmt.Errorf("invalid address %q: host contains unexpected path separator", raw)
	}
	// Reject userinfo (contains '@') for all schemes — not applicable here.
	if strings.Contains(host, "@") {
		return Addr{}, fmt.Errorf("invalid address %q: host should not contain userinfo (\"@\")", raw)
	}
	return Addr{Scheme: scheme, Host: host}, nil
}

func NewListener(raw string) (net.Listener, error) {
	addr, err := ParseAddr(raw)
	if err != nil {
		return nil, err
	}

	switch addr.Scheme {
	case "tcp":
		if lis, err := net.Listen("tcp", addr.Host); err != nil {
			return nil, fmt.Errorf("list tcp error %v", err)
		} else {
			return lis, nil
		}
	case "unix":
		// 启动前清理可能残留的socket文件，避免"address already in use"错误[1,5](@ref)
		if err := os.RemoveAll(addr.Host); err != nil {
			return nil, fmt.Errorf("list unix remove file %s error %v", addr.Host, err)
		}

		// 创建Unix Socket监听器[1,4](@ref)
		lis, err := net.Listen("unix", addr.Host)
		if err != nil {
			return nil, fmt.Errorf("list unix error %v", err)
		}

		// 可选：设置socket文件权限，增强安全性[4,5](@ref)
		if err := os.Chmod(addr.Host, 0660); err != nil {
			return nil, fmt.Errorf("list unix chmod %s error %v", addr.Host, err)
		}

		return lis, nil
	}
	return nil, fmt.Errorf("not support proto %s", addr.Scheme)
}
