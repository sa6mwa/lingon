#!/usr/bin/env bash
set -euo pipefail

CONFIG_DIR="${HOME}/.config/Android Open Source Project"
CONFIG_PATH="${CONFIG_DIR}/Emulator.conf"

mkdir -p "${CONFIG_DIR}"

# Keep host emulator controls predictable across launches:
# - shortcuts go to emulator controls (pinch/zoom overlays and ctrl combos)
# - pinch/mouse-wheel gestures remain enabled
cat > "${CONFIG_PATH}" <<EOF
[General]
showGpuWarning=false

[set]
autoFindAdb=true
clipboardSharing=true
crashReportPreference=0
disableMouseWheel=false
disablePinchToZoom=false
forwardShortcutsToDevice=false
savePath=${HOME}/Desktop
EOF

echo "Updated ${CONFIG_PATH} (shortcuts=emulator controls, pinch/mouse gestures enabled)."
