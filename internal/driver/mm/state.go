package mm

// modemStateName maps ModemManager Modem.State values to readable labels.
func modemStateName(state int32) string {
	switch state {
	case -1:
		return "failed"
	case 0:
		return "unknown"
	case 1:
		return "locked"
	case 2:
		return "disabled"
	case 3:
		return "disabling"
	case 4:
		return "enabling"
	case 5:
		return "enabled"
	case 6:
		return "searching"
	case 7:
		return "registered"
	case 8:
		return "disconnecting"
	case 9:
		return "connecting"
	case 10:
		return "connected"
	default:
		return "unknown"
	}
}

// failedReasonName maps ModemManager StateFailedReason values.
func failedReasonName(reason uint32) string {
	switch reason {
	case 0:
		return ""
	case 1:
		return "unknown"
	case 2:
		return "sim-missing"
	case 3:
		return "sim-error"
	case 4:
		return "unknown-capabilities"
	case 5:
		return "esim-without-profiles"
	default:
		return "unknown"
	}
}

// accessTechRegistered reports whether modem state implies network registration.
func accessTechRegistered(state int32) bool {
	return state >= 7 // registered, connecting, connected
}
