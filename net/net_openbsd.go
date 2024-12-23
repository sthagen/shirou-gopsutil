// SPDX-License-Identifier: BSD-3-Clause
//go:build openbsd

package net

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/shirou/gopsutil/v4/internal/common"
)

var portMatch = regexp.MustCompile(`(.*)\.(\d+)$`)

func ParseNetstat(output string, mode string,
	iocs map[string]IOCountersStat,
) error {
	lines := strings.Split(output, "\n")

	exists := make([]string, 0, len(lines)-1)

	columns := 9
	if mode == "inb" {
		columns = 6
	}
	for _, line := range lines {
		values := strings.Fields(line)
		if len(values) < 1 || values[0] == "Name" {
			continue
		}
		if common.StringsHas(exists, values[0]) {
			// skip if already get
			continue
		}

		if len(values) < columns {
			continue
		}
		base := 1
		// sometimes Address is omitted
		if len(values) < columns {
			base = 0
		}

		parsed := make([]uint64, 0, 8)
		var vv []string
		switch mode {
		case "inb":
			vv = []string{
				values[base+3], // BytesRecv
				values[base+4], // BytesSent
			}
		case "ind":
			vv = []string{
				values[base+3], // Ipkts
				values[base+4], // Idrop
				values[base+5], // Opkts
				values[base+6], // Odrops
			}
		case "ine":
			vv = []string{
				values[base+4], // Ierrs
				values[base+6], // Oerrs
			}
		}
		for _, target := range vv {
			if target == "-" {
				parsed = append(parsed, 0)
				continue
			}

			t, err := strconv.ParseUint(target, 10, 64)
			if err != nil {
				return err
			}
			parsed = append(parsed, t)
		}
		exists = append(exists, values[0])

		n, present := iocs[values[0]]
		if !present {
			n = IOCountersStat{Name: values[0]}
		}

		switch mode {
		case "inb":
			n.BytesRecv = parsed[0]
			n.BytesSent = parsed[1]
		case "ind":
			n.PacketsRecv = parsed[0]
			n.Dropin = parsed[1]
			n.PacketsSent = parsed[2]
			n.Dropout = parsed[3]
		case "ine":
			n.Errin = parsed[0]
			n.Errout = parsed[1]
		}

		iocs[n.Name] = n
	}
	return nil
}

func IOCounters(pernic bool) ([]IOCountersStat, error) {
	return IOCountersWithContext(context.Background(), pernic)
}

func IOCountersWithContext(ctx context.Context, pernic bool) ([]IOCountersStat, error) {
	netstat, err := exec.LookPath("netstat")
	if err != nil {
		return nil, err
	}
	out, err := invoke.CommandWithContext(ctx, netstat, "-inb")
	if err != nil {
		return nil, err
	}
	out2, err := invoke.CommandWithContext(ctx, netstat, "-ind")
	if err != nil {
		return nil, err
	}
	out3, err := invoke.CommandWithContext(ctx, netstat, "-ine")
	if err != nil {
		return nil, err
	}
	iocs := make(map[string]IOCountersStat)

	lines := strings.Split(string(out), "\n")
	ret := make([]IOCountersStat, 0, len(lines)-1)

	err = ParseNetstat(string(out), "inb", iocs)
	if err != nil {
		return nil, err
	}
	err = ParseNetstat(string(out2), "ind", iocs)
	if err != nil {
		return nil, err
	}
	err = ParseNetstat(string(out3), "ine", iocs)
	if err != nil {
		return nil, err
	}

	for _, ioc := range iocs {
		ret = append(ret, ioc)
	}

	if pernic == false {
		return getIOCountersAll(ret)
	}

	return ret, nil
}

// IOCountersByFile exists just for compatibility with Linux.
func IOCountersByFile(pernic bool, filename string) ([]IOCountersStat, error) {
	return IOCountersByFileWithContext(context.Background(), pernic, filename)
}

func IOCountersByFileWithContext(ctx context.Context, pernic bool, filename string) ([]IOCountersStat, error) {
	return IOCounters(pernic)
}

