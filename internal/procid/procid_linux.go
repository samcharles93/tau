//go:build linux

package procid

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// linuxClockTicksPerSec is USER_HZ, the unit /proc/<pid>/stat's starttime
// field is reported in. 100 is standard on every mainstream Linux
// distribution/architecture (verified via sysconf(_SC_CLK_TCK) at build
// time on essentially all production kernels); reading it dynamically
// would require cgo, which this package avoids.
const linuxClockTicksPerSec = 100

// CaptureProcessStartNS reads pid's process-start time from
// /proc/<pid>/stat and converts it to nanoseconds since boot, to be
// persisted as agent_instances.process_start_ns. Returns 0 (unavailable)
// on any read/parse failure - callers treat 0 as "no identity recorded".
func CaptureProcessStartNS(pid int) int64 {
	ticks, err := readProcStartTicks(pid)
	if err != nil {
		return 0
	}
	return ticksToNS(ticks)
}

// CheckPIDIdentity reports whether pid is currently running and, when
// wantStartNS is non-zero, whether its process-start identity still
// matches - distinguishing "the process we spawned is still running" from
// "the OS recycled this pid for an unrelated process" (see
// docs/specs/agents/04-storage-and-sessions.md, Orphan sweep).
func CheckPIDIdentity(pid int, wantStartNS int64) PIDCheck {
	ticks, err := readProcStartTicks(pid)
	if err != nil {
		if os.IsNotExist(err) {
			return PIDCheckDead
		}
		return PIDCheckIndeterminate
	}
	if wantStartNS == 0 {
		// No identity was recorded at spawn time (capture failed, or the
		// row predates this field) - pid existing is the best evidence
		// available.
		return PIDCheckAlive
	}
	if ticksToNS(ticks) == wantStartNS {
		return PIDCheckAlive
	}
	return PIDCheckDead
}

func ticksToNS(ticks int64) int64 {
	return ticks * int64(time.Second) / linuxClockTicksPerSec
}

// readProcStartTicks reads /proc/<pid>/stat field 22 (starttime, in clock
// ticks since boot; see proc(5)). Field 2 (comm) is parenthesized and may
// itself contain spaces or parentheses, so splitting must start after the
// *last* ')' in the line rather than naively splitting on whitespace from
// the start.
func readProcStartTicks(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	s := string(data)
	idx := strings.LastIndexByte(s, ')')
	if idx < 0 || idx+2 > len(s) {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(s[idx+2:])
	// Fields after comm start counting at field 3 (state); starttime is
	// field 22, so its index into this slice is 22-3 = 19.
	const starttimeIndex = 22 - 3
	if len(fields) <= starttimeIndex {
		return 0, fmt.Errorf("malformed /proc/%d/stat: too few fields", pid)
	}
	ticks, err := strconv.ParseInt(fields[starttimeIndex], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse starttime: %w", err)
	}
	return ticks, nil
}
