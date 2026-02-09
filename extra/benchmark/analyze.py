#!/usr/bin/env python3
"""
Surge Download Log Analyzer - Verbose Optimization Edition
Parses debug.log and provides detailed performance insights.
"""

import os
import re
import sys
from datetime import datetime, timedelta
from collections import defaultdict
from dataclasses import dataclass, field
from typing import Optional

try:
    import matplotlib.pyplot as plt
    import matplotlib.dates as mdates

    HAS_MATPLOTLIB = True
except ImportError:
    HAS_MATPLOTLIB = False
    print("⚠️  matplotlib not installed. Speed graph will be skipped.")

# ==============================================================================
# CONFIGURATION
# ==============================================================================
SLOW_TASK_THRESHOLD_MULTIPLIER = 2.0  # Tasks > 2x avg are flagged as slow
GAP_WARNING_THRESHOLD_MS = 500  # Warn if gap between tasks > 500ms
SPEED_VARIANCE_WARN_RATIO = 3.0  # Warn if slowest worker is 3x slower than fastest
TOP_N_SLOW_TASKS = 3  # Show top N slowest tasks per worker

MB = 1024 * 1024
GB = 1024 * 1024 * 1024


# ==============================================================================
# DATA CLASSES
# ==============================================================================
@dataclass
class Task:
    """Represents a single completed download task."""

    timestamp: datetime  # When the task FINISHED (log timestamp)
    offset: int
    length: int
    duration_seconds: float

    @property
    def start_time(self) -> datetime:
        """Infer when the task started."""
        return self.timestamp - timedelta(seconds=self.duration_seconds)

    @property
    def speed_mbps(self) -> float:
        """Calculate download speed in MB/s."""
        if self.duration_seconds <= 0:
            return 0.0
        return (self.length / MB) / self.duration_seconds


@dataclass
class WorkerStats:
    """Aggregated stats for a single worker."""

    worker_id: int
    start_time: Optional[datetime] = None
    end_time: Optional[datetime] = None
    tasks: list = field(default_factory=list)

    @property
    def total_work_time(self) -> float:
        return sum(t.duration_seconds for t in self.tasks)

    @property
    def total_bytes(self) -> int:
        return sum(t.length for t in self.tasks)

    @property
    def avg_speed_mbps(self) -> float:
        if self.total_work_time <= 0:
            return 0.0
        return (self.total_bytes / MB) / self.total_work_time

    @property
    def wall_time(self) -> float:
        if self.start_time and self.end_time:
            return (self.end_time - self.start_time).total_seconds()
        return 0.0

    @property
    def utilization(self) -> float:
        """Percentage of wall time spent downloading."""
        if self.wall_time <= 0:
            return 0.0
        # Cap at 100% due to timestamp precision
        return min(100.0, (self.total_work_time / self.wall_time) * 100)

    @property
    def idle_time(self) -> float:
        """Estimated idle time = wall time - work time."""
        return max(0.0, self.wall_time - self.total_work_time)


# ==============================================================================
# DURATION PARSING
# ==============================================================================
def parse_duration(duration_str: str) -> float:
    """
    Parse Go-style duration string to seconds.
    Supports: h, m, s, ms, µs/us, ns
    Examples: "1m30s", "500ms", "2.5s", "1h2m3.5s"
    """
    duration_str = duration_str.strip()
    total_seconds = 0.0

    # Regex to find all value-unit pairs
    pattern = re.compile(r"(\d+\.?\d*)(ns|µs|us|ms|s|m|h)")
    matches = pattern.findall(duration_str)

    if not matches:
        # Try parsing as just a number (assume seconds)
        try:
            return float(duration_str.rstrip("s"))
        except ValueError:
            return 0.0

    for value, unit in matches:
        val = float(value)
        if unit == "h":
            total_seconds += val * 3600
        elif unit == "m":
            total_seconds += val * 60
        elif unit == "s":
            total_seconds += val
        elif unit == "ms":
            total_seconds += val / 1000
        elif unit in ("µs", "us"):
            total_seconds += val / 1_000_000
        elif unit == "ns":
            total_seconds += val / 1_000_000_000

    return total_seconds


