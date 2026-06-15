#!/usr/bin/env bash
# One-time install of the pact-protobuf-plugin, required for the gRPC pact in
# `make contract`. It downloads pact-plugin-cli (an external binary) and then the
# plugin into ~/.pact/plugins. Run once per machine. (Kept out of `make bootstrap`
# because executing a downloaded binary needs a conscious opt-in — D-043.)
set -euo pipefail

CLI_URL="https://github.com/pact-foundation/pact-plugins/releases/download/pact-plugin-cli-v0.2.0/pact-plugin-macos-aarch64.gz"
case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)  CLI_URL="${CLI_URL/macos-aarch64/linux-x86_64}" ;;
  Linux-aarch64) CLI_URL="${CLI_URL/macos-aarch64/linux-aarch64}" ;;
  Darwin-x86_64) CLI_URL="${CLI_URL/macos-aarch64/macos-x86_64}" ;;
esac

tmp="$(mktemp -d)"
curl -sSfL "$CLI_URL" -o "$tmp/ppcli.gz"
gunzip -f "$tmp/ppcli.gz"
chmod +x "$tmp/ppcli"
"$tmp/ppcli" -y install https://github.com/pactflow/pact-protobuf-plugin/releases/latest
echo "pact-protobuf-plugin installed to ~/.pact/plugins"
