#!/usr/bin/env python3
"""
Surge Download Log Analyzer - Verbose Optimization Edition
Parses debug.log and provides detailed performance insights.
"""

import argparse
import csv
import json
import os
import re
import sys
from dataclasses import dataclass, field
from datetime import datetime, timedelta
from pathlib import Path
from typing import Any, Optional

try:
    import matplotlib.dates as mdates
    import matplotlib.pyplot as plt

    HAS_MATPLOTLIB = True
except ImportError:
    HAS_MATPLOTLIB = False

MB = 1024 * 1024
GB = 1024 * 1024 * 1024


@dataclass
class AnalyzerConfig:
    slow_task_multiplier: float = 2.0
    gap_warning_ms: int = 500
    speed_variance_warn_ratio: float = 3.0
    top_n_slow_tasks: int = 3
    output_dir: str = "."
    generate_graphs: bool = True
    summary_only: bool = False
    quiet: bool = False
    bucket_seconds: int = 5
    health_window_seconds: int = 30


@dataclass
class Task:
    """Represents a single completed download task."""

    timestamp: datetime
    offset: int
    length: int
    duration_seconds: float

    @property
    def start_time(self) -> datetime:
        return self.timestamp - timedelta(seconds=self.duration_seconds)

    @property
    def speed_mbps(self) -> float:
        if self.duration_seconds <= 0:
            return 0.0
        return (self.length / MB) / self.duration_seconds


@dataclass
class WorkerStats:
    """Aggregated stats for a single worker."""

    worker_id: int
    start_time: Optional[datetime] = None
    end_time: Optional[datetime] = None
    tasks: list[Task] = field(default_factory=list)

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
        if self.wall_time <= 0:
            return 0.0
        return min(100.0, (self.total_work_time / self.wall_time) * 100)

    @property
    def idle_time(self) -> float:
        return max(0.0, self.wall_time - self.total_work_time)


@dataclass
class ReportContext:
    data: dict[str, Any]
    workers: list[WorkerStats]
    all_tasks: list[Task]
    global_avg_speed: float
    global_avg_task_duration: float
    speed_by_worker: dict[int, float]
    slow_tasks: list[tuple[int, Task]]


def parse_duration(duration_str: str) -> float:
    """
    Parse Go-style duration string to seconds.
    Supports: h, m, s, ms, µs/us, ns
    """
    duration_str = duration_str.strip()
    total_seconds = 0.0

    pattern = re.compile(r"(\d+\.?\d*)(ns|µs|us|ms|s|m|h)")
    matches = pattern.findall(duration_str)

    if not matches:
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


def parse_log_file(filename: str) -> dict:
    """Parse the debug.log file and extract relevant data."""
    try:
        with open(filename, "r", encoding="utf-8") as f:
            lines = f.readlines()
    except FileNotFoundError:
        print(f"❌ Error: File '{filename}' not found.")
        sys.exit(1)

    timestamp_re = re.compile(r"\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\]")
    worker_task_re = re.compile(r"Worker (\d+): Task offset=(\d+) length=(\d+) took (\S+)")
    worker_event_re = re.compile(r"Worker (\d+) (started|finished)")
    dl_complete_re = re.compile(r"Download .+ completed in (\S+) \(([^)]+)\)")
    probe_complete_re = re.compile(r"Probe complete - filename: (.+), size: (\d+)")
    balancer_split_re = re.compile(r"Balancer: split largest task \(total splits: (\d+)\)")
    health_kill_re = re.compile(r"Health: Worker (\d+) (stalled|slow)")

    workers: dict[int, WorkerStats] = {}
    balancer_splits: list[tuple[datetime, int]] = []
    health_kills: list[tuple[datetime, int, str]] = []
    download_info: dict[str, Any] = {}
    current_time: Optional[datetime] = None

    for line in lines:
        line = line.strip()

        ts_match = timestamp_re.match(line)
        if ts_match:
            try:
                current_time = datetime.strptime(ts_match.group(1), "%Y-%m-%d %H:%M:%S")
            except ValueError:
                pass

        if not current_time:
            continue

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

        task_match = worker_task_re.search(line)
        if task_match:
            wid = int(task_match.group(1))
            offset = int(task_match.group(2))
            length = int(task_match.group(3))
            duration = parse_duration(task_match.group(4))

            if wid not in workers:
                workers[wid] = WorkerStats(worker_id=wid)

            workers[wid].tasks.append(
                Task(
                    timestamp=current_time,
                    offset=offset,
                    length=length,
                    duration_seconds=duration,
                )
            )
            continue

        split_match = balancer_split_re.search(line)
        if split_match:
            total = int(split_match.group(1))
            balancer_splits.append((current_time, total))
            continue

        dl_match = dl_complete_re.search(line)
        if dl_match:
            download_info["total_duration"] = parse_duration(dl_match.group(1))
            download_info["avg_speed"] = dl_match.group(2)
            download_info["end_time"] = current_time
            continue

        probe_match = probe_complete_re.search(line)
        if probe_match:
            download_info["filename"] = probe_match.group(1)
            download_info["size"] = int(probe_match.group(2))
            continue

        health_match = health_kill_re.search(line)
        if health_match:
            wid = int(health_match.group(1))
            reason = health_match.group(2)
            health_kills.append((current_time, wid, reason))

    return {
        "workers": workers,
        "balancer_splits": balancer_splits,
        "health_kills": health_kills,
        "download_info": download_info,
    }


