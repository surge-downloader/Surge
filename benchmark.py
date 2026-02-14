#!/usr/bin/env python3
"""
Benchmark script to compare Surge against other download tools:
- aria2c
- wget
- curl
- axel
"""

import argparse
import json
import os
import platform
import shutil
import subprocess
import statistics
import tempfile
import time
import random
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Optional, List, Callable
from urllib.request import Request, urlopen

# =============================================================================
# CONSTANTS & CONFIG
# =============================================================================
IS_WINDOWS = platform.system() == "Windows"
EXE_SUFFIX = ".exe" if IS_WINDOWS else ""
MB = 1024 * 1024
DEFAULT_TEST_URL = "https://sin-speed.hetzner.com/1GB.bin"

# =============================================================================
# DATA CLASSES
# =============================================================================
@dataclass
class BenchmarkResult:
    """Result of a single benchmark run."""
    tool: str
    success: bool
    elapsed_seconds: float
    file_size_bytes: int
    error: Optional[str] = None
    iter_results: Optional[List[float]] = None

    @property
    def speed_mbps(self) -> float:
        if self.elapsed_seconds <= 0:
            return 0.0
        return (self.file_size_bytes / MB) / self.elapsed_seconds


# =============================================================================
# UTILITIES
# =============================================================================
def run_command(
    cmd: List[str],
    cwd: Optional[str] = None,
    timeout: int = 3600,
    env: Optional[dict] = None,
) -> tuple[bool, str]:
    """Run a command and return (success, output)."""
    try:
        result = subprocess.run(
            cmd,
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=timeout,
            shell=False,
            env=env,
        )
        return result.returncode == 0, result.stdout + result.stderr
    except subprocess.TimeoutExpired:
        return False, "Command timed out"
    except FileNotFoundError as e:
        return False, f"Command not found: {e}"
    except Exception as e:
        return False, str(e)


def get_file_size(path: Path) -> int:
    return path.stat().st_size if path.exists() else 0


def cleanup_file(path: Path):
    try:
        if path.exists():
            path.unlink()
    except OSError:
        pass


def parse_go_duration(s: str) -> float:
    """Parse Go duration string (e.g., '1m30s', '500ms') to seconds."""
    total = 0.0
    matches = re.findall(r'(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)', s)
    multipliers = {
        'ns': 1e-9, 'us': 1e-6, 'µs': 1e-6, 'ms': 1e-3,
        's': 1, 'm': 60, 'h': 3600
    }
    for val, unit in matches:
        total += float(val) * multipliers.get(unit, 0)
    return total


def probe_content_length(url: str, timeout: int = 15) -> Optional[int]:
    """Best-effort probe of expected file size using HTTP HEAD."""
    try:
        req = Request(url, method="HEAD")
        with urlopen(req, timeout=timeout) as resp:
            value = resp.headers.get("Content-Length")
            if not value:
                return None
            size = int(value)
            return size if size > 0 else None
    except Exception:
        return None


def make_isolated_surge_env(base_dir: Path, max_connections: int, min_chunk_size_mb: int) -> dict:
    """Create an isolated Surge config so local user settings don't affect benchmark runs."""
    env = os.environ.copy()
    system = platform.system()

    # Resolve Surge config dir for each platform from env vars Surge uses.
    if system == "Windows":
        appdata = base_dir / "appdata"
        tmp_dir = base_dir / "tmp"
        appdata.mkdir(parents=True, exist_ok=True)
        tmp_dir.mkdir(parents=True, exist_ok=True)
        env["APPDATA"] = str(appdata)
        env["TEMP"] = str(tmp_dir)
        env["TMP"] = str(tmp_dir)
        surge_dir = appdata / "surge"
    elif system == "Darwin":
        home_dir = base_dir / "home"
        tmp_dir = base_dir / "tmp"
        home_dir.mkdir(parents=True, exist_ok=True)
        tmp_dir.mkdir(parents=True, exist_ok=True)
        env["HOME"] = str(home_dir)
        env["TMPDIR"] = str(tmp_dir)
        surge_dir = home_dir / "Library" / "Application Support" / "surge"
    else:
        xdg_config = base_dir / "xdg_config"
        xdg_state = base_dir / "xdg_state"
        xdg_runtime = base_dir / "xdg_runtime"
        xdg_config.mkdir(parents=True, exist_ok=True)
        xdg_state.mkdir(parents=True, exist_ok=True)
        xdg_runtime.mkdir(parents=True, exist_ok=True)
        env["XDG_CONFIG_HOME"] = str(xdg_config)
        env["XDG_STATE_HOME"] = str(xdg_state)
        env["XDG_RUNTIME_DIR"] = str(xdg_runtime)
        surge_dir = xdg_config / "surge"

    surge_dir.mkdir(parents=True, exist_ok=True)
    settings_path = surge_dir / "settings.json"
    settings = {
        "network": {
            "max_connections_per_host": max(1, max_connections),
            "max_concurrent_downloads": 1,
            "min_chunk_size": max(1, min_chunk_size_mb) * MB,
        }
    }
    settings_path.write_text(json.dumps(settings, indent=2), encoding="utf-8")
    return env

