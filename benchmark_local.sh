#!/bin/bash
# Local benchmark using localhost to eliminate network variables

set -e

# -------------------------------------------------------------------------
# Colors (inspired by mtime_functions.zsh)
# -------------------------------------------------------------------------
c_reset="\033[0m"
c_bold="\033[1m"
c_dim="\033[2m"
c_red="\033[31m"
c_green="\033[32m"
c_cyan="\033[36m"

# -------------------------------------------------------------------------
# Helpers
# -------------------------------------------------------------------------
print_header() {
    echo ""
    echo -e "${c_dim}•${c_reset} ${c_bold}$1${c_reset}"
}

print_kv() {
    local label="$1"
    local value="$2"
    echo -e "  ${c_dim}${label}:${c_reset} ${value}"
}

print_status() {
    local label="$1"
    local value="$2"
    echo -e "  ${c_dim}${label}:${c_reset} ${c_cyan}${value}${c_reset}"
}

# -------------------------------------------------------------------------
# Main
# -------------------------------------------------------------------------
echo ""
echo -e "${c_bold}Local Benchmark${c_reset} ${c_dim}//${c_reset} localhost"

TEMP_DIR=$(mktemp -d /tmp/surge_local_bench.XXXXXX)
print_kv "work dir" "$TEMP_DIR"

# Cleanup function
cleanup() {
    echo ""
    print_header "cleanup"
    pkill -f "miniserve.*$TEMP_DIR" 2>/dev/null || true
    rm -rf "$TEMP_DIR"
    echo -e "${c_dim}  Done${c_reset}"
}
trap cleanup EXIT

# Create test files
print_header "creating test files"

for i in 1 2 3; do
    print_kv "100MB_$i" "generating..."
    dd if=/dev/urandom of="$TEMP_DIR/100MB_$i.bin" bs=1M count=100 status=none
    size=$(ls -lh "$TEMP_DIR/100MB_$i.bin" | awk '{print $5}')
    echo -e "\r  ${c_dim}100MB_$i:${c_reset} ${c_green}${size}${c_reset}"
done

for i in 1 2 3; do
    print_kv "1GB_$i" "generating..."
    dd if=/dev/urandom of="$TEMP_DIR/1GB_$i.bin" bs=1M count=1024 status=none
    size=$(ls -lh "$TEMP_DIR/1GB_$i.bin" | awk '{print $5}')
    echo -e "\r  ${c_dim}1GB_$i:${c_reset} ${c_green}${size}${c_reset}"
done

# Start server
print_header "starting server"
miniserve -p 8888 "$TEMP_DIR" > /dev/null 2>&1 &
MINISERVE_PID=$!
sleep 1

if ! curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8888/ | grep -q "200"; then
    echo -e "  ${c_red}error:${c_reset} miniserve failed to start"
    exit 1
fi

print_status "server" "ready (pid: $MINISERVE_PID)"

# Run benchmarks
print_header "100MB benchmark"
for i in 1 2 3; do
    echo ""
    echo -e "${c_dim}•${c_reset} ${c_bold}iteration $i${c_reset} ${c_dim}//${c_reset} 100MB_$i.bin"
    ./benchmark.py "http://127.0.0.1:8888/100MB_$i.bin" -n 1 --cooldown 0 --no-shuffle 2>&1 | grep -v "^\s*work dir\|^\s*target\|^\s*warmup" || true
done

print_header "1GB benchmark"
for i in 1 2; do
    echo ""
    echo -e "${c_dim}•${c_reset} ${c_bold}iteration $i${c_reset} ${c_dim}//${c_reset} 1GB_$i.bin"
    ./benchmark.py "http://127.0.0.1:8888/1GB_$i.bin" -n 1 --cooldown 0 --no-shuffle 2>&1 | grep -v "^\s*work dir\|^\s*target\|^\s*warmup" || true
done