# ==============================================================================
# LOG PARSING
# ==============================================================================
def parse_log_file(filename: str) -> dict:
    """Parse the debug.log file and extract all relevant data."""
    try:
        with open(filename, "r", encoding="utf-8") as f:
            lines = f.readlines()
    except FileNotFoundError:
        print(f"❌ Error: File '{filename}' not found.")
        sys.exit(1)

    # Compiled regex patterns
    timestamp_re = re.compile(r"\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\]")
    worker_task_re = re.compile(
        r"Worker (\d+): Task offset=(\d+) length=(\d+) took (\S+)"
    )
    worker_event_re = re.compile(r"Worker (\d+) (started|finished)")
    dl_complete_re = re.compile(r"Download .+ completed in (\S+) \(([^)]+)\)")
    probe_complete_re = re.compile(r"Probe complete - filename: (.+), size: (\d+)")
    balancer_split_re = re.compile(
        r"Balancer: split largest task \(total splits: (\d+)\)"
    )
    health_kill_re = re.compile(r"Health: Worker (\d+) (stalled|slow)")

    # Data structures
    workers: dict[int, WorkerStats] = {}
    balancer_splits: list[tuple[datetime, int]] = []
    health_kills: list[tuple[datetime, int, str]] = []  # (timestamp, worker_id, reason)
    download_info = {}
    current_time: Optional[datetime] = None

    for line in lines:
        line = line.strip()

        # Extract timestamp
        ts_match = timestamp_re.match(line)
        if ts_match:
            try:
                current_time = datetime.strptime(ts_match.group(1), "%Y-%m-%d %H:%M:%S")
            except ValueError:
                pass

        if not current_time:
            continue

        # Worker started/finished
        event_match = worker_event_re.search(line)
        if event_match:
            wid = int(event_match.group(1))
            event = event_match.group(2)

            if wid not in workers:
                workers[wid] = WorkerStats(worker_id=wid)

            if event == "started":
                workers[wid].start_time = current_time
            elif event == "finished":
                workers[wid].end_time = current_time
            continue

        # Task completed
        task_match = worker_task_re.search(line)
        if task_match:
            wid = int(task_match.group(1))
            offset = int(task_match.group(2))
            length = int(task_match.group(3))
            duration = parse_duration(task_match.group(4))

            if wid not in workers:
                workers[wid] = WorkerStats(worker_id=wid)

            task = Task(
                timestamp=current_time,
                offset=offset,
                length=length,
                duration_seconds=duration,
            )
            workers[wid].tasks.append(task)
            continue

        # Balancer splits
        split_match = balancer_split_re.search(line)
        if split_match:
            total = int(split_match.group(1))
            balancer_splits.append((current_time, total))
            continue

        # Download completed
        dl_match = dl_complete_re.search(line)
        if dl_match:
            download_info["total_duration"] = parse_duration(dl_match.group(1))
            download_info["avg_speed"] = dl_match.group(2)
            download_info["end_time"] = current_time
            continue

        # Probe info
        probe_match = probe_complete_re.search(line)
        if probe_match:
            download_info["filename"] = probe_match.group(1)
            download_info["size"] = int(probe_match.group(2))
            continue

        # Health check kills
        health_match = health_kill_re.search(line)
        if health_match:
            wid = int(health_match.group(1))
            reason = health_match.group(2)
            health_kills.append((current_time, wid, reason))
            continue

    return {
        "workers": workers,
        "balancer_splits": balancer_splits,
        "health_kills": health_kills,
        "download_info": download_info,
    }


