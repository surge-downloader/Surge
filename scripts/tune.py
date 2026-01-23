#!/usr/bin/env python3
import os
import re
import sys
import shutil
import subprocess
import argparse
import optuna
import atexit
import json
import logging
import platform
from pathlib import Path
from typing import Dict, Tuple, Optional, Any
from dataclasses import dataclass
from datetime import datetime

# --- Configuration ---
@dataclass
class Config:
    config_file: Path = Path("internal/download/types/config.go").resolve()
    benchmark_script: Path = Path("benchmark.py").resolve()
    project_root: Path = Path(__file__).parent.parent.resolve()
    db_file: str = "surge_opt.db"
    results_dir: Path = Path("optimization_results")
    log_file: Path = Path("optimization.log")

    def __post_init__(self):
        self.results_dir.mkdir(parents=True, exist_ok=True)

CONFIG = Config()

# --- Logging Setup ---
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[
        logging.FileHandler(CONFIG.log_file),
        logging.StreamHandler(sys.stdout)
    ]
)
logger = logging.getLogger(__name__)

# --- Regex Patterns ---
# Improved to be resilient to comment changes.
# Captures: Group 1 (VarName = ), Group 2 (Current Value), Group 3 (Remainder/Comments)
REGEX_MAP = {
    "MinChunk":       r"(MinChunk\s*=\s*)([^/\n]+)(.*)",
    "MaxChunk":       r"(MaxChunk\s*=\s*)([^/\n]+)(.*)",
    "TargetChunk":    r"(TargetChunk\s*=\s*)([^/\n]+)(.*)",
    "WorkerBuffer":   r"(WorkerBuffer\s*=\s*)([^/\n]+)(.*)",
    "TasksPerWorker": r"(TasksPerWorker\s*=\s*)([^/\n]+)(.*)",
    "PerHostMax":     r"(PerHostMax\s*=\s*)([^/\n]+)(.*)",
}

# --- Helper: System Checks ---
def check_dependencies(use_network_sim: bool):
    """Verifies that required tools are available."""
    required_tools = ["go"]
    if use_network_sim:
        required_tools.append("tc")
    
    for tool in required_tools:
        if not shutil.which(tool):
            logger.error(f"Required tool not found: {tool}")
            sys.exit(1)
            
    if use_network_sim:
        # Check sudo access
        if os.geteuid() != 0:
            res = subprocess.run(["sudo", "-n", "true"], capture_output=True)
            if res.returncode != 0:
                logger.error("Network simulation requires passwordless sudo or running as root.")
                sys.exit(1)

# --- Network Emulation ---
class NetworkEmulator:
    """Manages Linux Traffic Control (tc) for network simulation."""
    
    def __init__(self, interface: str = "eth0", rate: str = "300mbit", 
                 delay: str = "35ms", loss: str = "0.1%"):
        self.interface = interface
        self.rate = rate
        self.delay = delay
        self.loss = loss
        self.active = False
    
    def setup(self) -> bool:
        """Apply network emulation rules."""
        logger.info(f"🌐 Setting up network simulation on {self.interface}")
        logger.info(f"   Config: {self.rate}, {self.delay}, {self.loss} loss")
        
        self._teardown_silent()
        
        # Use 'replace' or 'add' safely
        cmd = (
            f"sudo tc qdisc add dev {self.interface} root netem "
            f"rate {self.rate} delay {self.delay} loss {self.loss} "
            f"limit 100000"
        )
        
        result = subprocess.run(cmd, shell=True, capture_output=True, text=True)
        
        if result.returncode != 0:
            logger.warning(f"⚠️ Network emulation failed: {result.stderr.strip()}")
            return False
        
        self.active = True
        return True
    
    def _teardown_silent(self):
        subprocess.run(
            f"sudo tc qdisc del dev {self.interface} root",
            shell=True, stderr=subprocess.DEVNULL, stdout=subprocess.DEVNULL
        )
    
    def teardown(self):
        if self.active:
            logger.info("🧹 Tearing down network emulation...")
            self._teardown_silent()
            self.active = False