def parse_datetime(value: Optional[str]) -> Optional[datetime]:
    if not value:
        return None

    formats = [
        "%Y-%m-%d %H:%M:%S",
        "%Y-%m-%dT%H:%M:%S",
        "%Y-%m-%d",
    ]
    for fmt in formats:
        try:
            parsed = datetime.strptime(value, fmt)
            if fmt == "%Y-%m-%d":
                return parsed
            return parsed
        except ValueError:
            continue

    raise ValueError(
        "Invalid datetime format for --since/--until. Use 'YYYY-MM-DD' or 'YYYY-MM-DD HH:MM:SS'."
    )


def filter_data(
    data: dict[str, Any], worker_filter: Optional[int], since: Optional[datetime], until: Optional[datetime]
) -> dict[str, Any]:
    workers = data["workers"]

    filtered_workers: dict[int, WorkerStats] = {}
    for wid, worker in workers.items():
        if worker_filter is not None and wid != worker_filter:
            continue

        tasks = []
        for task in worker.tasks:
            if since and task.timestamp < since:
                continue
            if until and task.timestamp > until:
                continue
            tasks.append(task)

        start_time = worker.start_time
        end_time = worker.end_time
        if since and start_time and start_time < since:
            start_time = since
        if until and end_time and end_time > until:
            end_time = until

        filtered_workers[wid] = WorkerStats(
            worker_id=wid,
            start_time=start_time,
            end_time=end_time,
            tasks=tasks,
        )

    filtered_balancer_splits = []
    for ts, total in data["balancer_splits"]:
        if since and ts < since:
            continue
        if until and ts > until:
            continue
        filtered_balancer_splits.append((ts, total))

    filtered_health_kills = []
    for ts, wid, reason in data["health_kills"]:
        if worker_filter is not None and wid != worker_filter:
            continue
        if since and ts < since:
            continue
        if until and ts > until:
            continue
        filtered_health_kills.append((ts, wid, reason))

    return {
        "workers": filtered_workers,
        "balancer_splits": filtered_balancer_splits,
        "health_kills": filtered_health_kills,
        "download_info": data["download_info"],
    }


def print_header(title: str, char: str = "="):
    print(f"\n{char * 60}")
    print(f"  {title}")
    print(f"{char * 60}")


def build_report_context(data: dict[str, Any], cfg: AnalyzerConfig) -> Optional[ReportContext]:
    workers_map = data["workers"]
    workers = sorted(workers_map.values(), key=lambda w: w.worker_id)
    all_tasks = [t for w in workers for t in w.tasks]

    if not workers:
        return None

    total_bytes = sum(t.length for t in all_tasks)
    total_time = sum(t.duration_seconds for t in all_tasks)
    global_avg_speed = (total_bytes / MB) / total_time if total_time > 0 else 0.0
    global_avg_task_duration = total_time / len(all_tasks) if all_tasks else 0.0

    speed_by_worker = {w.worker_id: w.avg_speed_mbps for w in workers}

    slow_threshold = global_avg_task_duration * cfg.slow_task_multiplier
    slow_tasks: list[tuple[int, Task]] = []
    for w in workers:
        for t in w.tasks:
            if t.duration_seconds > slow_threshold:
                slow_tasks.append((w.worker_id, t))

    return ReportContext(
        data=data,
        workers=workers,
        all_tasks=all_tasks,
        global_avg_speed=global_avg_speed,
        global_avg_task_duration=global_avg_task_duration,
        speed_by_worker=speed_by_worker,
        slow_tasks=slow_tasks,
    )


def print_summary(ctx: ReportContext):
    download_info = ctx.data["download_info"]
    print_header("📥 DOWNLOAD SUMMARY")

    if "filename" in download_info:
        size_gb = download_info.get("size", 0) / GB
        print(f"  File:     {download_info['filename']}")
        print(f"  Size:     {size_gb:.2f} GB ({download_info.get('size', 0):,} bytes)")

    if "total_duration" in download_info:
        print(f"  Duration: {download_info['total_duration']:.2f}s")
        print(f"  Speed:    {download_info.get('avg_speed', 'N/A')}")

    print(f"  Workers:  {len(ctx.workers)}")
    print(f"  Total Tasks: {len(ctx.all_tasks)}")
    print(f"  Global Avg Speed: {ctx.global_avg_speed:.2f} MB/s")


def print_worker_table(ctx: ReportContext, cfg: AnalyzerConfig):
    print_header("🚀 WORKER PERFORMANCE BREAKDOWN")

    print(f"\n  Global Avg Speed: {ctx.global_avg_speed:.2f} MB/s")
    print(f"  Global Avg Task:  {ctx.global_avg_task_duration:.2f}s")
    print(f"  Total Tasks:      {len(ctx.all_tasks)}")

    print(f"\n  {'ID':>3} │ {'Tasks':>5} │ {'Avg Speed':>10} │ {'Util %':>7} │ {'Idle':>8} │ {'Status':<15}")
    print(f"  {'─'*3}─┼─{'─'*5}─┼─{'─'*10}─┼─{'─'*7}─┼─{'─'*8}─┼─{'─'*15}")

    for w in ctx.workers:
        task_count = len(w.tasks)
        avg_speed = w.avg_speed_mbps
        util = w.utilization
        idle = w.idle_time

        if task_count == 0:
            status = "NO TASKS ⚠️"
        elif idle > 5:
            status = f"IDLE {idle:.0f}s 💤"
        elif util < 50:
            status = "LOW UTIL ⚠️"
        elif avg_speed < (ctx.global_avg_speed / cfg.speed_variance_warn_ratio if ctx.global_avg_speed > 0 else 0):
            status = "SLOW 🐢"
        else:
            status = "OK ✅"

        util_str = f"{util:.2f}%" if util > 0 else "N/A"
        idle_str = f"{idle:.2f}s" if idle > 0 else "0s"
        speed_str = f"{avg_speed:.2f} MB/s" if avg_speed > 0 else "N/A"

        print(f"  {w.worker_id:>3} │ {task_count:>5} │ {speed_str:>10} │ {util_str:>7} │ {idle_str:>8} │ {status:<15}")