def print_box_header(title: str, width: int = 60):
    print(f"┌{'─' * (width - 2)}┐")
    print(f"│ {title:<{width - 4}} │")
    print(f"└{'─' * (width - 2)}┘")

# =============================================================================
# SETUP & BUILD
# =============================================================================
def build_surge(project_dir: Path) -> bool:
    print("  [>] Building surge...")
    output_name = f"surge{EXE_SUFFIX}"
    success, output = run_command(["go", "build", "-o", output_name, "."], cwd=str(project_dir))
    if not success:
        print(f"  [!] Failed to build surge: {output.strip()}")
        return False
    print("  [+] Surge built successfully")
    return True


def check_tool(name: str) -> bool:
    """Check if a tool is in the PATH."""
    if shutil.which(name):
        return True
    print(f"  [!] {name} not found in PATH")
    return False


# =============================================================================
# BENCHMARK ENGINE
# =============================================================================
def benchmark_surge(
    executable: Path,
    url: str,
    output_dir: Path,
    label: str = "surge",
    timing_mode: str = "external",
    env: Optional[dict] = None,
) -> BenchmarkResult:
    """Specialized benchmark for Surge to parse internal duration."""
    if not executable.exists():
        return BenchmarkResult(label, False, 0, 0, f"Binary not found: {executable}")
    
    # Clean potential previous runs
    for f in output_dir.glob("*"):
        if f.is_file(): 
            cleanup_file(f)

    start = time.perf_counter()
    success, output = run_command([
        str(executable), "server", "start", url,
        "--output", str(output_dir),
        "--exit-when-done"
    ], timeout=3600, env=env)
    elapsed = time.perf_counter() - start
    
    # Parse internal time if available
    actual_time = elapsed
    if timing_mode == "internal":
        match = re.search(r"Completed: .*? \[.*?\] \(in (.*?)\)", output)
        if match:
            try:
                t = parse_go_duration(match.group(1))
                if t > 0:
                    actual_time = t
            except ValueError:
                pass

    # Find the downloaded file
    downloaded_files = [f for f in output_dir.iterdir() if f.is_file()]
    file_size = max((get_file_size(f) for f in downloaded_files), default=0)
    
    # Cleanup
    for f in downloaded_files:
        cleanup_file(f)

    if not success:
        return BenchmarkResult(label, False, actual_time, file_size, output[:200])
    
    return BenchmarkResult(label, True, actual_time, file_size)


def benchmark_standard_tool(
    name: str, 
    bin_name: str, 
    cmd_builder: Callable[[str, Path, str], List[str]], 
    url: str, 
    output_dir: Path
) -> BenchmarkResult:
    """Generic benchmark runner for standard tools (wget, curl, etc)."""
    
    binary = shutil.which(bin_name)
    if not binary:
        return BenchmarkResult(name, False, 0, 0, f"{bin_name} not installed")
    
    output_file = output_dir / f"{name}_dl"
    cleanup_file(output_file)
    # Cleanup aux files (like .st for axel)
    cleanup_file(output_dir / f"{output_file.name}.st")

    cmd = cmd_builder(binary, output_file, url)
    
    start = time.perf_counter()
    success, output = run_command(cmd)
    elapsed = time.perf_counter() - start
    
    file_size = get_file_size(output_file)
    cleanup_file(output_file)
    cleanup_file(output_dir / f"{output_file.name}.st")

    if not success:
        return BenchmarkResult(name, False, elapsed, file_size, output[:200])
    
    return BenchmarkResult(name, True, elapsed, file_size)


