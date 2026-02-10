package benchmark

import (
	"math"
	"runtime"
	"sync/atomic"
	"time"
)

// BenchmarkMetrics collects performance metrics during download
type BenchmarkMetrics struct {
	StartTime     time.Time
	FirstByteTime time.Time
	EndTime       time.Time

	TotalBytes    int64
	BytesReceived atomic.Int64

	RetryCount    atomic.Int32
	ConnectionMax atomic.Int32
	ConnectionSum atomic.Int64
	SampleCount   atomic.Int64

	// Memory tracking
	StartMemAlloc uint64
	PeakMemAlloc  uint64
}

// NewBenchmarkMetrics creates a new metrics collector
func NewBenchmarkMetrics() *BenchmarkMetrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &BenchmarkMetrics{
		StartTime:     time.Now(),
		StartMemAlloc: m.Alloc,
	}
}

// RecordFirstByte marks when the first byte was received
func (bm *BenchmarkMetrics) RecordFirstByte() {
	if bm.FirstByteTime.IsZero() {
		bm.FirstByteTime = time.Now()
	}
}

// RecordRetry increments the retry counter
func (bm *BenchmarkMetrics) RecordRetry() {
	bm.RetryCount.Add(1)
}

// RecordConnections samples the current connection count
func (bm *BenchmarkMetrics) RecordConnections(count int32) {
	bm.ConnectionSum.Add(int64(count))
	bm.SampleCount.Add(1)

	for {
		current := bm.ConnectionMax.Load()
		if count <= current {
			break
		}
		if bm.ConnectionMax.CompareAndSwap(current, count) {
			break
		}
	}
}

// RecordBytes adds to the total bytes received
func (bm *BenchmarkMetrics) RecordBytes(n int64) {
	bm.BytesReceived.Add(n)
}

// Finish marks the download as complete and captures final stats
func (bm *BenchmarkMetrics) Finish(totalBytes int64) {
	bm.EndTime = time.Now()
	bm.TotalBytes = totalBytes

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if m.Alloc > bm.PeakMemAlloc {
		bm.PeakMemAlloc = m.Alloc
	}
}

// GetResults returns the computed metrics
func (bm *BenchmarkMetrics) GetResults() BenchmarkResults {
	elapsed := bm.EndTime.Sub(bm.StartTime)
	ttfb := bm.FirstByteTime.Sub(bm.StartTime)

	var avgConnections float64
	if samples := bm.SampleCount.Load(); samples > 0 {
		avgConnections = float64(bm.ConnectionSum.Load()) / float64(samples)
	}

	throughput := float64(0)
	if elapsed.Seconds() > 0 {
		throughput = float64(bm.TotalBytes) / elapsed.Seconds() / (1024 * 1024)
	}

	return BenchmarkResults{
		TotalTime:      elapsed,
		TTFB:           ttfb,
		ThroughputMBps: throughput,
		TotalBytes:     bm.TotalBytes,
		RetryCount:     int(bm.RetryCount.Load()),
		MaxConnections: int(bm.ConnectionMax.Load()),
		AvgConnections: avgConnections,
		MemoryUsedMB:   float64(bm.PeakMemAlloc-bm.StartMemAlloc) / (1024 * 1024),
	}
}

// BenchmarkResults holds the final computed metrics
type BenchmarkResults struct {
	TotalTime      time.Duration
	TTFB           time.Duration
	ThroughputMBps float64
	TotalBytes     int64
	RetryCount     int
	MaxConnections int
	AvgConnections float64
	MemoryUsedMB   float64
}

// String returns a formatted summary of the results
func (br BenchmarkResults) String() string {
	return formatBenchmarkResults(br)
}

func formatBenchmarkResults(br BenchmarkResults) string {
	return "" +
		"=== Benchmark Results ===\n" +
		"Throughput:     " + formatFloat(br.ThroughputMBps, 2) + " MB/s\n" +
		"Total Time:     " + br.TotalTime.Round(time.Millisecond).String() + "\n" +
		"TTFB:           " + br.TTFB.Round(time.Millisecond).String() + "\n" +
		"Total Bytes:    " + formatBytes(br.TotalBytes) + "\n" +
		"Retries:        " + formatInt(br.RetryCount) + "\n" +
		"Max Connections:" + formatInt(br.MaxConnections) + "\n" +
		"Avg Connections:" + formatFloat(br.AvgConnections, 1) + "\n" +
		"Memory Used:    " + formatFloat(br.MemoryUsedMB, 2) + " MB\n"
}

func formatFloat(f float64, decimals int) string {
	pow := math.Pow(10, float64(decimals))
	rounded := math.Round(f*pow) / pow
	format := "%." + formatInt(decimals) + "f"
	return sprintf(format, rounded)
}

func formatInt(i int) string {
	return sprintf("%d", i)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func sprintf(format string, args ...interface{}) string {
	// Simple sprintf without importing fmt to avoid cycles
	// For benchmark output, we'll use a simpler approach
	result := format
	for _, arg := range args {
		switch v := arg.(type) {
		case int:
			result = replaceFirst(result, "%d", intToString(v))
		case int64:
			result = replaceFirst(result, "%d", int64ToString(v))
		case float64:
			result = replaceFirstFloat(result, v)
		case byte:
			result = replaceFirst(result, "%c", string(v))
		}
	}
	return result
}

func replaceFirst(s, old, new string) string {
	for i := 0; i <= len(s)-len(old); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}

func replaceFirstFloat(s string, f float64) string {
	// Find %.Xf pattern
	for i := 0; i < len(s)-3; i++ {
		if s[i] == '%' && s[i+1] == '.' && s[i+3] == 'f' {
			decimals := int(s[i+2] - '0')
			formatted := floatToString(f, decimals)
			return s[:i] + formatted + s[i+4:]
		}
	}
	return s
}

func intToString(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func int64ToString(i int64) string {
	return intToString(int(i))
}

func floatToString(f float64, decimals int) string {
	neg := f < 0
	if neg {
		f = -f
	}
	intPart := int64(f)
	fracPart := f - float64(intPart)

	for i := 0; i < decimals; i++ {
		fracPart *= 10
	}

	result := int64ToString(intPart) + "."
	fracInt := int64(fracPart + 0.5)
	fracStr := int64ToString(fracInt)

	// Pad with zeros if needed
	for len(fracStr) < decimals {
		fracStr = "0" + fracStr
	}
	result += fracStr

	if neg {
		result = "-" + result
	}
	return result
}
