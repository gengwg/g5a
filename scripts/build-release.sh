#!/usr/bin/env bash
set -euo pipefail

usage="Usage: $0 vX.Y.Z [output-directory]"
version="${1:-}"
version="${version#g5a-}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "$usage" >&2
  exit 2
fi
output_dir="${2:-dist}"

for command_name in go tar zip sha256sum; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required command not found: $command_name" >&2
    exit 1
  fi
done

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"
version_number="${version#v}"
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/g5a-release.XXXXXX")
cleanup() {
  if [[ -n "${work_dir:-}" && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT
mkdir -p "$output_dir"

for target in linux:amd64 linux:arm64 darwin:amd64 darwin:arm64 windows:amd64 windows:arm64; do
  os=${target%%:*}
  arch=${target##*:}
  package_dir="$work_dir/g5a_v${version_number}_${os}_${arch}"
  mkdir -p "$package_dir"
  binary=g5a
  if [[ "$os" == "windows" ]]; then
    binary=g5a.exe
  fi

  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X main.version=$version" \
    -o "$package_dir/$binary" ./cmd/g5a
  cp LICENSE NOTICE "$package_dir/"

  if [[ "$os" == "windows" ]]; then
    archive="$output_dir/g5a_v${version_number}_${os}_${arch}.zip"
    zip -j -q "$archive" "$package_dir/$binary" "$package_dir/LICENSE" "$package_dir/NOTICE"
  else
    archive="$output_dir/g5a_v${version_number}_${os}_${arch}.tar.gz"
    tar -czf "$archive" -C "$package_dir" "$binary" LICENSE NOTICE
  fi
done

(
  cd "$output_dir"
  sha256sum ./*.tar.gz ./*.zip > checksums.txt
)
printf 'Built release archives in %s\n' "$output_dir"
