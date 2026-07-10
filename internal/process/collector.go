package process

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/zylcold/openclaw-observatory/internal/event"
	"github.com/zylcold/openclaw-observatory/internal/storage"
)

type Sink func(context.Context, []event.Event) error

type Collector struct {
	repo       *storage.Repository
	sink       Sink
	producerID string
	sequence   atomic.Uint64
	interval   time.Duration
}

var (
	clockTicksOnce sync.Once
	clockTicks     = 100.0
)

func NewCollector(repo *storage.Repository, interval time.Duration, sink Sink) *Collector {
	return &Collector{repo: repo, sink: sink, producerID: "daemon-" + event.NewID(), interval: interval}
}

func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(c.interval)
	defer t.Stop()
	c.collect(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.collect(ctx)
		}
	}
}

func (c *Collector) collect(ctx context.Context) {
	refs, err := c.repo.ActiveProcesses(ctx)
	if err != nil {
		return
	}
	for _, ref := range refs {
		s, err := sample(ctx, ref.ProcessID)
		if err != nil {
			if !alive(ref.ProcessID) {
				_ = c.sink(ctx, []event.Event{c.newEvent("gateway.crashed", ref, map[string]any{"reason": "process_missing"})})
			}
			continue
		}
		_ = c.sink(ctx, []event.Event{c.newEvent("resource.sampled", ref, s)})
	}
}

func (c *Collector) newEvent(kind string, ref storage.ProcessRef, payload any) event.Event {
	b, _ := json.Marshal(payload)
	pid := ref.ProcessID
	return event.Event{SchemaVersion: 1, EventID: event.NewID(), EventType: kind, OccurredAt: time.Now().UTC(), InstanceID: ref.InstanceID,
		ProducerID: c.producerID, ProcessID: &pid, Sequence: c.sequence.Add(1), Source: "daemon", Payload: b}
}

func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

func sample(ctx context.Context, pid int) (map[string]any, error) {
	var result map[string]any
	var err error
	if runtime.GOOS == "linux" {
		result, err = sampleLinux(pid)
	} else {
		result, err = sampleDarwin(ctx, pid)
	}
	if err != nil {
		return nil, err
	}
	if total, available, diskErr := diskSpace(); diskErr == nil {
		result["diskTotalBytes"] = total
		result["diskAvailableBytes"] = available
	}
	return result, nil
}

func diskSpace() (int64, int64, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, 0, err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(home, &stat); err != nil {
		return 0, 0, err
	}
	blockSize := uint64(stat.Bsize)
	return int64(stat.Blocks * blockSize), int64(stat.Bavail * blockSize), nil
}

func sampleDarwin(ctx context.Context, pid int) (map[string]any, error) {
	out, err := exec.CommandContext(ctx, "ps", "-o", "time=,rss=,vsz=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return nil, fmt.Errorf("unexpected ps output %q", out)
	}
	cpu, err := parseCPUTime(fields[0])
	if err != nil {
		return nil, err
	}
	rss, _ := strconv.ParseInt(fields[1], 10, 64)
	vms, _ := strconv.ParseInt(fields[2], 10, 64)
	return map[string]any{"cpuSecondsTotal": cpu, "residentMemoryBytes": rss * 1024, "virtualMemoryBytes": vms * 1024, "threads": countThreadsDarwin(ctx, pid), "openFds": countFDsDarwin(ctx, pid)}, nil
}

func countThreadsDarwin(ctx context.Context, pid int) int {
	out, err := exec.CommandContext(ctx, "ps", "-M", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	lines := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	// Header plus one process summary precede the thread rows.
	if lines > 2 {
		return lines - 2
	}
	return 0
}

func countFDsDarwin(ctx context.Context, pid int) int {
	out, err := exec.CommandContext(ctx, "lsof", "-a", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) > 1 && line[0] == 'f' {
			n++
		}
	}
	return n
}

func parseCPUTime(s string) (float64, error) {
	var days float64
	if p := strings.SplitN(s, "-", 2); len(p) == 2 {
		d, err := strconv.ParseFloat(p[0], 64)
		if err != nil {
			return 0, err
		}
		days = d
		s = p[1]
	}
	p := strings.Split(s, ":")
	if len(p) < 2 || len(p) > 3 {
		return 0, fmt.Errorf("invalid CPU time %q", s)
	}
	var h, m float64
	var sec float64
	var err error
	if len(p) == 3 {
		h, err = strconv.ParseFloat(p[0], 64)
		if err != nil {
			return 0, err
		}
		m, _ = strconv.ParseFloat(p[1], 64)
		sec, err = strconv.ParseFloat(p[2], 64)
	} else {
		m, _ = strconv.ParseFloat(p[0], 64)
		sec, err = strconv.ParseFloat(p[1], 64)
	}
	if err != nil {
		return 0, err
	}
	return days*86400 + h*3600 + m*60 + sec, nil
}

func sampleLinux(pid int) (map[string]any, error) {
	status, err := os.Open(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return nil, err
	}
	defer status.Close()
	result := map[string]any{}
	scanner := bufio.NewScanner(status)
	for scanner.Scan() {
		f := strings.Fields(scanner.Text())
		if len(f) < 2 {
			continue
		}
		switch strings.TrimSuffix(f[0], ":") {
		case "VmRSS":
			v, _ := strconv.ParseInt(f[1], 10, 64)
			result["residentMemoryBytes"] = v * 1024
		case "VmSize":
			v, _ := strconv.ParseInt(f[1], 10, 64)
			result["virtualMemoryBytes"] = v * 1024
		case "Threads":
			v, _ := strconv.Atoi(f[1])
			result["threads"] = v
		}
	}
	fd, _ := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	result["openFds"] = len(fd)
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err == nil {
		f := strings.Fields(string(stat))
		if len(f) > 15 {
			u, _ := strconv.ParseFloat(f[13], 64)
			s, _ := strconv.ParseFloat(f[14], 64)
			result["cpuSecondsTotal"] = (u + s) / linuxClockTicks()
		}
	}
	return result, scanner.Err()
}

func linuxClockTicks() float64 {
	clockTicksOnce.Do(func() {
		out, err := exec.Command("getconf", "CLK_TCK").Output()
		if err != nil {
			return
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil && v > 0 {
			clockTicks = v
		}
	})
	return clockTicks
}
