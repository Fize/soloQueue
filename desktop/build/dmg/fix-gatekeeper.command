#!/bin/bash
# Double-click this script to fix the "SoloQueue.app is damaged and cannot be opened" error on macOS.
#
# Reason: SoloQueue uses a fixed self-signed certificate but is not Apple-notarized. macOS may
# quarantine copies downloaded from the internet. This script only removes that quarantine
# attribute; it does not approve Accessibility, Automation, Full Disk Access, or other TCC access.

set -e

APP_PATHS=(
  "/Applications/SoloQueue.app"
  "$HOME/Applications/SoloQueue.app"
)

FOUND=""
for p in "${APP_PATHS[@]}"; do
  if [ -d "$p" ]; then
    FOUND="$p"
    break
  fi
done

if [ -z "$FOUND" ]; then
  echo "SoloQueue.app not found in /Applications."
  echo "Please drag SoloQueue.app to your Applications folder first, then run this script again."
  echo ""
  read -n 1 -s -r -p "Press any key to exit..."
  exit 1
fi

echo "Fixing: $FOUND"
xattr -dr com.apple.quarantine "$FOUND" 2>/dev/null || true

echo ""
echo "✅ Done! You can now open SoloQueue.app normally."
echo ""
read -n 1 -s -r -p "Press any key to exit..."
