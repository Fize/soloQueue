#!/bin/bash
# Double-click this script to fix the "SoloQueue.app is damaged and cannot be opened" error on macOS.
#
# Reason: SoloQueue uses an ad-hoc code signature. macOS flags non-notarized apps copied or
# downloaded from the internet with a "quarantine" attribute and refuses to run them.
# This script removes the quarantine attribute so you can run the app normally.

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
