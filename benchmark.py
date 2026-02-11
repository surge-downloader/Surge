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
import os
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
def run_command(cmd: List[str], cwd: Optional[str] = None, timeout: int = 3600, use_pty: bool = False) -> tuple[bool, str]:
    """Run a command and return (success, output)."""
    try:
        if use_pty and not IS_WINDOWS:
            import pty
            master, slave = pty.openpty()
            p = subprocess.Popen(
                cmd,
                cwd=cwd,
                stdin=slave,
                stdout=slave,
                stderr=slave,
                close_fds=True,
                text=True
            )
            os.close(slave)
            
            output = ""
            try:
                # Read output in chunks until EOF
                while True:
                    try:
                        chunk = os.read(master, 1024).decode('utf-8', errors='ignore')
                        if not chunk:
                            break
                        output += chunk
                    except OSError:
                        break
            except Exception:
                pass
            finally:
                 os.close(master)
                 p.wait()
                 
            return p.returncode == 0, output

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

def print_box_header(title: str, width: int = 60):
    print(f"┌{'─' * (width - 2)}┐")
    print(f"│ {title:<{width - 4}} │")
    print(f"└{'─' * (width - 2)}┘")

# =============================================================================
# LOGGING UTILS
# =============================================================================
def get_surge_log_dir() -> Path:
    """Get the Surge log directory based on OS conventions."""
    home = Path.home()
    if IS_WINDOWS:
        app_data = os.environ.get("APPDATA")
        if not app_data:
            app_data = home / "AppData" / "Roaming"
        return Path(app_data) / "surge" / "logs"
    elif platform.system() == "Darwin":
        return home / "Library" / "Application Support" / "surge" / "logs"
    else:
        # Linux
        config_home = os.environ.get("XDG_CONFIG_HOME")
        if not config_home:
            config_home = home / ".config"
        return Path(config_home) / "surge" / "logs"

def get_latest_log_file() -> Optional[Path]:
    """Find the most recently modified debug log file."""
    log_dir = get_surge_log_dir()
    if not log_dir.exists():
        return None
    
    logs = list(log_dir.glob("debug-*.log"))
    if not logs:
        return None
        
    return max(logs, key=lambda p: p.stat().st_mtime)

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
def benchmark_surge(executable: Path, url: str, output_dir: Path, label: str = "surge") -> BenchmarkResult:
    """Specialized benchmark for Surge to parse internal duration."""
    if not executable.exists():
        return BenchmarkResult(label, False, 0, 0, f"Binary not found: {executable}")
    
    # Clean potential previous runs
    for f in output_dir.glob("*"):
        if f.is_file(): 
            cleanup_file(f)

    start = time.perf_counter()
    # Determine command based on mode
    cmd = [str(executable)]
    if label == "Surge Server" or "Server" in label:
        cmd.extend(["server", "start", url])
    else:
        # TUI mode (default)
        cmd.append(url)

    # Common flags
    cmd.extend(["--output", str(output_dir), "--exit-when-done"])

    # Determine if we need PTY (TUI mode needs it, Server mode doesn't)
    use_pty = "TUI" in label

    # Retry logic for lock contention
    max_retries = 5
    for attempt in range(max_retries):
        start = time.perf_counter()
        success, output = run_command(cmd, use_pty=use_pty)
        elapsed = time.perf_counter() - start
        
        if success:
            break
            
        if "Surge is already running" in output or "Error acquiring lock" in output:
             print(f"  [!] Lock contention (attempt {attempt+1}/{max_retries}), retrying in 2s...")
             time.sleep(2)
             continue
        
        break # Other error, don't retry
    elapsed = time.perf_counter() - start
    
    # Parse internal time if available (Stdout server, Log TUI)
    actual_time = elapsed
    
    # 1. Try stdout (Server mode)
    match = re.search(r"Completed: .*? \[.*?\] \(in (.*?)\)", output)
    if match:
        try:
            t = parse_go_duration(match.group(1))
            if t > 0: actual_time = t
        except ValueError:
            pass
    elif "TUI" in label:
        # 2. Try latest log file (TUI mode)
        log_file = get_latest_log_file()
        if log_file:
            try:
                content = log_file.read_text()
                # Find the last completion message
                matches = list(re.finditer(r"Completed: .*? \[.*?\] \(in (.*?)\)", content))
                if matches:
                    last_match = matches[-1]
                    t = parse_go_duration(last_match.group(1))
                    if t > 0: actual_time = t
            except Exception as e:
                print(f"  [!] Failed to parse log: {e}")

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


# =============================================================================
# MAIN
# =============================================================================
def main():
    parser = argparse.ArgumentParser(description="Surge Benchmark Suite")
    parser.add_argument("url", nargs="?", default=DEFAULT_TEST_URL, help="URL to benchmark")
    parser.add_argument("-n", "--iterations", type=int, default=1, help="Iterations per tool")
    parser.add_argument("--surge-exec", type=Path, help="Specific Surge binary")
    parser.add_argument("--surge-baseline", type=Path, help="Baseline Surge binary for comparison")
    parser.add_argument("--surge-tui", action="store_true", help="Run Surge TUI benchmark")
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

    print_box_header("SURGE BENCHMARK SUITE")
    print(f"  Work Dir: {temp_dir}")
    print(f"  Target:   {args.url[:60]}")
    if len(args.url) > 60: print(f"            {args.url[60:]}")
    print()

    # ISOLATION: Set env vars to use temp_dir for Surge config/state
    # This prevents lock contention with system instances and keeps benchmarks clean
    os.environ["XDG_CONFIG_HOME"] = str(temp_dir)
    os.environ["HOME"] = str(temp_dir)
    os.environ["APPDATA"] = str(temp_dir)

    # Determine tasks
    tasks = []
    
    # 1. Setup Surge
    surge_bin = None
    if args.surge_exec and args.surge_exec.exists():
        surge_bin = args.surge_exec.resolve()
        print(f"  [+] Using provided Surge: {surge_bin.name}")
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
            tasks.append(lambda: benchmark_surge(surge_bin, args.url, download_dir, "Surge Server"))
        
        if (run_all or args.surge_tui) and surge_bin:
            tasks.append(lambda: benchmark_surge(surge_bin, args.url, download_dir, "Surge TUI"))
        
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
        print("\n  [>] Running network baseline...")
        _, out = run_command(["speedtest-cli", "--simple"], timeout=60)
        for line in out.strip().split('\n'):
            print(f"      {line}")

    # Execution Loop
    print(f"\n  [>] Running {len(tasks)} benchmarks x {num_iterations} iterations")
    print("  " + "─" * 40)
    
    raw_results = {i: [] for i in range(len(tasks))} # Store by task index

    try:
        for i in range(num_iterations):
            # Create a list of (index, task_func) and shuffle it
            indexed_tasks = list(enumerate(tasks))
            random.shuffle(indexed_tasks)

            for task_idx, task_func in indexed_tasks:
                res = task_func()
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

    finally:
        print("  [>] Cleaning up...")
        shutil.rmtree(temp_dir, ignore_errors=True)
        print("  [+] Done.")

if __name__ == "__main__":
    main()