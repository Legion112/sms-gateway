#!/bin/bash
# Reconnect WiFi when NetworkManager gets stuck disconnected / half-dead.
# Prefer a light nmcli reconnect; fall back to NetworkManager restart.
set -euo pipefail

CONNECTION="${WIFI_CONNECTION:-MTS_GPON_2F34}"
IFACE="${WIFI_IFACE:-wlan0}"
GATEWAY="${WIFI_GATEWAY:-192.168.1.1}"
PING_COUNT="${WIFI_PING_COUNT:-2}"
PING_WAIT="${WIFI_PING_WAIT:-2}"

log() {
	logger -t wifi-watchdog -- "$*"
	echo "$*"
}

nm_state() {
	# e.g. "wlan0:connected:MTS_GPON_2F34" or "wlan0:disconnected:"
	nmcli -t -f DEVICE,STATE,CONNECTION device status 2>/dev/null | awk -F: -v iface="$IFACE" '$1==iface {print $2; exit}'
}

has_default_route() {
	ip -4 route show default dev "$IFACE" 2>/dev/null | grep -q .
}

gateway_reachable() {
	ping -c "$PING_COUNT" -W "$PING_WAIT" -I "$IFACE" "$GATEWAY" >/dev/null 2>&1
}

wifi_healthy() {
	local state
	state="$(nm_state)"
	[ "$state" = "connected" ] || return 1
	has_default_route || return 1
	gateway_reachable || return 1
	return 0
}

if wifi_healthy; then
	exit 0
fi

state="$(nm_state)"
log "unhealthy iface=$IFACE state=${state:-unknown}; attempting nmcli connection up $CONNECTION"

if nmcli -w 30 connection up "$CONNECTION"; then
	sleep 3
	if wifi_healthy; then
		log "recovered via nmcli connection up"
		exit 0
	fi
fi

log "nmcli reconnect insufficient; restarting NetworkManager"
systemctl restart NetworkManager
sleep 5

# Give NM a chance to auto-activate, then force the known profile.
nmcli -w 30 connection up "$CONNECTION" >/dev/null 2>&1 || true
sleep 3

if wifi_healthy; then
	log "recovered after NetworkManager restart"
	exit 0
fi

log "still unhealthy after NetworkManager restart state=$(nm_state)"
exit 1