def print_speed_variance(ctx: ReportContext, cfg: AnalyzerConfig):
    print_header("⚡ SPEED VARIANCE ANALYSIS")

    active_speeds = {wid: s for wid, s in ctx.speed_by_worker.items() if s > 0}
    if not active_speeds:
        print("\n  No active workers with speed metrics.")
        return

    fastest_wid = max(active_speeds, key=active_speeds.get)
    slowest_wid = min(active_speeds, key=active_speeds.get)
    fastest_speed = active_speeds[fastest_wid]
    slowest_speed = active_speeds[slowest_wid]

    ratio = fastest_speed / slowest_speed if slowest_speed > 0 else 0

    print(f"\n  ⚡ Fastest: Worker {fastest_wid} @ {fastest_speed:.2f} MB/s")
    print(f"  🐢 Slowest: Worker {slowest_wid} @ {slowest_speed:.2f} MB/s")
    print(f"  📊 Ratio:   {ratio:.2f}x difference")

    if ratio >= cfg.speed_variance_warn_ratio:
        print(f"\n  ⚠️  WARNING: Speed variance is {ratio:.2f}x.")


def print_slow_tasks(ctx: ReportContext, cfg: AnalyzerConfig):
    print_header("🐌 SLOW TASK ANALYSIS")

    slow_threshold = ctx.global_avg_task_duration * cfg.slow_task_multiplier
    print(f"\n  Slow Task Threshold: > {slow_threshold:.2f}s ({cfg.slow_task_multiplier:.1f}x average)")

    if not ctx.slow_tasks:
        print("\n  ✅ No anomalously slow tasks detected.")
        return

    print(f"\n  Found {len(ctx.slow_tasks)} slow tasks:")
    print(f"\n  {'Worker':>6} │ {'Offset':>12} │ {'Size':>10} │ {'Duration':>10} │ {'Speed':>10}")
    print(f"  {'─'*6}─┼─{'─'*12}─┼─{'─'*10}─┼─{'─'*10}─┼─{'─'*10}")

    slow_tasks = sorted(ctx.slow_tasks, key=lambda x: x[1].duration_seconds, reverse=True)
    for wid, t in slow_tasks[:10]:
        offset_mb = t.offset / MB
        size_mb = t.length / MB
        print(
            f"  {wid:>6} │ {offset_mb:>10.2f}MB │ {size_mb:>8.2f}MB │ {t.duration_seconds:>8.2f}s │ {t.speed_mbps:>8.2f}MB/s"
        )

    if len(slow_tasks) > 10:
        print(f"\n  ... and {len(slow_tasks) - 10} more slow tasks")


def print_worker_details(ctx: ReportContext, cfg: AnalyzerConfig):
    print_header("📋 PER-WORKER TASK DETAILS")

    for w in ctx.workers:
        if not w.tasks:
            continue

        speeds = [t.speed_mbps for t in w.tasks]
        min_speed = min(speeds) if speeds else 0
        max_speed = max(speeds) if speeds else 0

        print(f"\n  ┌─ Worker {w.worker_id} ─────────────────────────────────────────")
        print(f"  │ Tasks: {len(w.tasks):<5}  Total Data: {w.total_bytes / MB:.2f} MB")
        print(f"  │ Speed: Min={min_speed:.2f}, Avg={w.avg_speed_mbps:.2f}, Max={max_speed:.2f} MB/s")
        print(f"  │ Wall Time: {w.wall_time:.2f}s  Work Time: {w.total_work_time:.2f}s  Idle: {w.idle_time:.2f}s")

        sorted_tasks = sorted(w.tasks, key=lambda t: t.duration_seconds, reverse=True)
        print("  │")
        print(f"  │ Top {cfg.top_n_slow_tasks} Slowest Tasks:")
        for i, t in enumerate(sorted_tasks[: cfg.top_n_slow_tasks], 1):
            print(f"  │   {i}. {t.duration_seconds:.2f}s @ {t.speed_mbps:.2f}MB/s (offset {t.offset / MB:.2f}MB)")

        print(f"  └{'─' * 50}")


def print_balancer_activity(ctx: ReportContext):
    balancer_splits = ctx.data["balancer_splits"]
    if not balancer_splits:
        return

    print_header("🔄 BALANCER ACTIVITY")

    total_splits = balancer_splits[-1][1]
    first_split_time = balancer_splits[0][0]
    last_split_time = balancer_splits[-1][0]

    print(f"\n  Total Splits: {total_splits}")
    split_duration = (last_split_time - first_split_time).total_seconds()
    print(f"  Split Window: {split_duration:.2f}s")
    splits_per_sec = len(balancer_splits) / split_duration if split_duration > 0 else 0
    print(f"  Split Rate:   {splits_per_sec:.2f} splits/sec")


