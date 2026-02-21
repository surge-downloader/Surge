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
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Optional, List, Callable

# =============================================================================
# CONSTANTS & CONFIG
# =============================================================================
IS_WINDOWS = platform.system() == "Windows"
EXE_SUFFIX = ".exe" if IS_WINDOWS else ""
MB = 1024 * 1024
DEFAULT_TEST_URL = "http://85.215.51.88:8080/100MB.bin"
FAST_TEST_URL = "http://85.215.51.88:8080/10MB.bin"
LARGE_TEST_URL = "http://85.215.51.88:8080/1GB.bin"

# =============================================================================
# ANSI COLORS (inspired by mtime_functions.zsh)
# =============================================================================
class C:
    reset = "\033[0m"
    bold = "\033[1m"
    dim = "\033[2m"
    red = "\033[31m"
    green = "\033[32m"
    yellow = "\033[33m"
    blue = "\033[34m"
    magenta = "\033[35m"
    cyan = "\033[36m"
    dark_gray = "\033[90m"
    # Semantic colors
    label = dim
    value = reset
    success = green
    error = red
    accent = cyan

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
# OUTPUT HELPERS (clean, minimal style)
# =============================================================================
def fmt_dim(text: str) -> str:
    return f"{C.dim}{text}{C.reset}"

def fmt_bold(text: str) -> str:
    return f"{C.bold}{text}{C.reset}"

def fmt_accent(text: str) -> str:
    return f"{C.accent}{text}{C.reset}"

def fmt_success(text: str) -> str:
    return f"{C.success}{text}{C.reset}"

def fmt_error(text: str) -> str:
    return f"{C.error}{text}{C.reset}"

def print_header(title: str):
    """Print a distinct section header with left accent."""
    print(f"\n{C.dim}•{C.reset} {C.bold}{title}{C.reset}")

def print_kv(label: str, value: str, indent: int = 2):
    """Print a key-value pair with dim label."""
    pad = " " * indent
    print(f"{pad}{C.dim}{label}:{C.reset} {value}")

def print_sep(char: str = "─", width: int = 50):
    """Print a subtle separator line."""
    print(f"{C.dim}{char * width}{C.reset}")

def print_status(tool: str, status: str, success: bool = True, is_last: bool = False):
    """Print tool status with tree branch style."""
    branch = f"{C.dim}╰─{C.reset}" if is_last else f"{C.dim}├─{C.reset}"
    color = C.success if success else C.error
    print(f"  {branch} {tool} {C.dim}took{C.reset} {color}{status}{C.reset}")

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
    print_kv("build", "surge...")
    output_name = f"surge{EXE_SUFFIX}"
    success, output = run_command(["go", "build", "-o", output_name, "."], cwd=str(project_dir))
    if not success:
        print(f"  {fmt_error('failed:')} {output.strip()[:60]}")
        return False
    return True


def check_tool(name: str) -> bool:
    """Check if a tool is in the PATH."""
    if shutil.which(name):
        return True
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
    cleanup_file(output_dir / f"{output_file.name}.st")

    cmd = cmd_builder(binary, output_file, url)

    start = time.perf_counter()
    success, output = run_command(cmd)
    elapsed = time.perf_counter() - start

    file_size = get_file_size(output_file)
    cleanup_file(output_file)
    cleanup_file(output_dir / f"{output_file.name}.st")

    if not success or file_size == 0:
        error_msg = output[:200] if output else "Download failed"
        return BenchmarkResult(name, False, elapsed, file_size, error_msg)

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
    return [binary, "-n", "4", "-q", "-o", str(out), url]

def cmd_wget(binary: str, out: Path, url: str) -> List[str]:
    return [binary, "-q", "-O", str(out), url]

def cmd_curl(binary: str, out: Path, url: str) -> List[str]:
    return [binary, "-s", "-f", "-L", "-o", str(out), url]


