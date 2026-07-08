#!/usr/bin/env bash
# Runs once after the devcontainer is created.
set -euo pipefail

WORKSPACE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "==> Configuring persistent shell history on /commandhistory"
if [ -d /commandhistory ]; then
    sudo chown -R "$(id -un):$(id -gn)" /commandhistory || true
    touch /commandhistory/.zsh_history /commandhistory/.bash_history
    for rc in "$HOME/.zshrc" "$HOME/.bashrc"; do
        if [ -f "$rc" ] && ! grep -q "commandhistory" "$rc"; then
            {
                echo ''
                echo '# Persist shell history on the devcontainer volume'
                if [[ "$rc" == *zshrc ]]; then
                    echo 'export HISTFILE=/commandhistory/.zsh_history'
                    echo 'setopt INC_APPEND_HISTORY'
                else
                    echo 'export HISTFILE=/commandhistory/.bash_history'
                fi
                echo 'export HISTSIZE=100000'
                echo 'export SAVEHIST=100000'
            } >> "$rc"
        fi
    done
fi

echo "==> Verifying committed ROM images against roms/SHA256SUMS"
(cd "$WORKSPACE/roms" && sha256sum -c SHA256SUMS)

echo "==> Syncing Go workspace"
cd "$WORKSPACE" && go work sync

echo "==> Toolchain versions"
go version
node --version

echo "==> Done. Try: go test ./core/..."