# ==============================================================================
# GRAPHING
# ==============================================================================
def generate_speed_graph(workers: dict, output_file: str = "speed_graph.png"):
    """
    Generate a graph of download speeds over time.
    Shows individual task speeds as scatter points and a rolling average line.
    """
    if not HAS_MATPLOTLIB:
        print("\n⚠️  Skipping graph generation (matplotlib not available).")
        return

    # Collect all tasks with timestamps
    all_tasks_with_time = []
    for w in workers.values():
        for t in w.tasks:
            all_tasks_with_time.append(
                {
                    "time": t.timestamp,
                    "start_time": t.start_time,
                    "speed": t.speed_mbps,
                    "worker_id": w.worker_id,
                    "size": t.length,
                }
            )

    if not all_tasks_with_time:
        print("\n⚠️  No task data available for graph.")
        return

    # Sort by completion time
    all_tasks_with_time.sort(key=lambda x: x["time"])

    # Extract data for plotting
    times = [t["time"] for t in all_tasks_with_time]
    speeds = [t["speed"] for t in all_tasks_with_time]
    worker_ids = [t["worker_id"] for t in all_tasks_with_time]
    sizes = [t["size"] for t in all_tasks_with_time]

    # Calculate rolling average (window of 10 tasks or 20% of total, whichever is smaller)
    window_size = min(10, max(3, len(speeds) // 5))
    rolling_avg = []
    for i in range(len(speeds)):
        start_idx = max(0, i - window_size + 1)
        window = speeds[start_idx : i + 1]
        rolling_avg.append(sum(window) / len(window))

    # Create figure with professional styling
    fig, ax = plt.subplots(figsize=(12, 6), dpi=100)
    fig.patch.set_facecolor("#1a1a2e")
    ax.set_facecolor("#16213e")

    # Color map for workers
    unique_workers = sorted(set(worker_ids))
    colors = plt.cm.viridis(
        [i / max(1, len(unique_workers) - 1) for i in range(len(unique_workers))]
    )
    color_map = {wid: colors[i] for i, wid in enumerate(unique_workers)}

    # Plot scatter points for each worker
    for wid in unique_workers:
        worker_times = [times[i] for i in range(len(times)) if worker_ids[i] == wid]
        worker_speeds = [speeds[i] for i in range(len(speeds)) if worker_ids[i] == wid]
        worker_sizes = [sizes[i] for i in range(len(sizes)) if worker_ids[i] == wid]

        # Size points by data size (normalized)
        max_size = max(sizes) if sizes else 1
        point_sizes = [30 + (s / max_size) * 100 for s in worker_sizes]

        ax.scatter(
            worker_times,
            worker_speeds,
            c=[color_map[wid]],
            s=point_sizes,
            alpha=0.6,
            label=f"Worker {wid}",
            edgecolors="white",
            linewidth=0.5,
        )

    # Plot rolling average line
    ax.plot(
        times,
        rolling_avg,
        color="#ff6b6b",
        linewidth=2.5,
        label=f"Rolling Avg ({window_size} tasks)",
        zorder=10,
    )

    # Add horizontal line for overall average
    overall_avg = sum(speeds) / len(speeds) if speeds else 0
    ax.axhline(
        y=overall_avg,
        color="#4ecdc4",
        linestyle="--",
        linewidth=1.5,
        label=f"Overall Avg: {overall_avg:.2f} MB/s",
        alpha=0.8,
    )

    # Styling
    ax.set_xlabel("Time", fontsize=12, color="white", fontweight="bold")
    ax.set_ylabel(
        "Download Speed (MB/s)", fontsize=12, color="white", fontweight="bold"
    )
    ax.set_title(
        "Download Speed Over Time",
        fontsize=14,
        color="white",
        fontweight="bold",
        pad=20,
    )

    # Format x-axis
    ax.xaxis.set_major_formatter(mdates.DateFormatter("%H:%M:%S"))
    plt.xticks(rotation=45, ha="right")

    # Grid and legend
    ax.grid(True, alpha=0.3, color="gray", linestyle="--")
    ax.legend(
        loc="upper right",
        facecolor="#0f3460",
        edgecolor="white",
        labelcolor="white",
        fontsize=9,
    )

    # Tick colors
    ax.tick_params(colors="white")
    for spine in ax.spines.values():
        spine.set_color("white")
        spine.set_alpha(0.3)

    plt.tight_layout()
    plt.savefig(
        output_file,
        facecolor=fig.get_facecolor(),
        edgecolor="none",
        bbox_inches="tight",
        dpi=150,
    )
    plt.close()

    print(f"\n📊 Speed graph saved to: {output_file}")


def generate_per_worker_speed_graph(
    workers: dict, health_kills: list = None, output_file: str = "worker_speeds.png"
):
    """
    Generate a subplot grid showing each worker's speed over time individually.
    Shows vertical lines where workers were killed by health checks.
    """
    if not HAS_MATPLOTLIB:
        print("\n⚠️  Skipping per-worker graph (matplotlib not available).")
        return

    # Filter workers with tasks
    active_workers = sorted(
        [w for w in workers.values() if w.tasks], key=lambda w: w.worker_id
    )

    if not active_workers:
        print("\n⚠️  No worker data available for per-worker graph.")
        return

    num_workers = len(active_workers)
    # Calculate grid layout (prefer 2 columns)
    cols = min(2, num_workers)
    rows = (num_workers + cols - 1) // cols

    fig, axes = plt.subplots(
        rows, cols, figsize=(7 * cols, 4 * rows), dpi=100, squeeze=False
    )
    fig.patch.set_facecolor("#1a1a2e")

    # Flatten axes array for easy iteration
    axes_flat = axes.flatten()

    # Calculate global speed range for consistent y-axis
    all_speeds = [t.speed_mbps for w in active_workers for t in w.tasks]
    y_max = max(all_speeds) * 1.1 if all_speeds else 100

    # Color for bars/line
    bar_color = "#4ecdc4"
    avg_color = "#ff6b6b"

    for idx, worker in enumerate(active_workers):
        ax = axes_flat[idx]
        ax.set_facecolor("#16213e")

        # Sort tasks by completion time
        sorted_tasks = sorted(worker.tasks, key=lambda t: t.timestamp)
        times = [t.timestamp for t in sorted_tasks]
        speeds = [t.speed_mbps for t in sorted_tasks]

        # Plot speed as line with markers
        ax.plot(
            times,
            speeds,
            color=bar_color,
            linewidth=1.5,
            marker="o",
            markersize=4,
            alpha=0.8,
            label="Speed",
        )

        # Add average line
        avg_speed = sum(speeds) / len(speeds) if speeds else 0
        ax.axhline(
            y=avg_speed,
            color=avg_color,
            linestyle="--",
            linewidth=1.5,
            alpha=0.8,
            label=f"Avg: {avg_speed:.2f} MB/s",
        )

        # Fill under the curve
        ax.fill_between(times, speeds, alpha=0.2, color=bar_color)

        # Styling
        ax.set_title(
            f"Worker {worker.worker_id}", fontsize=11, color="white", fontweight="bold"
        )
        ax.set_ylabel("Speed (MB/s)", fontsize=9, color="white")
        ax.set_ylim(0, y_max)

        # X-axis formatting
        ax.xaxis.set_major_formatter(mdates.DateFormatter("%H:%M:%S"))
        ax.tick_params(axis="x", rotation=45, labelsize=8, colors="white")
        ax.tick_params(axis="y", labelsize=8, colors="white")

        # Grid
        ax.grid(True, alpha=0.3, color="gray", linestyle="--")
        ax.legend(
            loc="upper right",
            fontsize=8,
            facecolor="#0f3460",
            edgecolor="white",
            labelcolor="white",
        )

        # Spine styling
        for spine in ax.spines.values():
            spine.set_color("white")
            spine.set_alpha(0.3)

        # Add vertical lines for health check kills
        if health_kills:
            worker_kills = [
                (ts, reason)
                for ts, wid, reason in health_kills
                if wid == worker.worker_id
            ]
            for kill_time, reason in worker_kills:
                color = (
                    "#ff4757" if reason == "stalled" else "#ffa502"
                )  # red for stalled, orange for slow
                ax.axvline(
                    x=kill_time, color=color, linestyle="-", linewidth=2, alpha=0.8
                )
                # Add small annotation at top
                ax.annotate(
                    reason[0].upper(),
                    xy=(kill_time, y_max * 0.95),
                    fontsize=7,
                    color=color,
                    ha="center",
                    fontweight="bold",
                )

    # Hide unused subplots
    for idx in range(num_workers, len(axes_flat)):
        axes_flat[idx].set_visible(False)

    plt.suptitle(
        "Per-Worker Download Speed",
        fontsize=14,
        color="white",
        fontweight="bold",
        y=1.02,
    )
    plt.tight_layout()
    plt.savefig(
        output_file,
        facecolor=fig.get_facecolor(),
        edgecolor="none",
        bbox_inches="tight",
        dpi=150,
    )
    plt.close()

    print(f"📊 Per-worker speed graph saved to: {output_file}")


# ==============================================================================
# ANALYSIS & REPORTING
# ==============================================================================
def print_header(title: str, char: str = "="):
    """Print a section header."""
    print(f"\n{char * 60}")
    print(f"  {title}")
    print(f"{char * 60}")


def analyze_and_report(data: dict):
    """Generate comprehensive analysis report."""
    workers = data["workers"]
    balancer_splits = data["balancer_splits"]
    download_info = data["download_info"]

    if not workers:
        print("❌ No worker data found in log file.")
        return

    # ==========================================================================
    # DOWNLOAD SUMMARY
    # ==========================================================================
    print_header("📥 DOWNLOAD SUMMARY")

    if "filename" in download_info:
        size_gb = download_info.get("size", 0) / GB
        print(f"  File:     {download_info['filename']}")
        print(f"  Size:     {size_gb:.2f} GB ({download_info.get('size', 0):,} bytes)")

    if "total_duration" in download_info:
        print(f"  Duration: {download_info['total_duration']:.2f}s")
        print(f"  Speed:    {download_info.get('avg_speed', 'N/A')}")

    print(f"  Workers:  {len(workers)}")

    # ==========================================================================
    # WORKER PERFORMANCE TABLE
    # ==========================================================================
    print_header("🚀 WORKER PERFORMANCE BREAKDOWN")

    # Calculate global averages for comparison
    all_tasks = [t for w in workers.values() for t in w.tasks]
    global_avg_speed = 0.0
    if all_tasks:
        total_bytes = sum(t.length for t in all_tasks)
        total_time = sum(t.duration_seconds for t in all_tasks)
        if total_time > 0:
            global_avg_speed = (total_bytes / MB) / total_time

    global_avg_task_duration = (
        sum(t.duration_seconds for t in all_tasks) / len(all_tasks) if all_tasks else 0
    )

    print(f"\n  Global Avg Speed: {global_avg_speed:.2f} MB/s")
    print(f"  Global Avg Task:  {global_avg_task_duration:.2f}s")
    print(f"  Total Tasks:      {len(all_tasks)}")

    # Table header
    print(
        f"\n  {'ID':>3} │ {'Tasks':>5} │ {'Avg Speed':>10} │ {'Util %':>7} │ {'Idle':>8} │ {'Status':<15}"
    )
    print(f"  {'─'*3}─┼─{'─'*5}─┼─{'─'*10}─┼─{'─'*7}─┼─{'─'*8}─┼─{'─'*15}")

    sorted_workers = sorted(workers.values(), key=lambda w: w.worker_id)

    speed_by_worker = {}
    for w in sorted_workers:
        task_count = len(w.tasks)
        avg_speed = w.avg_speed_mbps
        speed_by_worker[w.worker_id] = avg_speed
        util = w.utilization
        idle = w.idle_time

        # Determine status
        status = ""
        if task_count == 0:
            status = "NO TASKS ⚠️"
        elif idle > 5:
            status = f"IDLE {idle:.0f}s 💤"
        elif util < 50:
            status = "LOW UTIL ⚠️"
        elif avg_speed < global_avg_speed / SPEED_VARIANCE_WARN_RATIO:
            status = "SLOW 🐢"
        else:
            status = "OK ✅"

        util_str = f"{util:.2f}%" if util > 0 else "N/A"
        idle_str = f"{idle:.2f}s" if idle > 0 else "0s"
        speed_str = f"{avg_speed:.2f} MB/s" if avg_speed > 0 else "N/A"

        print(
            f"  {w.worker_id:>3} │ {task_count:>5} │ {speed_str:>10} │ {util_str:>7} │ {idle_str:>8} │ {status:<15}"
        )

    # ==========================================================================
    # SPEED VARIANCE ANALYSIS
    # ==========================================================================
    print_header("⚡ SPEED VARIANCE ANALYSIS")

    active_speeds = {wid: s for wid, s in speed_by_worker.items() if s > 0}
    if active_speeds:
        fastest_wid = max(active_speeds, key=active_speeds.get)
        slowest_wid = min(active_speeds, key=active_speeds.get)
        fastest_speed = active_speeds[fastest_wid]
        slowest_speed = active_speeds[slowest_wid]

        ratio = fastest_speed / slowest_speed if slowest_speed > 0 else 0

        print(f"\n  ⚡ Fastest: Worker {fastest_wid} @ {fastest_speed:.2f} MB/s")
        print(f"  🐢 Slowest: Worker {slowest_wid} @ {slowest_speed:.2f} MB/s")
        print(f"  📊 Ratio:   {ratio:.2f}x difference")

        if ratio >= SPEED_VARIANCE_WARN_RATIO:
            print(f"\n  ⚠️  WARNING: Speed variance is {ratio:.2f}x!")
            print(
                f"      This suggests Worker {slowest_wid} may have network issues or is"
            )
            print(
                f"      competing for bandwidth. Consider investigating connection quality."
            )

    # ==========================================================================
    # SLOW TASK DETECTION
    # ==========================================================================
    print_header("🐌 SLOW TASK ANALYSIS")

    slow_threshold = global_avg_task_duration * SLOW_TASK_THRESHOLD_MULTIPLIER
    print(f"\n  Slow Task Threshold: > {slow_threshold:.2f}s (2x average)")

    slow_tasks = []
    for w in sorted_workers:
        for t in w.tasks:
            if t.duration_seconds > slow_threshold:
                slow_tasks.append((w.worker_id, t))

    if slow_tasks:
        print(f"\n  Found {len(slow_tasks)} slow tasks:")
        print(
            f"\n  {'Worker':>6} │ {'Offset':>12} │ {'Size':>10} │ {'Duration':>10} │ {'Speed':>10}"
        )
        print(f"  {'─'*6}─┼─{'─'*12}─┼─{'─'*10}─┼─{'─'*10}─┼─{'─'*10}")

        # Sort by duration descending
        slow_tasks.sort(key=lambda x: x[1].duration_seconds, reverse=True)
        for wid, t in slow_tasks[:10]:  # Top 10 slowest
            offset_mb = t.offset / MB
            size_mb = t.length / MB
            print(
                f"  {wid:>6} │ {offset_mb:>10.2f}MB │ {size_mb:>8.2f}MB │ {t.duration_seconds:>8.2f}s │ {t.speed_mbps:>8.2f}MB/s"
            )

        if len(slow_tasks) > 10:
            print(f"\n  ... and {len(slow_tasks) - 10} more slow tasks")
    else:
        print("\n  ✅ No anomalously slow tasks detected.")

    # ==========================================================================
    # PER-WORKER DETAILED BREAKDOWN
    # ==========================================================================
    print_header("📋 PER-WORKER TASK DETAILS")

    for w in sorted_workers:
        if not w.tasks:
            continue

        speeds = [t.speed_mbps for t in w.tasks]
        min_speed = min(speeds) if speeds else 0
        max_speed = max(speeds) if speeds else 0

        print(f"\n  ┌─ Worker {w.worker_id} ─────────────────────────────────────────")
        print(f"  │ Tasks: {len(w.tasks):<5}  Total Data: {w.total_bytes / MB:.2f} MB")
        print(
            f"  │ Speed: Min={min_speed:.2f}, Avg={w.avg_speed_mbps:.2f}, Max={max_speed:.2f} MB/s"
        )
        print(
            f"  │ Wall Time: {w.wall_time:.2f}s  Work Time: {w.total_work_time:.2f}s  Idle: {w.idle_time:.2f}s"
        )

        # Top N slowest tasks for this worker
        sorted_tasks = sorted(w.tasks, key=lambda t: t.duration_seconds, reverse=True)
        print(f"  │")
        print(f"  │ Top {TOP_N_SLOW_TASKS} Slowest Tasks:")
        for i, t in enumerate(sorted_tasks[:TOP_N_SLOW_TASKS], 1):
            print(
                f"  │   {i}. {t.duration_seconds:.2f}s @ {t.speed_mbps:.2f}MB/s (offset {t.offset / MB:.2f}MB)"
            )

        print(f"  └{'─' * 50}")

    # ==========================================================================
    # BALANCER ACTIVITY
    # ==========================================================================
    if balancer_splits:
        print_header("🔄 BALANCER ACTIVITY")

        total_splits = balancer_splits[-1][1] if balancer_splits else 0
        first_split_time = balancer_splits[0][0] if balancer_splits else None
        last_split_time = balancer_splits[-1][0] if balancer_splits else None

        print(f"\n  Total Splits: {total_splits}")
        if first_split_time and last_split_time:
            split_duration = (last_split_time - first_split_time).total_seconds()
            print(f"  Split Window: {split_duration:.2f}s")
            splits_per_sec = (
                len(balancer_splits) / split_duration if split_duration > 0 else 0
            )
            print(f"  Split Rate:   {splits_per_sec:.2f} splits/sec")

        if total_splits > 20:
            print(
                f"\n  ⚠️  High split count ({total_splits}) suggests end-game fragmentation."
            )
            print(
                f"      Consider increasing MinChunk or implementing smarter work stealing."
            )

    # ==========================================================================
    # OPTIMIZATION RECOMMENDATIONS
    # ==========================================================================
    print_header("💡 OPTIMIZATION RECOMMENDATIONS", "=")

    recommendations = []

    # Check for high speed variance
    if active_speeds:
        ratio = (
            max(active_speeds.values()) / min(active_speeds.values())
            if min(active_speeds.values()) > 0
            else 0
        )
        if ratio > SPEED_VARIANCE_WARN_RATIO:
            recommendations.append(
                f"HIGH SPEED VARIANCE ({ratio:.2f}x): Some workers are much slower. "
                f"Check network conditions or implement speed-based work stealing."
            )

    # Check for idle workers
    idle_workers = [w for w in sorted_workers if w.idle_time > 5]
    if idle_workers:
        recommendations.append(
            f"WORKER IDLE TIME: {len(idle_workers)} workers had >5s idle time. "
            f"Work stealing may not be aggressive enough."
        )

    # Check for low utilization
    low_util_workers = [w for w in sorted_workers if 0 < w.utilization < 70]
    if low_util_workers:
        recommendations.append(
            f"LOW UTILIZATION: {len(low_util_workers)} workers below 70% utilization. "
            f"Check for connection issues or increase chunk sizes."
        )

    # Check balancer activity
    if balancer_splits and len(balancer_splits) > 30:
        recommendations.append(
            f"EXCESSIVE SPLITTING: {len(balancer_splits)} balancer splits. "
            f"Consider increasing MinChunk to reduce end-game overhead."
        )

    # Check for slow tasks
    if slow_tasks:
        recommendations.append(
            f"SLOW TASKS: {len(slow_tasks)} tasks took >2x average duration. "
            f"Consider implementing task timeout/retry or connection health checks."
        )

    if recommendations:
        for i, rec in enumerate(recommendations, 1):
            print(f"\n  {i}. {rec}")
    else:
        print("\n  ✅ No major optimization issues detected. Download looks healthy!")

    # ==========================================================================
    # GENERATE SPEED GRAPHS
    # ==========================================================================
    print_header("📊 SPEED GRAPH GENERATION")
    generate_speed_graph(workers)
    generate_per_worker_speed_graph(workers, health_kills=data.get("health_kills", []))

    print("\n" + "=" * 60)


# ==============================================================================
# MAIN
# ==============================================================================
def main():
    filename = sys.argv[1] if len(sys.argv) > 1 else "debug.log"
    print(f"\n🔍 Surge Log Analyzer - Verbose Mode")
    print(f"   Analyzing: {filename}")

    data = parse_log_file(filename)
    analyze_and_report(data)


if __name__ == "__main__":
    main()