# --- Configuration Management ---
class ConfigManager:
    """Handles Go config file modifications safely."""
    
    def __init__(self, config_file: Path):
        self.config_file = config_file
        self.backup_file = config_file.with_suffix('.go.bak')
        
        if not self.config_file.exists():
            raise FileNotFoundError(f"Config file not found: {self.config_file}")

    def backup(self):
        """Create backup of original config."""
        if not self.backup_file.exists():
            shutil.copy2(self.config_file, self.backup_file)
    
    def restore(self):
        """Restore original config from backup."""
        if self.backup_file.exists():
            shutil.move(self.backup_file, self.config_file)
    
    def apply_params(self, params: Dict[str, str]):
        """Apply parameter values to config file using regex."""
        try:
            content = self.config_file.read_text(encoding='utf-8')
            
            for key, val in params.items():
                pattern = REGEX_MAP.get(key)
                if not pattern:
                    continue
                
                # Check if the parameter exists in the file before trying to replace
                if not re.search(pattern, content):
                    logger.warning(f"Parameter {key} not found in config file via regex.")
                    continue

                # \g<1> is var name, val is new value, \g<3> is comments
                content = re.sub(pattern, f"\\g<1>{val}\\g<3>", content)
            
            self.config_file.write_text(content, encoding='utf-8')
        except Exception as e:
            logger.error(f"Failed to apply params: {e}")
            self.restore() # Safety restore
            raise

    def cleanup(self):
        """Remove backup file if it exists (usually handled by restore, but strictly cleanup)."""
        if self.backup_file.exists():
            os.remove(self.backup_file)

# --- Build & Benchmark ---
class BenchmarkRunner:
    """Handles build and benchmark execution."""
    
    def __init__(self, project_root: Path, benchmark_script: Path):
        self.project_root = project_root
        self.benchmark_script = benchmark_script
        self.binary_name = "surge-tuned"
        if platform.system() == "Windows":
            self.binary_name += ".exe"
    
    def build(self) -> Tuple[bool, str]:
        """Build the surge binary."""
        # -w -s flags strip debug info for slightly faster builds/smaller binaries
        cmd = ["go", "build", "-ldflags", "-w -s", "-o", self.binary_name, "."]
        return self._run_command(cmd, timeout=120)
    
    def benchmark(self, runs: int = 3) -> Tuple[bool, str]:
        """Run benchmark and return output."""
        cmd = [
            sys.executable,
            str(self.benchmark_script),
            "--surge-exec", self.binary_name,
            "-n", str(runs),
            "--surge"
        ]
        return self._run_command(cmd, timeout=600)
    
    def _run_command(self, cmd, timeout: int) -> Tuple[bool, str]:
        try:
            result = subprocess.run(
                cmd,
                cwd=str(self.project_root),
                capture_output=True,
                text=True,
                timeout=timeout
            )
            output = result.stdout + "\n" + result.stderr
            return result.returncode == 0, output
        except subprocess.TimeoutExpired:
            return False, f"Command timed out after {timeout}s"
        except Exception as e:
            return False, f"Execution Error: {str(e)}"
    
    def cleanup_binary(self):
        binary_path = self.project_root / self.binary_name
        if binary_path.exists():
            try:
                binary_path.unlink()
            except PermissionError:
                logger.warning(f"Could not delete binary {binary_path} (locked?)")

# --- Optimization Helpers ---
def params_to_go_format(params: Dict) -> Dict[str, str]:
    """Convert trial parameters to Go code format."""
    # Ensure values are cast correctly
    return {
        "MinChunk":       f"{int(params['MinChunk_MB'])} * MB",
        "MaxChunk":       f"{int(params['MaxChunk_MB'])} * MB",
        "TargetChunk":    f"{int(params['TargetChunk_MB'])} * MB",
        "WorkerBuffer":   f"{int(params['WorkerBuffer_KB'])} * KB",
        "TasksPerWorker": str(int(params['TasksPerWorker'])),
        "PerHostMax":     str(int(params['PerHostMax'])),
    }