# =============================================================================
# REPORTING (clean, minimal tables)
# =============================================================================
def print_results(results: List[BenchmarkResult]):
    """Print results in a clean, aligned format."""
    print_header("results")

    # Find max tool name length for alignment
    max_len = max(len(r.tool) for r in results) if results else 12
    max_len = max(max_len, 12)

    # Subtle header with consistent column widths
    # Calculate width: "  " + tool_col + " " + time_col(10) + " " + speed_col(12) + " " + "Size"
    header_width = 2 + max_len + 1 + 10 + 1 + 12 + 1 + 4 + 4
    header = f"  {C.dim}{'Tool':<{max_len}} {'Time':<10} {'Speed':<12} Size{C.reset}"
    print(header)
    print_sep("─", header_width)

    for r in results:
        time_str = f"{r.elapsed_seconds:.2f}s" if r.elapsed_seconds > 0 else "-"
        if r.success and r.speed_mbps > 0:
            speed_str = f"{r.speed_mbps:.1f} MB/s"
            size_str = f"{r.file_size_bytes / MB:.0f} MB"
            print(f"  {r.tool:<{max_len}} {C.accent}{time_str:<10}{C.reset} {C.green}{speed_str:<12}{C.reset} {C.dim}{size_str}{C.reset}")
        elif r.success:
            # Tool succeeded but no file size detected
            print(f"  {r.tool:<{max_len}} {C.accent}{time_str:<10}{C.reset} {C.yellow}{'no size':<12}{C.reset}")
        else:
            print(f"  {r.tool:<{max_len}} {C.red}{time_str:<10}{C.reset} {C.red}{'failed':<12}{C.reset}")

    # Summary
    successful = [r for r in results if r.success and r.speed_mbps > 0]
    if successful:
        winner = max(successful, key=lambda r: r.speed_mbps)
        print_sep("─", max_len + 35)
        rel_speeds = [(r.speed_mbps / winner.speed_mbps) for r in successful]
        avg_rel = sum(rel_speeds) / len(rel_speeds) if rel_speeds else 1.0
        print(f"  {C.dim}Fastest:{C.reset} {C.accent}{winner.tool}{C.reset}  {C.dim}({winner.speed_mbps:.1f} MB/s){C.reset}")


def print_histogram(results: List[BenchmarkResult]):
    """Print a minimal horizontal bar chart."""
    successful = sorted([r for r in results if r.success and r.speed_mbps > 0],
                        key=lambda r: r.speed_mbps, reverse=True)
    if not successful:
        return

    print_header("comparison")
    max_speed = successful[0].speed_mbps
    width = 36
    max_len = max(len(r.tool) for r in successful)

    for r in successful:
        bar_len = int((r.speed_mbps / max_speed) * width)
        pct = (r.speed_mbps / max_speed) * 100
        bar = f"{C.accent}{'━' * bar_len}{C.dim}{'─' * (width - bar_len)}{C.reset}"
        print(f"  {r.tool:<{max_len}}  {bar}  {C.dim}{pct:.0f}%{C.reset}")