def print_recommendations(ctx: ReportContext, cfg: AnalyzerConfig):
    print_header("💡 OPTIMIZATION RECOMMENDATIONS")

    recommendations = []
    active_speeds = {wid: s for wid, s in ctx.speed_by_worker.items() if s > 0}
    if active_speeds:
        min_speed = min(active_speeds.values())
        max_speed = max(active_speeds.values())
        ratio = (max_speed / min_speed) if min_speed > 0 else 0
        if ratio > cfg.speed_variance_warn_ratio:
            recommendations.append(
                f"HIGH SPEED VARIANCE ({ratio:.2f}x): Some workers are much slower; inspect network quality."
            )

    idle_workers = [w for w in ctx.workers if w.idle_time > 5]
    if idle_workers:
        recommendations.append(
            f"WORKER IDLE TIME: {len(idle_workers)} workers had >5s idle time; work stealing may need tuning."
        )

    low_util_workers = [w for w in ctx.workers if 0 < w.utilization < 70]
    if low_util_workers:
        recommendations.append(
            f"LOW UTILIZATION: {len(low_util_workers)} workers below 70% utilization."
        )

    balancer_splits = ctx.data["balancer_splits"]
    if balancer_splits and len(balancer_splits) > 30:
        recommendations.append(
            f"EXCESSIVE SPLITTING: {len(balancer_splits)} balancer splits; increase MinChunk."
        )

    if ctx.slow_tasks:
        recommendations.append(
            f"SLOW TASKS: {len(ctx.slow_tasks)} tasks took >{cfg.slow_task_multiplier:.1f}x average duration."
        )

    if recommendations:
        for i, rec in enumerate(recommendations, 1):
            print(f"\n  {i}. {rec}")
    else:
        print("\n  ✅ No major optimization issues detected. Download looks healthy!")


def ensure_output_dir(output_dir: str):
    Path(output_dir).mkdir(parents=True, exist_ok=True)