def extract_speed(output: str) -> Optional[float]:
    patterns = [
        r"surge \(current\).*?│\s*([\d\.]+)\s*MB/s",
        r"Speed:\s*([\d\.]+)\s*MB/s",
        r"Average.*?:\s*([\d\.]+)\s*MB/s",
        r"Throughput:\s*([\d\.]+)\s*MB/s", 
    ]
    
    for pattern in patterns:
        match = re.search(pattern, output, re.IGNORECASE)
        if match:
            return float(match.group(1))
    return None

# --- Optuna Objective ---
class OptimizationObjective:
    def __init__(self, config_manager: ConfigManager, 
                 benchmark_runner: BenchmarkRunner,
                 benchmark_runs: int = 3):
        self.config_manager = config_manager
        self.benchmark_runner = benchmark_runner
        self.benchmark_runs = benchmark_runs
    
    def __call__(self, trial: optuna.Trial) -> float:
        # 1. Suggest Parameters
        min_chunk    = trial.suggest_int("MinChunk_MB", 1, 16)
        max_chunk    = trial.suggest_int("MaxChunk_MB", 16, 128, step=8)
        target_chunk = trial.suggest_int("TargetChunk_MB", 8, 64, step=4)
        buffer_kb    = trial.suggest_int("WorkerBuffer_KB", 64, 4096, step=64) # Increased upper bound
        tasks        = trial.suggest_int("TasksPerWorker", 2, 64, step=2)      # Increased upper bound
        hosts        = trial.suggest_int("PerHostMax", 8, 128, step=8)
        
        # 2. Heuristic Pruning (Invalid Logic)
        if min_chunk >= target_chunk:
            raise optuna.TrialPruned("Constraint: MinChunk >= TargetChunk")
        if max_chunk <= target_chunk:
            raise optuna.TrialPruned("Constraint: MaxChunk <= TargetChunk")
        if target_chunk < min_chunk * 1.5:
             # Encourage distinct tiers
            raise optuna.TrialPruned("Constraint: TargetChunk too close to MinChunk")

        # 3. Apply Config
        self.config_manager.backup()
        try:
            params = params_to_go_format(trial.params)
            self.config_manager.apply_params(params)
            
            # 4. Build
            build_ok, build_out = self.benchmark_runner.build()
            if not build_ok:
                logger.error(f"Build failed for Trial {trial.number}")
                logger.debug(build_out)
                return 0.0 # Fail trial
            
            # 5. Benchmark
            bench_ok, bench_out = self.benchmark_runner.benchmark(self.benchmark_runs)
            if not bench_ok:
                logger.error(f"Benchmark failed for Trial {trial.number}")
                logger.debug(bench_out)
                return 0.0
            
            speed = extract_speed(bench_out)
            if speed is None:
                logger.error(f"Could not parse speed for Trial {trial.number}")
                return 0.0
            
            # Report result
            logger.info(f"Trial {trial.number}: {speed:.2f} MB/s (Params: {trial.params})")
            return speed
            
        except Exception as e:
            logger.exception(f"Unexpected error in trial {trial.number}")
            return 0.0
        finally:
            self.config_manager.restore()

# --- Main ---
def generate_visualizations(study: optuna.Study, output_dir: Path):
    """Generates HTML visualizations for the study."""
    try:
        from optuna.visualization import plot_optimization_history, plot_param_importances
        
        logger.info("📊 Generating visualizations...")
        
        # History
        fig_hist = plot_optimization_history(study)
        fig_hist.write_html(str(output_dir / "history.html"))
        
        # Importance (if enough trials)
        if len(study.trials) > 10:
            try:
                fig_imp = plot_param_importances(study)
                fig_imp.write_html(str(output_dir / "importance.html"))
            except Exception:
                pass # Can fail with certain samplers/insufficient data
                
        logger.info(f"   Saved to {output_dir}")
    except ImportError:
        logger.warning("   plotly/kaleido not installed. Skipping visualizations.")