# =============================================================================
# MAIN
# =============================================================================
def main():
    parser = argparse.ArgumentParser(
        description="Surge Benchmark",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s                    # Run default benchmark
  %(prog)s --fast             # Quick test with 10MB file
  %(prog)s --only surge,curl  # Compare specific tools
        """
    )
    parser.add_argument("url", nargs="?", default=DEFAULT_TEST_URL, help="URL to benchmark")
    parser.add_argument("-n", "--iterations", type=int, default=1, help="Iterations per tool")
    parser.add_argument("--fast", action="store_true", help="Use 10MB test file")
    parser.add_argument("--cooldown", type=float, default=1, help="Cooldown between runs (sec)")
    parser.add_argument("--no-shuffle", action="store_true", help="Don't shuffle tool order")
    parser.add_argument("--surge-exec", type=Path, help="Specific Surge binary")
    parser.add_argument("--surge-baseline", type=Path, help="Baseline Surge binary")
    parser.add_argument("--mirror-suite", action="store_true", help="Multi-mirror test")
    parser.add_argument("--speedtest", action="store_true", help="Run speedtest-cli first")
    parser.add_argument("--only", type=str, help="Comma-separated tools to run")

    for tool in ["surge", "wget", "curl", "axel"]:
        parser.add_argument(f"--{tool}", action="store_true", help=f"Run {tool}")
    parser.add_argument("--aria2", action="store_true", help="Run aria2c")

    args = parser.parse_args()

    # Configuration
    num_iterations = args.iterations
    test_url = FAST_TEST_URL if args.fast else args.url
    project_dir = Path(__file__).parent.resolve()
    temp_dir = Path(tempfile.mkdtemp(prefix="surge_bench_"))
    download_dir = temp_dir / "downloads"
    download_dir.mkdir(parents=True, exist_ok=True)

    # Quiet startup info with consistent header style
    print(f"\n{C.dim}•{C.reset} {C.bold}benchmark{C.reset} {C.dim}//{C.reset} {test_url.split('/')[-1]}")
    print_kv("work dir", str(temp_dir))
    print_kv("target", args.url[:50] + ("..." if len(args.url) > 50 else ""))

    # Determine tasks
    tasks = []
    only_tools = set(args.only.split(',')) if args.only else None
    def should_run(name):
        return only_tools is None or name in only_tools

    # Setup Surge
    surge_bin = None
    if args.surge_exec and args.surge_exec.exists():
        surge_bin = args.surge_exec.resolve()
        print_kv("surge", surge_bin.name)
    elif shutil.which("go") and build_surge(project_dir):
        surge_bin = project_dir / f"surge{EXE_SUFFIX}"

    standard_tools = [
        ("aria2", "aria2c", cmd_aria2),
        ("axel", "axel", cmd_axel),
        ("wget", "wget", cmd_wget),
        ("curl", "curl", cmd_curl)
    ]

    # Build task list
    if args.mirror_suite:
        urls = [
            "https://distrib-coffee.ipsl.jussieu.fr/pub/linux/ubuntu-releases/noble/ubuntu-24.04.3-desktop-amd64.iso",
            "https://mirror.cedia.org.ec/ubuntu-releases/24.04.3/ubuntu-24.04.3-desktop-amd64.iso",
            "https://mirror.bharatdatacenter.com/ubuntu-releases/noble/ubuntu-24.04.3-desktop-amd64.iso",
        ]
        if surge_bin and should_run("surge"):
            tasks.append(lambda: benchmark_surge(surge_bin, ",".join(urls), download_dir, "surge (3 mirrors)"))
            tasks.append(lambda: benchmark_surge(surge_bin, ",".join(urls[:2]), download_dir, "surge (2 mirrors)"))
            tasks.append(lambda: benchmark_surge(surge_bin, urls[0], download_dir, "surge (1 mirror)"))
        if args.surge_baseline and args.surge_baseline.exists():
             tasks.append(lambda: benchmark_surge(args.surge_baseline, urls[0], download_dir, "surge baseline"))
    else:
        only_request = args.only is not None
        flag_request = any(getattr(args, t) for t in ["surge", "wget", "curl", "axel"]) or args.surge_exec or args.surge_baseline
        run_all = not flag_request and not only_request

        if (run_all or args.surge or only_request) and surge_bin and should_run("surge"):
            tasks.append(lambda: benchmark_surge(surge_bin, test_url, download_dir, "surge"))

        if args.surge_baseline and args.surge_baseline.exists() and should_run("surge"):
            tasks.append(lambda: benchmark_surge(args.surge_baseline, test_url, download_dir, "surge baseline"))

        for name, bin_name, func in standard_tools:
            tool_requested = getattr(args, name) or (only_request and should_run(name))
            if (run_all or tool_requested) and check_tool(bin_name):
                tasks.append(lambda n=name, b=bin_name, f=func: benchmark_standard_tool(n, b, f, test_url, download_dir))

    if not tasks:
        print(f"\n  {fmt_error('No benchmarks to run')} — check installed tools or arguments")
        shutil.rmtree(temp_dir)
        return

    # Speedtest (quiet)
    if args.speedtest and shutil.which("speedtest-cli"):
        print_kv("network", "testing...")
        _, out = run_command(["speedtest-cli", "--simple"], timeout=60)
        if out:
            lines = [l.strip() for l in out.strip().split('\n') if l.strip()]
            print(f"\r  {C.dim}network:{C.reset} {C.dim}│{C.reset} ", end="")
            print(f" {C.dim}│{C.reset} ".join(lines))

    # Warmup
    print_kv("warmup", "connection...")
    warmup_cmd = ["curl", "-s", "-f", "-o", "/dev/null", "--max-time", "5",
                  "--range", "0-1048575", test_url]
    subprocess.run(warmup_cmd, capture_output=True, timeout=10)
    time.sleep(0.3)
    print(f"\r  {C.dim}warmup:{C.reset} {fmt_success('ready')}")

    # Execution
    print_header(f"running {len(tasks)} tools × {num_iterations} iterations")

    raw_results = {i: [] for i in range(len(tasks))}

    try:
        for i in range(num_iterations):
            if num_iterations > 1:
                print(f"{C.dim}•{C.reset} {C.bold}iteration {i+1}/{num_iterations}{C.reset}")

            indexed_tasks = list(enumerate(tasks))
            if not args.no_shuffle:
                random.shuffle(indexed_tasks)

            for j, (task_idx, task_func) in enumerate(indexed_tasks):
                res = task_func()
                raw_results[task_idx].append(res)

                status = f"{res.elapsed_seconds:.2f}s" if res.success else "failed"
                is_last = (j == len(indexed_tasks) - 1)
                print_status(res.tool, status, res.success, is_last)

                if args.cooldown > 0:
                    time.sleep(args.cooldown)

        # Aggregate
        final_results = []
        for i in range(len(tasks)):
            runs = raw_results[i]
            successful = [r for r in runs if r.success]

            if not successful:
                final_results.append(BenchmarkResult(runs[0].tool, False, 0, 0, runs[-1].error))
                continue

            avg_time = sum(r.elapsed_seconds for r in successful) / len(successful)
            file_size = successful[0].file_size_bytes

            final_results.append(BenchmarkResult(
                successful[0].tool, True, avg_time, file_size,
                iter_results=[r.elapsed_seconds for r in successful]
            ))

        print_results(final_results)
        print_histogram(final_results)

    finally:
        # Quiet cleanup
        shutil.rmtree(temp_dir, ignore_errors=True)


if __name__ == "__main__":
    main()