func FilterCounters() ([]FilterStat, error) {
	return FilterCountersWithContext(context.Background())
}

func FilterCountersWithContext(ctx context.Context) ([]FilterStat, error) {
	return nil, common.ErrNotImplementedError
}

func ConntrackStats(percpu bool) ([]ConntrackStat, error) {
	return ConntrackStatsWithContext(context.Background(), percpu)
}

func ConntrackStatsWithContext(ctx context.Context, percpu bool) ([]ConntrackStat, error) {
	return nil, common.ErrNotImplementedError
}

// ProtoCounters returns network statistics for the entire system
// If protocols is empty then all protocols are returned, otherwise
// just the protocols in the list are returned.
// Not Implemented for OpenBSD
func ProtoCounters(protocols []string) ([]ProtoCountersStat, error) {
	return ProtoCountersWithContext(context.Background(), protocols)
}

func ProtoCountersWithContext(ctx context.Context, protocols []string) ([]ProtoCountersStat, error) {
	return nil, common.ErrNotImplementedError
}

func parseNetstatLine(line string) (ConnectionStat, error) {
	f := strings.Fields(line)
	if len(f) < 5 {
		return ConnectionStat{}, fmt.Errorf("wrong line,%s", line)
	}

	var netType, netFamily uint32
	switch f[0] {
	case "tcp":
		netType = syscall.SOCK_STREAM
		netFamily = syscall.AF_INET
	case "udp":
		netType = syscall.SOCK_DGRAM
		netFamily = syscall.AF_INET
	case "tcp6":
		netType = syscall.SOCK_STREAM
		netFamily = syscall.AF_INET6
	case "udp6":
		netType = syscall.SOCK_DGRAM
		netFamily = syscall.AF_INET6
	default:
		return ConnectionStat{}, fmt.Errorf("unknown type, %s", f[0])
	}

	laddr, raddr, err := parseNetstatAddr(f[3], f[4], netFamily)
	if err != nil {
		return ConnectionStat{}, fmt.Errorf("failed to parse netaddr, %s %s", f[3], f[4])
	}

	n := ConnectionStat{
		Fd:     uint32(0), // not supported
		Family: uint32(netFamily),
		Type:   uint32(netType),
		Laddr:  laddr,
		Raddr:  raddr,
		Pid:    int32(0), // not supported
	}
	if len(f) == 6 {
		n.Status = f[5]
	}

	return n, nil
}

func parseNetstatAddr(local string, remote string, family uint32) (laddr Addr, raddr Addr, err error) {
	parse := func(l string) (Addr, error) {
		matches := portMatch.FindStringSubmatch(l)
		if matches == nil {
			return Addr{}, fmt.Errorf("wrong addr, %s", l)
		}
		host := matches[1]
		port := matches[2]
		if host == "*" {
			switch family {
			case syscall.AF_INET:
				host = "0.0.0.0"
			case syscall.AF_INET6:
				host = "::"
			default:
				return Addr{}, fmt.Errorf("unknown family, %d", family)
			}
		}
		lport, err := strconv.ParseInt(port, 10, 32)
		if err != nil {
			return Addr{}, err
		}
		return Addr{IP: host, Port: uint32(lport)}, nil
	}

	laddr, err = parse(local)
	if remote != "*.*" { // remote addr exists
		raddr, err = parse(remote)
		if err != nil {
			return laddr, raddr, err
		}
	}

	return laddr, raddr, err
}

// Return a list of network connections opened.
func Connections(kind string) ([]ConnectionStat, error) {
	return ConnectionsWithContext(context.Background(), kind)
}

