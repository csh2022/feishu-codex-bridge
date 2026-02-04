#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
用法：
  ./scripts/build.sh [options]

选项：
  --out <path>       输出二进制路径（默认：./bin/feishu-codex-bridge）
  --out-dir <dir>    输出目录（默认：./bin）
  --os <goos>        目标 GOOS（默认：使用 go env GOOS 或环境变量 GOOS）
  --arch <goarch>    目标 GOARCH（默认：使用 go env GOARCH 或环境变量 GOARCH）
  --cgo <0|1>        设置 CGO_ENABLED（默认：0）
  --debug            Debug 构建（不加 -trimpath/-s/-w）
  --test             构建前先执行 go test ./...
  -h, --help         显示帮助

示例：
  ./scripts/build.sh
  ./scripts/build.sh --test
  ./scripts/build.sh --os linux --arch amd64 --out ./bin/feishu-codex-bridge-linux-amd64
EOF
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

app_name="feishu-codex-bridge"
out_dir="$repo_root/bin"
out_path="$out_dir/$app_name"

debug=0
run_tests=0
cgo_enabled="${CGO_ENABLED:-0}"
target_os="${GOOS:-}"
target_arch="${GOARCH:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --out)
      out_path="${2:?missing value for --out}"
      shift 2
      ;;
    --out-dir)
      out_dir="${2:?missing value for --out-dir}"
      out_path="$out_dir/$app_name"
      shift 2
      ;;
    --os)
      target_os="${2:?missing value for --os}"
      shift 2
      ;;
    --arch)
      target_arch="${2:?missing value for --arch}"
      shift 2
      ;;
    --cgo)
      cgo_enabled="${2:?missing value for --cgo}"
      shift 2
      ;;
    --debug)
      debug=1
      shift
      ;;
    --test)
      run_tests=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      echo "" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if ! command -v go >/dev/null 2>&1; then
  echo "go not found in PATH" >&2
  exit 127
fi

mkdir -p "$(dirname "$out_path")"

export CGO_ENABLED="$cgo_enabled"
if [[ -n "$target_os" ]]; then export GOOS="$target_os"; fi
if [[ -n "$target_arch" ]]; then export GOARCH="$target_arch"; fi

echo "Repo: $repo_root"
echo "Go:   $(go version)"
echo "Env:  CGO_ENABLED=$CGO_ENABLED GOOS=${GOOS:-$(go env GOOS)} GOARCH=${GOARCH:-$(go env GOARCH)}"

if [[ "$run_tests" == "1" ]]; then
  echo "+ go test ./..."
  go test ./...
fi

build_args=(build -o "$out_path")
if [[ "$debug" == "0" ]]; then
  build_args+=(-trimpath -ldflags "-s -w")
fi
build_args+=(".")

printf '+ go'
for arg in "${build_args[@]}"; do
  printf ' %q' "$arg"
done
printf '\n'
go "${build_args[@]}"

echo "Built: $out_path"
