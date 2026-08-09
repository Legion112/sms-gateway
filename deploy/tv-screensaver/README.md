# HDMI terminal screensaver (first_pi)

Pretty TTY animations on the console attached to the TV (`tty1`). No desktop required.

## Install (on the Pi)

```bash
cd ~/smsgateway   # or wherever the repo is synced
bash deploy/tv-screensaver/install.sh
```

Or from your PC after `rsync`:

```bash
rsync -avz deploy/tv-screensaver/ first_pi:~/tv-screensaver/
ssh -t first_pi 'bash ~/tv-screensaver/install.sh'
```

(`-t` so you can type your sudo password.)

## Use

| Action | How |
|--------|-----|
| Start | Autostarts on `tty1` login; or run `tv-screensaver` |
| Stop | `Ctrl+C` → normal shell |
| Mode | `TV_ANIMATION=pipes\|matrix\|clock\|bonsai\|cycle` (default **cycle**) |

Examples:

```bash
TV_ANIMATION=pipes tv-screensaver
TV_ANIMATION=clock tv-screensaver
```
