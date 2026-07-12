#!/usr/bin/env bash
# Exercises `make download-talos-iso` in isolation: no real network access,
# no real sudo, no touching /var/lib/libvirt/images.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

fake_bin="$work_dir/bin"
mkdir -p "$fake_bin"

wget_log="$work_dir/wget.log"
: > "$wget_log"

# Fake `wget -O <dest> <url>`: records the call and writes a stub ISO.
cat > "$fake_bin/wget" <<'EOF'
#!/usr/bin/env bash
echo "$@" >> "$WGET_LOG"
dest=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -O) dest="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo "fake-iso-content" > "$dest"
EOF
chmod +x "$fake_bin/wget"

# Fake `sudo`: just runs the command directly (no privilege escalation needed in the test sandbox).
cat > "$fake_bin/sudo" <<'EOF'
#!/usr/bin/env bash
exec "$@"
EOF
chmod +x "$fake_bin/sudo"

iso_path="$work_dir/metal-amd64.iso"

run_target() {
  WGET_LOG="$wget_log" PATH="$fake_bin:$PATH" \
    make -C "$repo_root" download-talos-iso \
      TALOS_ISO_PATH="$iso_path" \
      TALOS_ISO_URL="https://example.invalid/metal-amd64.iso"
}

fail() { echo "FAIL: $1" >&2; exit 1; }

echo "case 1: ISO missing -> should download"
[[ ! -f "$iso_path" ]] || fail "test setup: ISO should not exist yet"
run_target
[[ -f "$iso_path" ]] || fail "ISO was not created"
[[ "$(wc -l < "$wget_log")" -eq 1 ]] || fail "expected exactly one wget call, got $(wc -l < "$wget_log")"
grep -q "https://example.invalid/metal-amd64.iso" "$wget_log" || fail "wget was not called with the expected URL"

echo "case 2: ISO already present -> should skip download"
run_target
[[ "$(wc -l < "$wget_log")" -eq 1 ]] || fail "wget should not be called again once the ISO exists"

echo "OK: download-talos-iso downloads once and is idempotent thereafter"