# =============================================================================
# TOOL CONFIGURATIONS
# =============================================================================
def make_cmd_aria2(connections: int, min_split_size: str, no_conf: bool, file_allocation: str) -> Callable[[str, Path, str], List[str]]:
    def cmd(binary: str, out: Path, url: str) -> List[str]:
        c = max(1, connections)
        args = [
            binary,
            "-x", str(c),
            "-s", str(c),
            "--min-split-size", min_split_size,
            "--file-allocation", file_allocation,
            "-o", out.name,
            "-d", str(out.parent),
            "--allow-overwrite=true",
            "--summary-interval=0",
            "--console-log-level=warn",
        ]
        if no_conf:
            args.append("--no-conf")
        args.append(url)
        return args
    return cmd

def cmd_axel(binary: str, out: Path, url: str) -> List[str]:
    return [binary, "-n", "16", "-q", "-o", str(out), url]

def cmd_wget(binary: str, out: Path, url: str) -> List[str]:
    return [binary, "-q", "-O", str(out), url]

def cmd_curl(binary: str, out: Path, url: str) -> List[str]:
    return [binary, "-s", "-L", "-o", str(out), url]


# =============================================================================
# REPORTING
# =============================================================================
def print_results(results: List[BenchmarkResult]):
    # Header
    print("\n")
    print("┌──────────────────────┬──────────┬──────────────┬──────────────┬──────────────┐")
    print(f"│ {'Tool':<20} │ {'Status':<8} │ {'Avg Time':<12} │ {'Avg Speed':<12} │ {'Size':<12} │")
    print("├──────────────────────┼──────────┼──────────────┼──────────────┼──────────────┤")
    
    for r in results:
        status = "OK" if r.success else "FAIL"
        time_str = f"{r.elapsed_seconds:.2f}s" if r.elapsed_seconds > 0 else "-"
        speed_str = f"{r.speed_mbps:.2f} MB/s" if r.success and r.speed_mbps > 0 else "-"
        size_str = f"{r.file_size_bytes / MB:.1f} MB" if r.file_size_bytes > 0 else "-"
        
        print(f"│ {r.tool:<20} │ {status:<8} │ {time_str:<12} │ {speed_str:<12} │ {size_str:<12} │")
        
    print("└──────────────────────┴──────────┴──────────────┴──────────────┴──────────────┘")

    iter_stats = [r for r in results if r.iter_results and len(r.iter_results) > 1]
    if iter_stats:
        print("\n[ STATS ]")
        for r in iter_stats:
            median_t = statistics.median(r.iter_results)
            std_t = statistics.pstdev(r.iter_results)
            print(f"  {r.tool:<20} median={median_t:.2f}s stddev={std_t:.2f}s n={len(r.iter_results)}")

    # Errors
    failures = [r for r in results if not r.success and r.error]
    if failures:
        print("\n[ ERRORS ]")
        for f in failures:
            print(f"  > {f.tool}: {f.error.strip()[:80]}...")

    # Winner
    successful = [r for r in results if r.success and r.speed_mbps > 0]
    if successful:
        winner = max(successful, key=lambda r: r.speed_mbps)
        print(f"\n[ SUMMARY ]")
        print(f"  Fastest: {winner.tool}")
        print(f"  Speed:   {winner.speed_mbps:.2f} MB/s")


def print_histogram(results: List[BenchmarkResult]):
    successful = sorted([r for r in results if r.success and r.speed_mbps > 0], 
                        key=lambda r: r.speed_mbps, reverse=True)
    if not successful: return

    print("\n[ SPEED VISUALIZATION ]")
    max_speed = successful[0].speed_mbps
    width = 50
    
    for r in successful:
        bar_len = int((r.speed_mbps / max_speed) * width)
        bar = "#" * bar_len
        print(f"  {r.tool:<15} │ {bar:<{width}} {r.speed_mbps:.2f} MB/s")
    print()


