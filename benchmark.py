#!/usr/bin/env python3
"""
Benchmark script to compare Surge against other download tools:
- aria2c
- wget
- curl
- axel
"""

import argparse
import platform
import shutil
import subprocess
import tempfile
import time
import random
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Optional, List, Callable

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
def run_command(cmd: List[str], cwd: Optional[str] = None, timeout: int = 3600) -> tuple[bool, str]:
    """Run a command and return (success, output)."""
    try:
        result = subprocess.run(
            cmd,
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=timeout,
            shell=IS_WINDOWS,
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


# =============================================================================
# SETUP & BUILD
# =============================================================================
def build_surge(project_dir: Path) -> bool:
    print("  Building surge...")
    output_name = f"surge{EXE_SUFFIX}"
    success, output = run_command(["go", "build", "-o", output_name, "."], cwd=str(project_dir))
    if not success:
        print(f"    [X] Failed to build surge: {output.strip()}")
        return False
    print("    [OK] Surge built successfully")
    return True


def check_tool(name: str) -> bool:
    """Check if a tool is in the PATH."""
    if shutil.which(name):
        print(f"    [OK] {name} found")
        return True
    print(f"    [X] {name} not found")
    return False


# =============================================================================
# BENCHMARK ENGINE
# =============================================================================
def benchmark_surge(executable: Path, url: str, output_dir: Path, label: str = "surge") -> BenchmarkResult:
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
    ])
    elapsed = time.perf_counter() - start
    
    # Parse internal time if available
    actual_time = elapsed
    match = re.search(r"Completed: .*? \[.*?\] \(in (.*?)\)", output)
    if match:
        try:
            t = parse_go_duration(match.group(1))
            if t > 0: actual_time = t
        except ValueError:
            pass

    # Find the downloaded file (ignoring the surge binary if it's there)
    # Surge preserves original filenames, so we scan for the largest file.
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
def cmd_aria2(binary: str, out: Path, url: str) -> List[str]:
    return [
        binary, "-x", "16", "-s", "16", "-o", out.name, "-d", str(out.parent),
        "--allow-overwrite=true", "--console-log-level=warn", url
    ]

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
    print("\n" + "=" * 80)
    print(f"  {'Tool':<20} │ {'Status':<8} │ {'Avg Time':<10} │ {'Avg Speed':<12} │ {'Size':<10}")
    print(f"  {'─'*20}─┼─{'─'*8}─┼─{'─'*10}─┼─{'─'*12}─┼─{'─'*10}")
    
    for r in results:
        status = "OK" if r.success else "FAIL"
        time_str = f"{r.elapsed_seconds:.2f}s" if r.elapsed_seconds > 0 else "-"
        speed_str = f"{r.speed_mbps:.2f} MB/s" if r.success and r.speed_mbps > 0 else "-"
        size_str = f"{r.file_size_bytes / MB:.1f} MB" if r.file_size_bytes > 0 else "-"
        
        print(f"  {r.tool:<20} │ {status:<8} │ {time_str:<10} │ {speed_str:<12} │ {size_str:<10}")
        
        if not r.success and r.error:
            print(f"    └─ Error: {r.error.strip()[:80]}...")

    # Winner
    successful = [r for r in results if r.success and r.speed_mbps > 0]
    if successful:
        winner = max(successful, key=lambda r: r.speed_mbps)
        print("-" * 80)
        print(f"  🏆 WINNER: {winner.tool} @ {winner.speed_mbps:.2f} MB/s")
    print("=" * 80 + "\n")


def print_histogram(results: List[BenchmarkResult]):
    successful = sorted([r for r in results if r.success and r.speed_mbps > 0], 
                       key=lambda r: r.speed_mbps, reverse=True)
    if not successful: return

    print("  SPEED VISUALIZATION")
    print("  " + "-" * 50)
    max_speed = successful[0].speed_mbps
    
    for r in successful:
        bar_len = int((r.speed_mbps / max_speed) * 40)
        print(f"  {r.tool:<15} │ {'█' * bar_len:<40} {r.speed_mbps:.2f} MB/s")
    print()