def generate_speed_graph(workers: dict[int, WorkerStats], output_file: str):
    if not HAS_MATPLOTLIB:
        print("\n⚠️  Skipping graph generation (matplotlib not available).")
        return

    all_tasks_with_time = []
    for w in workers.values():
        for t in w.tasks:
            all_tasks_with_time.append(
                {
                    "time": t.timestamp,
                    "speed": t.speed_mbps,
                    "worker_id": w.worker_id,
                    "size": t.length,
                }
            )

    if not all_tasks_with_time:
        print("\n⚠️  No task data available for graph.")
        return

    all_tasks_with_time.sort(key=lambda x: x["time"])

    times = [t["time"] for t in all_tasks_with_time]
    speeds = [t["speed"] for t in all_tasks_with_time]
    worker_ids = [t["worker_id"] for t in all_tasks_with_time]
    sizes = [t["size"] for t in all_tasks_with_time]

    window_size = min(10, max(3, len(speeds) // 5))
    rolling_avg = []
    for i in range(len(speeds)):
        start_idx = max(0, i - window_size + 1)
        window = speeds[start_idx : i + 1]
        rolling_avg.append(sum(window) / len(window))

    fig, ax = plt.subplots(figsize=(12, 6), dpi=100)
    fig.patch.set_facecolor("#1a1a2e")
    ax.set_facecolor("#16213e")

    unique_workers = sorted(set(worker_ids))
    colors = plt.cm.viridis([i / max(1, len(unique_workers) - 1) for i in range(len(unique_workers))])
    color_map = {wid: colors[i] for i, wid in enumerate(unique_workers)}

    max_size = max(sizes) if sizes else 1
    for wid in unique_workers:
        worker_times = [times[i] for i in range(len(times)) if worker_ids[i] == wid]
        worker_speeds = [speeds[i] for i in range(len(speeds)) if worker_ids[i] == wid]
        worker_sizes = [sizes[i] for i in range(len(sizes)) if worker_ids[i] == wid]
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

    ax.plot(times, rolling_avg, color="#ff6b6b", linewidth=2.5, label=f"Rolling Avg ({window_size} tasks)", zorder=10)

    overall_avg = sum(speeds) / len(speeds) if speeds else 0
    ax.axhline(
        y=overall_avg,
        color="#4ecdc4",
        linestyle="--",
        linewidth=1.5,
        label=f"Overall Avg: {overall_avg:.2f} MB/s",
        alpha=0.8,
    )

    ax.set_xlabel("Time", fontsize=12, color="white", fontweight="bold")
    ax.set_ylabel("Download Speed (MB/s)", fontsize=12, color="white", fontweight="bold")
    ax.set_title("Download Speed Over Time", fontsize=14, color="white", fontweight="bold", pad=20)

    ax.xaxis.set_major_formatter(mdates.DateFormatter("%H:%M:%S"))
    plt.xticks(rotation=45, ha="right")

    ax.grid(True, alpha=0.3, color="gray", linestyle="--")
    ax.legend(loc="upper right", facecolor="#0f3460", edgecolor="white", labelcolor="white", fontsize=9)

    ax.tick_params(colors="white")
    for spine in ax.spines.values():
        spine.set_color("white")
        spine.set_alpha(0.3)

    plt.tight_layout()
    plt.savefig(output_file, facecolor=fig.get_facecolor(), edgecolor="none", bbox_inches="tight", dpi=150)
    plt.close()

    print(f"\n📊 Speed graph saved to: {output_file}")


def generate_per_worker_speed_graph(
    workers: dict[int, WorkerStats], health_kills: list[tuple[datetime, int, str]], output_file: str
):
    if not HAS_MATPLOTLIB:
        print("\n⚠️  Skipping per-worker graph (matplotlib not available).")
        return

    active_workers = sorted([w for w in workers.values() if w.tasks], key=lambda w: w.worker_id)
    if not active_workers:
        print("\n⚠️  No worker data available for per-worker graph.")
        return

    num_workers = len(active_workers)
    cols = min(2, num_workers)
    rows = (num_workers + cols - 1) // cols

    fig, axes = plt.subplots(rows, cols, figsize=(7 * cols, 4 * rows), dpi=100, squeeze=False)
    fig.patch.set_facecolor("#1a1a2e")
    axes_flat = axes.flatten()

    all_speeds = [t.speed_mbps for w in active_workers for t in w.tasks]
    y_max = max(all_speeds) * 1.1 if all_speeds else 100

    for idx, worker in enumerate(active_workers):
        ax = axes_flat[idx]
        ax.set_facecolor("#16213e")

        sorted_tasks = sorted(worker.tasks, key=lambda t: t.timestamp)
        times = [t.timestamp for t in sorted_tasks]
        speeds = [t.speed_mbps for t in sorted_tasks]

        ax.plot(times, speeds, color="#4ecdc4", linewidth=1.5, marker="o", markersize=4, alpha=0.8, label="Speed")

        avg_speed = sum(speeds) / len(speeds) if speeds else 0
        ax.axhline(
            y=avg_speed,
            color="#ff6b6b",
            linestyle="--",
            linewidth=1.5,
            alpha=0.8,
            label=f"Avg: {avg_speed:.2f} MB/s",
        )

        ax.fill_between(times, speeds, alpha=0.2, color="#4ecdc4")
        ax.set_title(f"Worker {worker.worker_id}", fontsize=11, color="white", fontweight="bold")
        ax.set_ylabel("Speed (MB/s)", fontsize=9, color="white")
        ax.set_ylim(0, y_max)

        ax.xaxis.set_major_formatter(mdates.DateFormatter("%H:%M:%S"))
        ax.tick_params(axis="x", rotation=45, labelsize=8, colors="white")
        ax.tick_params(axis="y", labelsize=8, colors="white")
        ax.grid(True, alpha=0.3, color="gray", linestyle="--")
        ax.legend(loc="upper right", fontsize=8, facecolor="#0f3460", edgecolor="white", labelcolor="white")

        for spine in ax.spines.values():
            spine.set_color("white")
            spine.set_alpha(0.3)

        worker_kills = [(ts, reason) for ts, wid, reason in health_kills if wid == worker.worker_id]
        for kill_time, reason in worker_kills:
            color = "#ff4757" if reason == "stalled" else "#ffa502"
            ax.axvline(x=kill_time, color=color, linestyle="-", linewidth=2, alpha=0.8)
            ax.annotate(
                reason[0].upper(),
                xy=(kill_time, y_max * 0.95),
                fontsize=7,
                color=color,
                ha="center",
                fontweight="bold",
            )

    for idx in range(num_workers, len(axes_flat)):
        axes_flat[idx].set_visible(False)

    plt.suptitle("Per-Worker Download Speed", fontsize=14, color="white", fontweight="bold", y=1.02)
    plt.tight_layout()
    plt.savefig(output_file, facecolor=fig.get_facecolor(), edgecolor="none", bbox_inches="tight", dpi=150)
    plt.close()

    print(f"📊 Per-worker speed graph saved to: {output_file}")


def percentile(values: list[float], pct: float) -> float:
    if not values:
        return 0.0
    sorted_vals = sorted(values)
    k = (len(sorted_vals) - 1) * pct
    f = int(k)
    c = min(f + 1, len(sorted_vals) - 1)
    if f == c:
        return sorted_vals[f]
    return sorted_vals[f] + (k - f) * (sorted_vals[c] - sorted_vals[f])


def calc_speed_fairness(speed_by_worker: dict[int, float]) -> dict[str, float]:
    active = [s for s in speed_by_worker.values() if s > 0]
    if not active:
        return {"min_speed": 0.0, "max_speed": 0.0, "ratio": 0.0, "cv": 0.0}

    mean = sum(active) / len(active)
    variance = sum((s - mean) ** 2 for s in active) / len(active)
    stddev = variance**0.5
    min_speed = min(active)
    max_speed = max(active)
    ratio = (max_speed / min_speed) if min_speed > 0 else 0.0
    cv = (stddev / mean) if mean > 0 else 0.0
    return {
        "min_speed": min_speed,
        "max_speed": max_speed,
        "ratio": ratio,
        "cv": cv,
    }


def build_throughput_buckets(ctx: ReportContext, bucket_seconds: int) -> list[dict[str, Any]]:
    if bucket_seconds <= 0:
        bucket_seconds = 1
    if not ctx.all_tasks:
        return []

    sorted_tasks = sorted(ctx.all_tasks, key=lambda t: t.timestamp)
    base_ts = sorted_tasks[0].timestamp
    buckets: dict[int, int] = {}
    for task in sorted_tasks:
        delta = int((task.timestamp - base_ts).total_seconds())
        bucket_id = delta // bucket_seconds
        buckets[bucket_id] = buckets.get(bucket_id, 0) + task.length

    output = []
    for bucket_id in sorted(buckets.keys()):
        bytes_in_bucket = buckets[bucket_id]
        ts = base_ts + timedelta(seconds=bucket_id * bucket_seconds)
        mbps = (bytes_in_bucket / MB) / bucket_seconds
        output.append(
            {
                "bucket_start": ts.strftime("%Y-%m-%dT%H:%M:%S"),
                "bucket_seconds": bucket_seconds,
                "bytes": bytes_in_bucket,
                "throughput_mbps": mbps,
            }
        )
    return output


def compute_health_event_impact(ctx: ReportContext, window_seconds: int) -> list[dict[str, Any]]:
    impacts = []
    if window_seconds <= 0:
        window_seconds = 30

    health_events = ctx.data.get("health_kills", [])
    workers_by_id = {w.worker_id: w for w in ctx.workers}
    for event_time, worker_id, reason in health_events:
        worker = workers_by_id.get(worker_id)
        if not worker:
            continue

        before_start = event_time - timedelta(seconds=window_seconds)
        after_end = event_time + timedelta(seconds=window_seconds)

        before = [
            t.speed_mbps
            for t in worker.tasks
            if before_start <= t.timestamp < event_time
        ]
        after = [
            t.speed_mbps
            for t in worker.tasks
            if event_time <= t.timestamp <= after_end
        ]

        before_avg = sum(before) / len(before) if before else 0.0
        after_avg = sum(after) / len(after) if after else 0.0
        delta_pct = ((after_avg - before_avg) / before_avg * 100.0) if before_avg > 0 else 0.0

        impacts.append(
            {
                "time": event_time.strftime("%Y-%m-%dT%H:%M:%S"),
                "worker_id": worker_id,
                "reason": reason,
                "window_seconds": window_seconds,
                "before_avg_speed_mbps": before_avg,
                "after_avg_speed_mbps": after_avg,
                "delta_pct": delta_pct,
            }
        )
    return impacts


def print_advanced_metrics(ctx: ReportContext, cfg: AnalyzerConfig):
    print_header("📈 ADVANCED METRICS")

    task_speeds = [t.speed_mbps for t in ctx.all_tasks]
    task_durations = [t.duration_seconds for t in ctx.all_tasks]
    fairness = calc_speed_fairness(ctx.speed_by_worker)
    throughput = build_throughput_buckets(ctx, cfg.bucket_seconds)
    impacts = compute_health_event_impact(ctx, cfg.health_window_seconds)

    print(f"\n  Task Duration p50/p95/p99: {percentile(task_durations, 0.5):.2f}s / {percentile(task_durations, 0.95):.2f}s / {percentile(task_durations, 0.99):.2f}s")
    print(f"  Task Speed p50/p95/p99:    {percentile(task_speeds, 0.5):.2f} / {percentile(task_speeds, 0.95):.2f} / {percentile(task_speeds, 0.99):.2f} MB/s")
    print(f"  Worker Speed Ratio:        {fairness['ratio']:.2f}x")
    print(f"  Worker Speed CV:           {fairness['cv']:.2f}")

    if throughput:
        peak = max(throughput, key=lambda x: x["throughput_mbps"])
        print(f"  Peak Throughput:           {peak['throughput_mbps']:.2f} MB/s @ {peak['bucket_start']}")

    if impacts:
        print("\n  Health Event Impacts:")
        for item in impacts:
            print(
                f"  - Worker {item['worker_id']} ({item['reason']}) {item['before_avg_speed_mbps']:.2f} -> {item['after_avg_speed_mbps']:.2f} MB/s ({item['delta_pct']:.1f}%)"
            )


def build_json_report(ctx: ReportContext, cfg: AnalyzerConfig) -> dict[str, Any]:
    task_speeds = [t.speed_mbps for t in ctx.all_tasks]
    task_durations = [t.duration_seconds for t in ctx.all_tasks]
    fairness = calc_speed_fairness(ctx.speed_by_worker)
    throughput = build_throughput_buckets(ctx, cfg.bucket_seconds)
    health_impacts = compute_health_event_impact(ctx, cfg.health_window_seconds)

    workers = []
    for w in ctx.workers:
        wspeeds = [t.speed_mbps for t in w.tasks]
        workers.append(
            {
                "worker_id": w.worker_id,
                "task_count": len(w.tasks),
                "total_bytes": w.total_bytes,
                "total_work_time": round(w.total_work_time, 6),
                "wall_time": round(w.wall_time, 6),
                "idle_time": round(w.idle_time, 6),
                "utilization": round(w.utilization, 4),
                "avg_speed_mbps": round(w.avg_speed_mbps, 6),
                "speed_p50_mbps": round(percentile(wspeeds, 0.5), 6),
                "speed_p95_mbps": round(percentile(wspeeds, 0.95), 6),
            }
        )

    return {
        "schema_version": 2,
        "generated_at": datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ"),
        "config": {
            "slow_task_multiplier": cfg.slow_task_multiplier,
            "gap_warning_ms": cfg.gap_warning_ms,
            "speed_variance_warn_ratio": cfg.speed_variance_warn_ratio,
            "top_n_slow_tasks": cfg.top_n_slow_tasks,
            "bucket_seconds": cfg.bucket_seconds,
            "health_window_seconds": cfg.health_window_seconds,
        },
        "download_info": {
            **ctx.data["download_info"],
            "end_time": ctx.data["download_info"].get("end_time").strftime("%Y-%m-%dT%H:%M:%S")
            if ctx.data["download_info"].get("end_time")
            else None,
        },
        "summary": {
            "worker_count": len(ctx.workers),
            "task_count": len(ctx.all_tasks),
            "global_avg_speed_mbps": round(ctx.global_avg_speed, 6),
            "global_avg_task_duration_s": round(ctx.global_avg_task_duration, 6),
            "task_duration_p50_s": round(percentile(task_durations, 0.5), 6),
            "task_duration_p95_s": round(percentile(task_durations, 0.95), 6),
            "task_speed_p50_mbps": round(percentile(task_speeds, 0.5), 6),
            "task_speed_p95_mbps": round(percentile(task_speeds, 0.95), 6),
            "task_speed_p99_mbps": round(percentile(task_speeds, 0.99), 6),
            "slow_task_count": len(ctx.slow_tasks),
            "worker_speed_ratio": round(fairness["ratio"], 6),
            "worker_speed_cv": round(fairness["cv"], 6),
        },
        "workers": workers,
        "events": {
            "balancer_splits": [
                {"time": ts.strftime("%Y-%m-%dT%H:%M:%S"), "total": total}
                for ts, total in ctx.data["balancer_splits"]
            ],
            "health_kills": [
                {"time": ts.strftime("%Y-%m-%dT%H:%M:%S"), "worker_id": wid, "reason": reason}
                for ts, wid, reason in ctx.data["health_kills"]
            ],
        },
        "throughput_buckets": throughput,
        "health_event_impacts": health_impacts,
    }


def write_json_report(report: dict[str, Any], output_dir: str):
    path = Path(output_dir) / "analysis_report.json"
    with path.open("w", encoding="utf-8") as f:
        json.dump(report, f, indent=2)
    print(f"📄 JSON report saved to: {path}")


def write_html_report(report: dict[str, Any], output_dir: str):
    path = Path(output_dir) / "analysis_report.html"
    summary = report.get("summary", {})
    workers = report.get("workers", [])
    throughput = report.get("throughput_buckets", [])

    worker_rows = "\n".join(
        f"<tr><td>{w['worker_id']}</td><td>{w['task_count']}</td><td>{w['avg_speed_mbps']:.2f}</td><td>{w['utilization']:.2f}</td><td>{w['idle_time']:.2f}</td></tr>"
        for w in workers
    )
    throughput_rows = "\n".join(
        f"<tr><td>{b['bucket_start']}</td><td>{b['throughput_mbps']:.2f}</td><td>{b['bytes']}</td></tr>"
        for b in throughput
    )

    html = f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<title>Surge Analysis Report</title>
<style>
body {{ font-family: Helvetica, Arial, sans-serif; margin: 24px; color: #222; }}
h1, h2 {{ margin-bottom: 8px; }}
table {{ border-collapse: collapse; width: 100%; margin: 12px 0 24px; }}
th, td {{ border: 1px solid #ddd; padding: 8px; text-align: left; }}
th {{ background: #f5f7fb; }}
.cards {{ display: grid; grid-template-columns: repeat(4, minmax(120px, 1fr)); gap: 12px; }}
.card {{ background: #f5f7fb; border: 1px solid #dbe1ee; border-radius: 6px; padding: 10px; }}
.label {{ color: #5b6470; font-size: 12px; }}
.value {{ font-size: 22px; font-weight: 700; }}
</style>
</head>
<body>
  <h1>Surge Download Analysis</h1>
  <div class="cards">
    <div class="card"><div class="label">Workers</div><div class="value">{summary.get('worker_count', 0)}</div></div>
    <div class="card"><div class="label">Tasks</div><div class="value">{summary.get('task_count', 0)}</div></div>
    <div class="card"><div class="label">Avg Speed (MB/s)</div><div class="value">{summary.get('global_avg_speed_mbps', 0):.2f}</div></div>
    <div class="card"><div class="label">Slow Tasks</div><div class="value">{summary.get('slow_task_count', 0)}</div></div>
  </div>
  <h2>Worker Stats</h2>
  <table>
    <thead><tr><th>Worker</th><th>Tasks</th><th>Avg Speed (MB/s)</th><th>Utilization %</th><th>Idle (s)</th></tr></thead>
    <tbody>{worker_rows}</tbody>
  </table>
  <h2>Throughput Buckets</h2>
  <table>
    <thead><tr><th>Bucket Start</th><th>Throughput (MB/s)</th><th>Bytes</th></tr></thead>
    <tbody>{throughput_rows}</tbody>
  </table>
</body>
</html>
"""
    with path.open("w", encoding="utf-8") as f:
        f.write(html)
    print(f"📄 HTML report saved to: {path}")


def write_csv_reports(ctx: ReportContext, output_dir: str, bucket_seconds: int, health_window_seconds: int):
    per_worker_path = Path(output_dir) / "workers.csv"
    with per_worker_path.open("w", encoding="utf-8", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(
            [
                "worker_id",
                "task_count",
                "total_bytes",
                "total_work_time_s",
                "wall_time_s",
                "idle_time_s",
                "utilization_pct",
                "avg_speed_mbps",
            ]
        )
        for w in ctx.workers:
            writer.writerow(
                [
                    w.worker_id,
                    len(w.tasks),
                    w.total_bytes,
                    f"{w.total_work_time:.6f}",
                    f"{w.wall_time:.6f}",
                    f"{w.idle_time:.6f}",
                    f"{w.utilization:.4f}",
                    f"{w.avg_speed_mbps:.6f}",
                ]
            )

    tasks_path = Path(output_dir) / "tasks.csv"
    with tasks_path.open("w", encoding="utf-8", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(
            [
                "worker_id",
                "timestamp",
                "offset",
                "length",
                "duration_s",
                "speed_mbps",
            ]
        )
        for w in ctx.workers:
            for t in sorted(w.tasks, key=lambda x: x.timestamp):
                writer.writerow(
                    [
                        w.worker_id,
                        t.timestamp.strftime("%Y-%m-%dT%H:%M:%S"),
                        t.offset,
                        t.length,
                        f"{t.duration_seconds:.6f}",
                        f"{t.speed_mbps:.6f}",
                    ]
                )

    throughput = build_throughput_buckets(ctx, bucket_seconds)
    throughput_path = Path(output_dir) / "throughput.csv"
    with throughput_path.open("w", encoding="utf-8", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["bucket_start", "bucket_seconds", "bytes", "throughput_mbps"])
        for bucket in throughput:
            writer.writerow(
                [
                    bucket["bucket_start"],
                    bucket["bucket_seconds"],
                    bucket["bytes"],
                    f"{bucket['throughput_mbps']:.6f}",
                ]
            )

    impacts = compute_health_event_impact(ctx, health_window_seconds)
    impacts_path = Path(output_dir) / "health_impacts.csv"
    with impacts_path.open("w", encoding="utf-8", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(
            [
                "time",
                "worker_id",
                "reason",
                "window_seconds",
                "before_avg_speed_mbps",
                "after_avg_speed_mbps",
                "delta_pct",
            ]
        )
        for impact in impacts:
            writer.writerow(
                [
                    impact["time"],
                    impact["worker_id"],
                    impact["reason"],
                    impact["window_seconds"],
                    f"{impact['before_avg_speed_mbps']:.6f}",
                    f"{impact['after_avg_speed_mbps']:.6f}",
                    f"{impact['delta_pct']:.6f}",
                ]
            )

    print(f"📄 CSV reports saved to: {per_worker_path}, {tasks_path}, {throughput_path}, {impacts_path}")


def analyze_and_report(data: dict[str, Any], cfg: AnalyzerConfig, output_format: str = "text") -> int:
    ctx = build_report_context(data, cfg)
    if not ctx:
        print("❌ No worker data found in log file.")
        return 1

    if output_format in ("text", "all") and not cfg.quiet:
        print_summary(ctx)
        print_worker_table(ctx, cfg)
        print_speed_variance(ctx, cfg)
        print_slow_tasks(ctx, cfg)
        print_advanced_metrics(ctx, cfg)
        if not cfg.summary_only:
            print_worker_details(ctx, cfg)
            print_balancer_activity(ctx)
            print_recommendations(ctx, cfg)

    ensure_output_dir(cfg.output_dir)

    report: Optional[dict[str, Any]] = None
    if output_format in ("json", "all"):
        report = build_json_report(ctx, cfg)
        write_json_report(report, cfg.output_dir)

    if output_format in ("csv", "all"):
        write_csv_reports(ctx, cfg.output_dir, cfg.bucket_seconds, cfg.health_window_seconds)

    if output_format in ("html", "all"):
        if report is None:
            report = build_json_report(ctx, cfg)
        write_html_report(report, cfg.output_dir)

    if cfg.generate_graphs and output_format in ("text", "all"):
        if not cfg.quiet:
            print_header("📊 SPEED GRAPH GENERATION")
        generate_speed_graph(data["workers"], os.path.join(cfg.output_dir, "speed_graph.png"))
        generate_per_worker_speed_graph(
            data["workers"],
            health_kills=data.get("health_kills", []),
            output_file=os.path.join(cfg.output_dir, "worker_speeds.png"),
        )

    if output_format in ("text", "all") and not cfg.quiet:
        print("\n" + "=" * 60)

    return 0


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Surge debug.log analyzer")
    parser.add_argument("input", nargs="?", default="debug.log", help="Path to debug log file")
    parser.add_argument("--output-dir", default=".", help="Directory for generated reports/graphs")
    parser.add_argument(
        "--format",
        choices=["text", "json", "csv", "html", "all"],
        default="text",
        help="Output format",
    )
    parser.add_argument("--no-graphs", action="store_true", help="Disable PNG graph generation")
    parser.add_argument("--summary-only", action="store_true", help="Show summary sections only")
    parser.add_argument("--quiet", action="store_true", help="Suppress text report output")
    parser.add_argument("--worker", type=int, help="Analyze only a single worker id")
    parser.add_argument("--since", help="Filter events/tasks at or after this timestamp")
    parser.add_argument("--until", help="Filter events/tasks at or before this timestamp")
    parser.add_argument("--slow-task-multiplier", type=float, default=2.0, help="Slow task threshold multiplier")
    parser.add_argument("--gap-warning-ms", type=int, default=500, help="Warn when inter-task gap exceeds this ms")
    parser.add_argument(
        "--speed-variance-warn-ratio",
        type=float,
        default=3.0,
        help="Warn if fastest worker exceeds slowest by this ratio",
    )
    parser.add_argument("--top-n-slow-tasks", type=int, default=3, help="Show top N slow tasks per worker")
    parser.add_argument("--bucket-seconds", type=int, default=5, help="Throughput bucket size in seconds")
    parser.add_argument(
        "--health-window-seconds",
        type=int,
        default=30,
        help="Window size for before/after health-event speed impact",
    )
    return parser


def main() -> int:
    parser = build_arg_parser()
    args = parser.parse_args()

    try:
        since = parse_datetime(args.since)
        until = parse_datetime(args.until)
    except ValueError as exc:
        print(f"❌ {exc}")
        return 2

    if since and until and since > until:
        print("❌ --since must be earlier than or equal to --until")
        return 2
    if args.bucket_seconds <= 0:
        print("❌ --bucket-seconds must be > 0")
        return 2
    if args.health_window_seconds <= 0:
        print("❌ --health-window-seconds must be > 0")
        return 2

    cfg = AnalyzerConfig(
        slow_task_multiplier=args.slow_task_multiplier,
        gap_warning_ms=args.gap_warning_ms,
        speed_variance_warn_ratio=args.speed_variance_warn_ratio,
        top_n_slow_tasks=args.top_n_slow_tasks,
        output_dir=args.output_dir,
        generate_graphs=not args.no_graphs,
        summary_only=args.summary_only,
        quiet=args.quiet,
        bucket_seconds=args.bucket_seconds,
        health_window_seconds=args.health_window_seconds,
    )

    if not cfg.quiet:
        print("\n🔍 Surge Log Analyzer - Verbose Mode")
        print(f"   Analyzing: {args.input}")

    data = parse_log_file(args.input)
    data = filter_data(data, worker_filter=args.worker, since=since, until=until)
    return analyze_and_report(data, cfg, output_format=args.format)


if __name__ == "__main__":
    sys.exit(main())