def print_head_to_head(tasks: List[tuple[str, Callable[[], BenchmarkResult]]], raw_results: dict[int, List[BenchmarkResult]]):
    """Print direct iteration-level comparison between Surge and aria2."""
    surge_idx = None
    aria_idx = None
    for idx, (name, _) in enumerate(tasks):
        if name == "Surge (Current)":
            surge_idx = idx
        elif name == "aria2":
            aria_idx = idx

    if surge_idx is None or aria_idx is None:
        return

    surge_runs = raw_results.get(surge_idx, [])
    aria_runs = raw_results.get(aria_idx, [])
    if not surge_runs or not aria_runs:
        return

    comparable = []
    for s, a in zip(surge_runs, aria_runs):
        if s.success and a.success and s.elapsed_seconds > 0 and a.elapsed_seconds > 0:
            comparable.append((s.elapsed_seconds, a.elapsed_seconds))

    if not comparable:
        return

    wins = sum(1 for s, a in comparable if s < a)
    losses = sum(1 for s, a in comparable if s > a)
    ties = len(comparable) - wins - losses
    speedups = [a / s for s, a in comparable]
    median_speedup = statistics.median(speedups)
    win_rate = wins / len(comparable)

    print("[ SURGE vs ARIA2 ]")
    print(f"  Comparable iterations: {len(comparable)}")
    print(f"  Surge wins/losses/ties: {wins}/{losses}/{ties}")
    print(f"  Median speedup (aria2_time/surge_time): {median_speedup:.2f}x")
    if len(comparable) >= 5:
        if win_rate >= 0.8 and median_speedup >= 1.05:
            print("  Verdict: Strong evidence Surge is faster under this setup.")
        elif win_rate > 0.5 and median_speedup > 1.0:
            print("  Verdict: Moderate evidence Surge is faster under this setup.")
        else:
            print("  Verdict: Inconclusive or aria2 is competitive under this setup.")
    else:
        print("  Verdict: Increase iterations (>=5) for a stronger claim.")