func ConnectionsWithContext(ctx context.Context, kind string) ([]ConnectionStat, error) {
	var ret []ConnectionStat

	args := []string{"-na"}
	switch strings.ToLower(kind) {
	default:
		fallthrough
	case "":
		fallthrough
	case "all":
		fallthrough
	case "inet":
		// nothing to add
	case "inet4":
		args = append(args, "-finet")
	case "inet6":
		args = append(args, "-finet6")
	case "tcp":
		args = append(args, "-ptcp")
	case "tcp4":
		args = append(args, "-ptcp", "-finet")
	case "tcp6":
		args = append(args, "-ptcp", "-finet6")
	case "udp":
		args = append(args, "-pudp")
	case "udp4":
		args = append(args, "-pudp", "-finet")
	case "udp6":
		args = append(args, "-pudp", "-finet6")
	case "unix":
		return ret, common.ErrNotImplementedError
	}

	netstat, err := exec.LookPath("netstat")
	if err != nil {
		return nil, err
	}
	out, err := invoke.CommandWithContext(ctx, netstat, args...)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if !(strings.HasPrefix(line, "tcp") || strings.HasPrefix(line, "udp")) {
			continue
		}
		n, err := parseNetstatLine(line)
		if err != nil {
			continue
		}

		ret = append(ret, n)
	}

	return ret, nil
}

// Return a list of network connections opened by a process.
func ConnectionsPid(kind string, pid int32) ([]ConnectionStat, error) {
	return ConnectionsPidWithContext(context.Background(), kind, pid)
}

func ConnectionsPidWithContext(ctx context.Context, kind string, pid int32) ([]ConnectionStat, error) {
	return nil, common.ErrNotImplementedError
}

// Return a list of network connections opened returning at most `max`
// connections for each running process.
func ConnectionsMax(kind string, maxConn int) ([]ConnectionStat, error) {
	return ConnectionsMaxWithContext(context.Background(), kind, maxConn)
}

func ConnectionsMaxWithContext(ctx context.Context, kind string, maxConn int) ([]ConnectionStat, error) {
	return nil, common.ErrNotImplementedError
}

// Return up to `max` network connections opened by a process.
func ConnectionsPidMax(kind string, pid int32, maxConn int) ([]ConnectionStat, error) {
	return ConnectionsPidMaxWithContext(context.Background(), kind, pid, maxConn)
}

func ConnectionsPidMaxWithContext(ctx context.Context, kind string, pid int32, maxConn int) ([]ConnectionStat, error) {
	return nil, common.ErrNotImplementedError
}

// Return a list of network connections opened, omitting `Uids`.
// WithoutUids functions are reliant on implementation details. They may be altered to be an alias for Connections or be
// removed from the API in the future.
func ConnectionsWithoutUids(kind string) ([]ConnectionStat, error) {
	return ConnectionsWithoutUidsWithContext(context.Background(), kind)
}

func ConnectionsWithoutUidsWithContext(ctx context.Context, kind string) ([]ConnectionStat, error) {
	return ConnectionsMaxWithoutUidsWithContext(ctx, kind, 0)
}

func ConnectionsMaxWithoutUidsWithContext(ctx context.Context, kind string, maxConn int) ([]ConnectionStat, error) {
	return ConnectionsPidMaxWithoutUidsWithContext(ctx, kind, 0, maxConn)
}

func ConnectionsPidWithoutUids(kind string, pid int32) ([]ConnectionStat, error) {
	return ConnectionsPidWithoutUidsWithContext(context.Background(), kind, pid)
}

func ConnectionsPidWithoutUidsWithContext(ctx context.Context, kind string, pid int32) ([]ConnectionStat, error) {
	return ConnectionsPidMaxWithoutUidsWithContext(ctx, kind, pid, 0)
}

func ConnectionsPidMaxWithoutUids(kind string, pid int32, maxConn int) ([]ConnectionStat, error) {
	return ConnectionsPidMaxWithoutUidsWithContext(context.Background(), kind, pid, maxConn)
}

func ConnectionsPidMaxWithoutUidsWithContext(ctx context.Context, kind string, pid int32, maxConn int) ([]ConnectionStat, error) {
	return connectionsPidMaxWithoutUidsWithContext(ctx, kind, pid, maxConn)
}

func connectionsPidMaxWithoutUidsWithContext(ctx context.Context, kind string, pid int32, maxConn int) ([]ConnectionStat, error) {
	return nil, common.ErrNotImplementedError
}