# =============================================================================
# MAIN
# =============================================================================
def main():
    parser = argparse.ArgumentParser(description="Surge Benchmark Suite")
    parser.add_argument("url", nargs="?", default=DEFAULT_TEST_URL, help="URL to benchmark")
    parser.add_argument("-n", "--iterations", type=int, default=1, help="Iterations per tool")
    parser.add_argument("--surge-exec", type=Path, help="Specific Surge binary")
    parser.add_argument("--surge-baseline", type=Path, help="Baseline Surge binary for comparison")
    parser.add_argument("--mirror-suite", action="store_true", help="Run multi-mirror test suite")
    parser.add_argument("--speedtest", action="store_true", help="Run network speedtest-cli")
    
    # Tool flags
    for tool in ["surge", "aria2", "wget", "curl", "axel"]:
        parser.add_argument(f"--{tool}", action="store_true", help=f"Run {tool} benchmark")

    args = parser.parse_args()
    
    # Configuration
    num_iterations = args.iterations
    project_dir = Path(__file__).parent.resolve()
    temp_dir = Path(tempfile.mkdtemp(prefix="surge_bench_"))
    download_dir = temp_dir / "downloads"
    download_dir.mkdir(parents=True, exist_ok=True)

    print(f"\n⚡ Surge Benchmark Suite")
    print(f"   Temp Dir: {temp_dir}")
    print("=" * 60)

    # Determine tasks
    tasks = []
    
    # 1. Setup Surge
    surge_bin = None
    if args.surge_exec and args.surge_exec.exists():
        surge_bin = args.surge_exec.resolve()
        print(f"  [OK] Using provided Surge: {surge_bin}")
    elif shutil.which("go") and build_surge(project_dir):
        surge_bin = project_dir / f"surge{EXE_SUFFIX}"
    
    # Define generic tools to check
    # (flag_name, bin_name, cmd_func)
    standard_tools = [
        ("aria2", "aria2c", cmd_aria2),
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
            tasks.append(lambda: benchmark_surge(surge_bin, ",".join(urls), download_dir, "Surge (3 Mirrors)"))
            tasks.append(lambda: benchmark_surge(surge_bin, ",".join(urls[:2]), download_dir, "Surge (2 Mirrors)"))
            tasks.append(lambda: benchmark_surge(surge_bin, urls[0], download_dir, "Surge (1 Mirror)"))
        if args.surge_baseline and args.surge_baseline.exists():
             tasks.append(lambda: benchmark_surge(args.surge_baseline, urls[0], download_dir, "Surge Baseline"))

    else:
        # Standard Single URL Logic
        specific_request = any(getattr(args, t) for t in ["surge", "aria2", "wget", "curl", "axel"]) or args.surge_exec or args.surge_baseline
        run_all = not specific_request

        # Surge tasks
        if (run_all or args.surge) and surge_bin:
            tasks.append(lambda: benchmark_surge(surge_bin, args.url, download_dir, "Surge (Current)"))
        
        if args.surge_baseline and args.surge_baseline.exists():
            tasks.append(lambda: benchmark_surge(args.surge_baseline, args.url, download_dir, "Surge (Baseline)"))

        # Standard tool tasks
        for name, bin_name, func in standard_tools:
            if (run_all or getattr(args, name)) and check_tool(bin_name):
                # Capture variables in lambda default args to avoid closure binding issues
                tasks.append(lambda n=name, b=bin_name, f=func: benchmark_standard_tool(n, b, f, args.url, download_dir))

    if not tasks:
        print("\n  [!] No benchmarks to run. Check installed tools or arguments.")
        shutil.rmtree(temp_dir)
        return

    # Speedtest
    if args.speedtest and shutil.which("speedtest-cli"):
        print("\nRunning network baseline...")
        _, out = run_command(["speedtest-cli", "--simple"], timeout=60)
        print(f"  {out.strip().replace('\n', ' | ')}")

    # Execution Loop
    print(f"\n🚀 Running {len(tasks)} benchmarks x {num_iterations} iterations...")
    raw_results = {i: [] for i in range(len(tasks))} # Store by task index

    try:
        for i in range(num_iterations):
            print(f"\n  [Iteration {i+1}/{num_iterations}]")
            # Create a list of (index, task_func) and shuffle it
            indexed_tasks = list(enumerate(tasks))
            random.shuffle(indexed_tasks)

            for task_idx, task_func in indexed_tasks:
                res = task_func()
                raw_results[task_idx].append(res)
                
                status = f"{res.elapsed_seconds:.2f}s" if res.success else "FAIL"
                print(f"    👉 {res.tool:<20} : {status}")
                time.sleep(2) # Cooldown

        # Aggregate
        final_results = []
        for i in range(len(tasks)):
            runs = raw_results[i]
            successful = [r for r in runs if r.success]
            
            if not successful:
                final_results.append(BenchmarkResult(runs[0].tool, False, 0, 0, runs[-1].error))
                continue
            
            # Simple average (could add outlier removal here if needed)
            avg_time = sum(r.elapsed_seconds for r in successful) / len(successful)
            file_size = successful[0].file_size_bytes
            
            final_results.append(BenchmarkResult(
                successful[0].tool, True, avg_time, file_size, iter_results=[r.elapsed_seconds for r in successful]
            ))

        print_results(final_results)
        print_histogram(final_results)

    finally:
        print("\n🧹 Cleaning up...")
        shutil.rmtree(temp_dir, ignore_errors=True)

if __name__ == "__main__":
    main()