# =============================================================================
# MAIN
# =============================================================================
def main():
    parser = argparse.ArgumentParser(description="Surge Benchmark Suite")
    parser.add_argument("url", nargs="?", default=DEFAULT_TEST_URL, help="URL to benchmark")
    parser.add_argument("-n", "--iterations", type=int, default=1, help="Iterations per tool")
    parser.add_argument("--warmups", type=int, default=0, help="Warmup rounds per tool (not counted)")
    parser.add_argument("--surge-exec", type=Path, help="Specific Surge binary")
    parser.add_argument("--surge-baseline", type=Path, help="Baseline Surge binary for comparison")
    parser.add_argument("--mirror-suite", action="store_true", help="Run multi-mirror test suite")
    parser.add_argument("--speedtest", action="store_true", help="Run network speedtest-cli")
    parser.add_argument("--connections", type=int, default=16, help="Shared connection target for Surge and aria2")
    parser.add_argument("--surge-max-connections", type=int, help="Override Surge max connections per host")
    parser.add_argument("--surge-min-chunk-mb", type=int, default=2, help="Surge min chunk size in MB for isolated config")
    parser.add_argument("--surge-timing", choices=["external", "internal"], default="external",
                        help="How to time Surge runs (external wall-clock is fair/default)")
    parser.add_argument("--isolate-surge", action=argparse.BooleanOptionalAction, default=True,
                        help="Use isolated Surge config during benchmark")
    parser.add_argument("--aria2-connections", type=int, help="Override aria2 split/connection count")
    parser.add_argument("--aria2-min-split-size", default="20M", help="aria2 --min-split-size (e.g., 4M, 20M)")
    parser.add_argument("--aria2-file-allocation", choices=["none", "prealloc", "trunc", "falloc"], default="prealloc",
                        help="aria2 file allocation mode")
    parser.add_argument("--aria2-no-conf", action=argparse.BooleanOptionalAction, default=True,
                        help="Run aria2 with --no-conf for reproducibility")
    parser.add_argument("--strict-size", action=argparse.BooleanOptionalAction, default=True,
                        help="Fail a run if downloaded size != expected Content-Length")
    
    # Tool flags
    for tool in ["surge", "aria2", "wget", "curl", "axel"]:
        parser.add_argument(f"--{tool}", action="store_true", help=f"Run {tool} benchmark")

    args = parser.parse_args()
    
    # Configuration
    num_iterations = args.iterations
    if num_iterations < 1:
        print("  [!] Iterations must be >= 1")
        return
    if args.warmups < 0:
        print("  [!] Warmups must be >= 0")
        return

    project_dir = Path(__file__).parent.resolve()
    temp_dir = Path(tempfile.mkdtemp(prefix="surge_bench_"))
    download_dir = temp_dir / "downloads"
    download_dir.mkdir(parents=True, exist_ok=True)
    expected_size = None if args.mirror_suite else probe_content_length(args.url)
    shared_connections = max(1, args.connections)
    surge_connections = max(1, args.surge_max_connections or shared_connections)
    aria2_connections = max(1, args.aria2_connections or shared_connections)
    surge_env = make_isolated_surge_env(temp_dir / "surge_env", surge_connections, args.surge_min_chunk_mb) if args.isolate_surge else None

    print_box_header("SURGE BENCHMARK SUITE")
    print(f"  Work Dir: {temp_dir}")
    print(f"  Target:   {args.url[:60]}")
    if len(args.url) > 60: print(f"            {args.url[60:]}")
    print(f"  Timing:   Surge={args.surge_timing} (recommended: external)")
    print(f"  Conns:    Surge={surge_connections} | aria2={aria2_connections}")
    print(f"  Aria2:    min-split-size={args.aria2_min_split_size}, file-allocation={args.aria2_file_allocation}, no-conf={args.aria2_no_conf}")
    if expected_size:
        print(f"  Expected: {expected_size / MB:.1f} MB (from HTTP Content-Length)")
    else:
        print("  Expected: Unknown size (Content-Length unavailable)")
    print()

    # Determine tasks
    tasks: List[tuple[str, Callable[[], BenchmarkResult]]] = []
    
    # 1. Setup Surge
    surge_bin = None
    if args.surge_exec and args.surge_exec.exists():
        surge_bin = args.surge_exec.resolve()
        print(f"  [+] Using provided Surge: {surge_bin.name}")
    elif shutil.which("go") and build_surge(project_dir):
        surge_bin = project_dir / f"surge{EXE_SUFFIX}"
    
    # Define generic tools to check
    # (flag_name, bin_name, cmd_func)
    aria2_cmd = make_cmd_aria2(
        connections=aria2_connections,
        min_split_size=args.aria2_min_split_size,
        no_conf=args.aria2_no_conf,
        file_allocation=args.aria2_file_allocation,
    )

    standard_tools = [
        ("aria2", "aria2c", aria2_cmd),
        ("axel", "axel", cmd_axel),
        ("wget", "wget", cmd_wget),
        ("curl", "curl", cmd_curl)
    ]

    # Mirror Suite Logic
    if args.mirror_suite:
        urls = [
            "https://distrib-coffee.ipsl.jussieu.fr/pub/linux/ubuntu-releases/noble/ubuntu-24.04.3-desktop-amd64.iso",
            "https://mirror.cedia.org.ec/ubuntu-releases/24.04.3/ubuntu-24.04.3-desktop-amd64.iso",
            "https://mirror.bharatdatacenter.com/ubuntu-releases/noble/ubuntu-24.04.3-desktop-amd64.iso",
        ]
        if surge_bin:
            tasks.append(("Surge (3 Mirrors)", lambda: benchmark_surge(surge_bin, ",".join(urls), download_dir, "Surge (3 Mirrors)", args.surge_timing, surge_env)))
            tasks.append(("Surge (2 Mirrors)", lambda: benchmark_surge(surge_bin, ",".join(urls[:2]), download_dir, "Surge (2 Mirrors)", args.surge_timing, surge_env)))
            tasks.append(("Surge (1 Mirror)", lambda: benchmark_surge(surge_bin, urls[0], download_dir, "Surge (1 Mirror)", args.surge_timing, surge_env)))
        if args.surge_baseline and args.surge_baseline.exists():
             tasks.append(("Surge Baseline", lambda: benchmark_surge(args.surge_baseline, urls[0], download_dir, "Surge Baseline", args.surge_timing, surge_env)))

    else:
        # Standard Single URL Logic
        specific_request = any(getattr(args, t) for t in ["surge", "aria2", "wget", "curl", "axel"]) or args.surge_exec or args.surge_baseline
        run_all = not specific_request

        # Surge tasks
        if (run_all or args.surge) and surge_bin:
            tasks.append(("Surge (Current)", lambda: benchmark_surge(surge_bin, args.url, download_dir, "Surge (Current)", args.surge_timing, surge_env)))
        
        if args.surge_baseline and args.surge_baseline.exists():
            tasks.append(("Surge (Baseline)", lambda: benchmark_surge(args.surge_baseline, args.url, download_dir, "Surge (Baseline)", args.surge_timing, surge_env)))

        # Standard tool tasks
        for name, bin_name, func in standard_tools:
            if (run_all or getattr(args, name)) and check_tool(bin_name):
                # Capture variables in lambda default args to avoid closure binding issues
                tasks.append((name, lambda n=name, b=bin_name, f=func: benchmark_standard_tool(n, b, f, args.url, download_dir)))

    if not tasks:
        print("\n  [!] No benchmarks to run. Check installed tools or arguments.")
        shutil.rmtree(temp_dir)
        return

    # Speedtest
    if args.speedtest and shutil.which("speedtest-cli"):
        print("\n  [>] Running network baseline...")
        _, out = run_command(["speedtest-cli", "--simple"], timeout=60)
        for line in out.strip().split('\n'):
            print(f"      {line}")

    # Execution Loop
    def enforce_size(result: BenchmarkResult) -> BenchmarkResult:
        if expected_size is None or not result.success:
            return result
        if result.file_size_bytes != expected_size:
            msg = f"size mismatch: got {result.file_size_bytes} bytes, expected {expected_size}"
            if args.strict_size:
                result.success = False
            result.error = msg
        return result

    if args.warmups > 0:
        print(f"\n  [>] Warmup: {len(tasks)} benchmarks x {args.warmups} rounds (not measured)")
        for _ in range(args.warmups):
            indexed_tasks = list(enumerate(tasks))
            random.shuffle(indexed_tasks)
            for _, (_, task_func) in indexed_tasks:
                _ = enforce_size(task_func())
                time.sleep(1)

    print(f"\n  [>] Running {len(tasks)} benchmarks x {num_iterations} iterations")
    print("  " + "─" * 40)
    
    raw_results = {i: [] for i in range(len(tasks))} # Store by task index

    try:
        for i in range(num_iterations):
            # Create a list of (index, task_func) and shuffle it
            indexed_tasks = list(enumerate(tasks))
            random.shuffle(indexed_tasks)

            for task_idx, (_, task_func) in indexed_tasks:
                res = enforce_size(task_func())
                raw_results[task_idx].append(res)
                
                status_icon = "+" if res.success else "!"
                status_txt = f"{res.elapsed_seconds:.2f}s" if res.success else "FAIL"
                
                # Print progress
                sys.stdout.write(f"  [{status_icon}] {res.tool:<25} : {status_txt}\n")
                
                time.sleep(2) # Cooldown

        # Aggregate
        final_results = []
        for i in range(len(tasks)):
            runs = raw_results[i]
            successful = [r for r in runs if r.success]
            
            if not successful:
                final_results.append(BenchmarkResult(runs[0].tool, False, 0, 0, runs[-1].error))
                continue
            
            # Simple average
            avg_time = sum(r.elapsed_seconds for r in successful) / len(successful)
            file_size = successful[0].file_size_bytes
            
            final_results.append(BenchmarkResult(
                successful[0].tool, True, avg_time, file_size, iter_results=[r.elapsed_seconds for r in successful]
            ))

        print_results(final_results)
        print_histogram(final_results)
        print_head_to_head(tasks, raw_results)

    finally:
        print("  [>] Cleaning up...")
        shutil.rmtree(temp_dir, ignore_errors=True)
        print("  [+] Done.")

if __name__ == "__main__":
    main()
