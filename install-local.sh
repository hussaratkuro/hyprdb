#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
build_dir="$(mktemp -d)"
trap 'rm -rf -- "$build_dir"' EXIT

cd "$project_dir"
go build -trimpath -tags novulkan -o "$build_dir/hyprdb" .
install -Dm755 "$build_dir/hyprdb" "$HOME/.local/bin/hyprdb"
install -Dm644 hyprdb.png "$HOME/.local/share/icons/hyprdb/hyprdb.png"
sed "s|__HOME__|$HOME|g" hyprdb.desktop > "$build_dir/hyprdb.desktop"
install -Dm644 "$build_dir/hyprdb.desktop" "$HOME/.local/share/applications/hyprdb.desktop"

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$HOME/.local/share/applications"
fi

echo "Installed hyprdb in $HOME/.local/bin"
