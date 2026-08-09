#!/bin/bash
# Install terminal animations for first_pi HDMI console (tty1).
# Run ON the Pi:  bash deploy/tv-screensaver/install.sh
set -euo pipefail

echo "Installing packages (needs sudo)..."
sudo apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y cmatrix tty-clock cbonsai curl ca-certificates coreutils

echo "Installing pipes.sh..."
sudo curl -fsSL -o /usr/local/bin/pipes.sh \
	https://raw.githubusercontent.com/pipeseroni/pipes.sh/master/pipes.sh
sudo chmod +x /usr/local/bin/pipes.sh

SCRIPT_SRC="$(cd "$(dirname "$0")" && pwd)/tv-screensaver"
sudo install -m 755 "$SCRIPT_SRC" /usr/local/bin/tv-screensaver

MARKER="# tv-screensaver (HDMI tty1)"
PROFILE="${HOME}/.bash_profile"
touch "$PROFILE"
if ! grep -qF "$MARKER" "$PROFILE" 2>/dev/null; then
	cat >>"$PROFILE" <<'EOF'

# tv-screensaver (HDMI tty1)
# Set TV_ANIMATION=pipes|matrix|clock|bonsai|cycle  (default: cycle)
if [ -z "${SSH_CONNECTION:-}" ] && [ "$(tty 2>/dev/null)" = "/dev/tty1" ] && [ -z "${TV_SCREENSAVER_RAN:-}" ]; then
	export TV_SCREENSAVER_RAN=1
	/usr/local/bin/tv-screensaver || true
fi
EOF
	echo "Added autostart hook to $PROFILE"
else
	echo "Autostart hook already present in $PROFILE"
fi

echo
echo "Done. On the TV console (tty1):"
echo "  logout  (or reboot)  → screensaver starts"
echo "  Or now:  sudo openvt -c 1 -s -- su - legion -c tv-screensaver"
echo "  Ctrl+C returns to shell."
echo "  Modes:  TV_ANIMATION=pipes tv-screensaver"