def main():
    parser = argparse.ArgumentParser(
        description="Optimize Surge download parameters using Optuna",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter
    )
    parser.add_argument("--trials", type=int, default=50, help="Number of optimization trials")
    parser.add_argument("--benchmark-runs", type=int, default=3, help="Benchmark runs per trial")
    parser.add_argument("--interface", default="eth0", help="Network interface for tc emulation")
    parser.add_argument("--rate", default="300mbit", help="Simulated bandwidth")
    parser.add_argument("--delay", default="35ms", help="Simulated latency")
    parser.add_argument("--loss", default="0.1%", help="Simulated packet loss")
    parser.add_argument("--no-network-sim", action="store_true", help="Skip network simulation")
    parser.add_argument("--study-name", default="surge_tuning", help="Name of the Optuna study")
    parser.add_argument("--visualize", action="store_true", help="Generate HTML plots after finish")

    args = parser.parse_args()
    
    # 1. System Check
    check_dependencies(not args.no_network_sim)

    # 2. Setup Components
    network = NetworkEmulator(args.interface, args.rate, args.delay, args.loss)
    config_mgr = ConfigManager(CONFIG.config_file)
    benchmark = BenchmarkRunner(CONFIG.project_root, CONFIG.benchmark_script)
    
    # 3. Cleanup Handling
    def cleanup():
        network.teardown()
        config_mgr.restore() # Ensure config is reset on exit
        benchmark.cleanup_binary()
    
    atexit.register(cleanup)
    
    # 4. Network Simulation
    network_active = False
    if not args.no_network_sim:
        network_active = network.setup()
    
    # 5. Create/Load Study
    storage_url = f"sqlite:///{CONFIG.db_file}"
    logger.info(f"💾 Using database: {storage_url}")
    
    study = optuna.create_study(
        study_name=args.study_name,
        direction="maximize",
        storage=storage_url,
        load_if_exists=True,
        sampler=optuna.samplers.TPESampler(seed=42)
    )
    
    logger.info(f"🚀 Starting Optimization: {args.trials} trials")
    
    # 6. Run Optimization
    objective = OptimizationObjective(config_mgr, benchmark, args.benchmark_runs)
    try:
        study.optimize(objective, n_trials=args.trials, show_progress_bar=True)
    except KeyboardInterrupt:
        logger.warning("\n⚠️ Optimization interrupted by user. Saving current progress...")
    
    # 7. Reporting
    if len(study.trials) == 0:
        logger.warning("No trials completed.")
        return

    best_trial = study.best_trial
    logger.info(f"\n{'='*60}")
    logger.info(f"🏆 Best Trial: #{best_trial.number}")
    logger.info(f"   Speed: {best_trial.value:.2f} MB/s")
    logger.info(f"   Params: {json.dumps(best_trial.params, indent=2)}")
    logger.info(f"{'='*60}\n")
    
    # 8. Export Results
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    results_file = CONFIG.results_dir / f"results_{timestamp}.json"
    
    with open(results_file, 'w') as f:
        json.dump({
            "best_params": best_trial.params,
            "best_value": best_trial.value,
            "network": vars(network),
            "all_trials": [
                {"number": t.number, "value": t.value, "params": t.params, "state": str(t.state)}
                for t in study.trials
            ]
        }, f, indent=2)
    
    logger.info(f"📝 Full results saved to: {results_file}")
    
    # 9. Visualization
    if args.visualize:
        generate_visualizations(study, CONFIG.results_dir)

    # 10. Apply Best
    logger.info("✅ Applying best configuration to source code...")
    best_params_go = params_to_go_format(best_trial.params)
    config_mgr.apply_params(best_params_go)
    # Note: We do NOT restore() here, leaving the file in the optimized state.

if __name__ == "__main__":
    main()