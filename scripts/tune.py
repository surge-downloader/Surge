#!/usr/bin/env python3
import os
import re
import sys
import shutil
import subprocess
import argparse
import optuna
from pathlib import Path

# --- Configuration ---
CONFIG_FILE = Path("internal/download/types/config.go").resolve()
BENCHMARK_SCRIPT = Path("benchmark.py").resolve()
PROJECT_ROOT = Path(__file__).parent.parent.resolve()

REGEX_MAP = {
    "MinChunk":     r"(MinChunk\s*=\s*)(.*)(  // Minimum chunk size)",
    "MaxChunk":     r"(MaxChunk\s*=\s*)(.*)( // Maximum chunk size)",
    "TargetChunk":  r"(TargetChunk\s*=\s*)(.*)(  // Target chunk size)",
    "WorkerBuffer": r"(WorkerBuffer\s*=\s*)(.*)",
    "TasksPerWorker": r"(TasksPerWorker\s*=\s*)(.*)( // Target tasks per connection)",
    "PerHostMax":   r"(PerHostMax\s*=\s*)(.*)( // Max concurrent connections per host)",
}

def run_command(cmd, timeout=600):
    try:
        res = subprocess.run(cmd, cwd=str(PROJECT_ROOT), capture_output=True, text=True, timeout=timeout)
        return res.returncode == 0, res.stdout + res.stderr
    except Exception as e:
        return False, str(e)

def apply_config(params):
    content = CONFIG_FILE.read_text()
    for key, val in params.items():
        pattern = REGEX_MAP.get(key)
        if key == "WorkerBuffer":
             content = re.sub(r"(WorkerBuffer\s*=\s*)(.*)", f"\\g<1>{val}", content)
        else:
             content = re.sub(pattern, f"\\g<1>{val}\\g<3>", content)
    CONFIG_FILE.write_text(content)

def get_go_params(trial_params):
    """Converts Optuna numbers back to Go syntax strings"""
    return {
        "MinChunk":     f"{trial_params['MinChunk_MB']} * MB",
        "MaxChunk":     f"{trial_params['MaxChunk_MB']} * MB",
        "TargetChunk":  f"{trial_params['TargetChunk_MB']} * MB",
        "WorkerBuffer": f"{trial_params['WorkerBuffer_KB']} * KB",
        "TasksPerWorker": str(trial_params['TasksPerWorker']),
        "PerHostMax":   str(trial_params['PerHostMax']),
    }

def objective(trial):
    # 1. Range Search
    min_chunk   = trial.suggest_int("MinChunk_MB", 1, 16)
    max_chunk   = trial.suggest_int("MaxChunk_MB", 8, 128, step=4)
    target_chunk= trial.suggest_int("TargetChunk_MB", 4, 64)
    buffer_kb   = trial.suggest_int("WorkerBuffer_KB", 32, 1024, step=32)
    tasks       = trial.suggest_int("TasksPerWorker", 1, 32)
    hosts       = trial.suggest_int("PerHostMax", 4, 128)

    # 2. Logic Gates
    if min_chunk > target_chunk:
        raise optuna.TrialPruned("MinChunk > TargetChunk")
    if max_chunk < target_chunk:
        raise optuna.TrialPruned("MaxChunk < TargetChunk")

    # 3. Benchmark
    # We backup inside the objective loop so every trial is isolated
    shutil.copy(CONFIG_FILE, str(CONFIG_FILE) + ".bak")
    try:
        params = get_go_params(trial.params)
        apply_config(params)
        
        if not run_command(["go", "build", "-o", "surge-tuned", "."])[0]:
            return 0.0
        
        cmd = [sys.executable, str(BENCHMARK_SCRIPT), "--surge-exec", "surge-tuned", "-n", "3", "--surge"]
        success, out = run_command(cmd)
        
        match = re.search(r"surge \(current\).*?│\s*([\d\.]+)\s*MB/s", out)
        return float(match.group(1)) if match else 0.0
    finally:
        # Always restore after a trial so the next trial starts clean
        if Path(str(CONFIG_FILE) + ".bak").exists():
            shutil.copy(str(CONFIG_FILE) + ".bak", CONFIG_FILE)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--trials", type=int, default=50)
    args = parser.parse_args()
    
    study = optuna.create_study(
        study_name="surge_tuning", 
        direction="maximize",
        storage="sqlite:///surge_opt.db",
        load_if_exists=True,
        sampler=optuna.samplers.TPESampler(seed=42)
    )

    print("Injecting known best configuration (Warm Start)...")
    study.enqueue_trial({
        "MinChunk_MB": 1,
        "MaxChunk_MB": 16,
        "TargetChunk_MB": 16,
        "WorkerBuffer_KB": 512,
        "TasksPerWorker": 2,
        "PerHostMax": 64
    })

    print(f"Starting optimization with {args.trials} trials...")
    study.optimize(objective, n_trials=args.trials)
    
    print(f"Best Speed: {study.best_value:.2f} MB/s")
    
    # --- FINALIZATION ---
    # Apply the winning configuration permanently (no backup needed)
    print("Applying best configuration to config.go...")
    best_params_go = get_go_params(study.best_params)
    apply_config(best_params_go)
    
    # Cleanup artifacts
    if Path("surge-tuned").exists():
        os.remove("surge-tuned")
    if Path(str(CONFIG_FILE) + ".bak").exists():
        os.remove(str(CONFIG_FILE) + ".bak")

if __name__ == "__main__":
    main()
