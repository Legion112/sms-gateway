package cli

import (
	"fmt"
	"time"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
	"github.com/legion/sms-gateway/internal/state"
	"github.com/spf13/cobra"
)

func newStatusCmd(flags *cmdutil.Flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show SIM and SMS readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(*flags)
		},
	}
	cmdutil.BindModemFlags(cmd, flags)
	return cmd
}

func runStatus(flags cmdutil.Flags) error {
	base, targets, err := cmdutil.OpenModemTargets(flags)
	if err != nil {
		// The daemon holds every port it watches for the life of the process,
		// so on a working gateway this is the normal outcome rather than the
		// exceptional one. Everything this command reports is already in the
		// daemon's published state, so fall back to that instead of telling
		// the operator the port is busy and leaving them to read the journal.
		if reportStatusFromDaemon(base, flags, err) {
			return nil
		}
		return cmdutil.NewCLIError(cmdutil.ExitSetup, fmt.Sprintf("error: %v", err))
	}
	defer closeModems(targets)

	multi := len(targets) > 1
	var firstErr error
	for i, target := range targets {
		if i > 0 {
			fmt.Println()
		}
		cmdutil.PrintSection(target.Name, multi)
		if err := statusOne(base, target); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func statusOne(base config.Config, target cmdutil.ModemHandle) error {
	ctx, cancel := cmdutil.SMSContext(target.Config)
	defer cancel()

	status, err := target.Modem.SMSStatus(ctx)
	if err != nil {
		fmt.Printf("driver: %s\n", target.Config.Driver)
		if status.Device != "" {
			fmt.Printf("device: %s\n", status.Device)
		}
		fmt.Printf("status: failed\n")
		return cmdutil.NewCLIError(cmdutil.ExitModem, fmt.Sprintf("error: %v", err))
	}

	line := cmdutil.ResolveLine(base, status.ICCID)

	fmt.Printf("driver: %s\n", status.Driver)
	fmt.Printf("device: %s\n", status.Device)
	fmt.Printf("sim: %s\n", status.SimStatus)
	if status.ICCID != "" {
		fmt.Printf("iccid: %s\n", config.NormalizeICCID(status.ICCID))
	} else {
		fmt.Printf("iccid: unknown\n")
	}
	if status.IMSI != "" {
		fmt.Printf("imsi: %s\n", status.IMSI)
	}
	fmt.Printf("phone: %s\n", cmdutil.FormatPhoneStatus(line))
	if status.PhoneNumber != "" {
		fmt.Printf("reported_msisdn: %s\n", status.PhoneNumber)
	}
	fmt.Printf("modem: %s\n", status.ModemState)
	fmt.Printf("network: %s\n", status.NetworkState)
	printRadioPosture(target)
	printRegistration(target)
	fmt.Printf("messages: %s\n", status.MessageStore)
	if status.MessageCount >= 0 {
		fmt.Printf("message_count: %d\n", status.MessageCount)
	}
	fmt.Printf("sms_ready: %t\n", status.SMSReady)
	fmt.Printf("detail: %s\n", status.Detail)
	return nil
}

// printRadioPosture reports how the modem picks its technology and operator,
// and flags the two settings that let a modem look perfectly healthy while
// being unable to receive anything. It only reads: correcting the posture is
// the watch daemon's job, and a status command must not rewrite modem NV.
func printRadioPosture(target cmdutil.ModemHandle) {
	controller, ok := target.Modem.(modem.RadioController)
	if !ok {
		return
	}
	ctx, cancel := cmdutil.SMSContext(target.Config)
	defer cancel()

	posture, err := controller.ReadRadioPosture(ctx)
	if err != nil {
		fmt.Printf("radio: unreadable (%v)\n", err)
		return
	}
	fmt.Printf("scan_mode: %s\n", posture.ScanMode)
	fmt.Printf("operator_selection: %s\n", posture.Operator)
	if posture.ScanMode == config.ScanModeLTE {
		fmt.Printf("warning: locked to LTE; a roaming SIM then needs the visited network to carry SMS over SGs/CSFB\n")
	}
	if posture.Operator == modem.OperatorManual {
		fmt.Printf("warning: operator pinned manually; the modem will not reselect when this network degrades\n")
	}
}

// printRegistration reports each domain's registration and what it means for
// incoming SMS. The network line above it shows the best of the three, which is
// the right answer to "has this modem got service" and hides the one this
// section exists for: an LTE-only attach reads as registered while the domain
// SMS travels over is not there at all.
func printRegistration(target cmdutil.ModemHandle) {
	reader, ok := target.Modem.(modem.RegistrationReader)
	if !ok {
		return
	}
	ctx, cancel := cmdutil.SMSContext(target.Config)
	defer cancel()

	reg, err := reader.ReadRegistration(ctx)
	if err != nil {
		fmt.Printf("registration: unreadable (%v)\n", err)
		return
	}
	fmt.Printf("cs_registration: %s\n", reg.CS)
	fmt.Printf("ps_registration: %s\n", reg.PS)
	fmt.Printf("ims_registration: %s\n", reg.IMS)

	printSMSPath(reg.SMSPath())
}

// printSMSPath reports the verdict and what an operator should do about it. It
// is shared by the live read and the daemon's published state so the two can
// never disagree about what a path means.
func printSMSPath(path modem.SMSPath) {
	fmt.Printf("sms_path: %s\n", path)
	switch path {
	case modem.SMSPathUnproven:
		fmt.Printf("warning: attached for data with no circuit-switched or IMS registration; incoming SMS depend on the network carrying them over SGs\n")
	case modem.SMSPathOffline:
		fmt.Printf("warning: no domain is registered; this modem cannot receive SMS\n")
	case modem.SMSPathIMS:
		fmt.Printf("note: SMS arrive over IMS; there is no circuit-switched registration to fall back on\n")
	}
}

// reportStatusFromDaemon prints the running daemon's own view of every modem the
// command was asked about, reporting whether it could.
//
// It declines rather than printing a partial answer: a `status` that silently
// left out one of two modems would be read as a report on both. When it
// declines the caller prints the original error, so nothing is swallowed.
func reportStatusFromDaemon(base config.Config, flags cmdutil.Flags, openErr error) bool {
	path := base.StatePath()
	if path == "" {
		return false
	}
	snap, err := state.Read(path)
	if err != nil {
		return false
	}
	names, err := base.ResolveModemTargets(flags.Modem)
	if err != nil || len(names) == 0 {
		return false
	}
	records := make([]state.Modem, 0, len(names))
	for _, name := range names {
		rec, ok := snap.Find(name)
		if !ok {
			return false
		}
		records = append(records, rec)
	}

	now := time.Now()
	fmt.Printf("source: watch daemon (pid %d), published %s ago\n", snap.PID, ago(snap.Age(now)))
	// Named plainly: an operator who ran this expecting to talk to the modem
	// must not have to work out why the numbers look frozen.
	fmt.Printf("reason: the modem could not be opened: %v\n", openErr)
	if snap.Stale(now) {
		fmt.Printf("warning: this state is older than the daemon's publishing interval; the daemon may have stopped, and every reading below may be out of date\n")
	}

	multi := len(records) > 1
	for _, rec := range records {
		fmt.Println()
		if multi {
			cmdutil.PrintSection(rec.Name, true)
		}
		printDaemonModem(base, rec, now)
	}
	fmt.Println()
	fmt.Printf("note: sim state, message count and live network state are only readable with the port; stop sms-gateway-watch to read them\n")
	return true
}

func printDaemonModem(base config.Config, rec state.Modem, now time.Time) {
	fmt.Printf("driver: %s\n", rec.Driver)
	if rec.Device != "" {
		fmt.Printf("device: %s\n", rec.Device)
	}
	if rec.ICCID != "" {
		fmt.Printf("iccid: %s\n", config.NormalizeICCID(rec.ICCID))
	}
	// Resolved from config rather than stored, so one formatter serves both
	// paths and a sim_numbers edit shows up without waiting for a republish.
	fmt.Printf("phone: %s\n", cmdutil.FormatPhoneStatus(cmdutil.ResolveLine(base, rec.ICCID)))

	if rec.Connected {
		fmt.Printf("state: up\n")
	} else {
		fmt.Printf("state: down (the daemon is retrying)\n")
	}
	if rec.Stalled {
		fmt.Printf("warning: connected but not polling successfully\n")
	}
	if rec.LastPollOK.IsZero() {
		fmt.Printf("last_poll: never\n")
	} else {
		fmt.Printf("last_poll: %s ago\n", ago(now.Sub(rec.LastPollOK)))
	}
	fmt.Printf("polls: %d\n", rec.Polls)
	fmt.Printf("reconnects: %d\n", rec.Reconnects)

	if rec.SMSPath == "" {
		// Distinct from a reading of "unknown": nothing has been asked yet, and
		// the stored domains are all RegAbsent, which would print as
		// "unsupported" and read as a firmware limitation.
		fmt.Printf("sms_path: not read yet\n")
		return
	}
	reg := rec.Registration()
	fmt.Printf("cs_registration: %s\n", reg.CS)
	fmt.Printf("ps_registration: %s\n", reg.PS)
	fmt.Printf("ims_registration: %s\n", reg.IMS)
	printSMSPath(rec.SMSPath)
	if !rec.RegCheckedAt.IsZero() {
		fmt.Printf("registration_read: %s ago\n", ago(now.Sub(rec.RegCheckedAt)))
	}
	if rec.RegUnreadable {
		fmt.Printf("warning: the last registration read failed; the three domains above are the last ones that answered\n")
	}
}

// ago renders a duration for an operator. Negative values come from clock skew
// between the daemon's write and this read, and are reported as the present
// rather than as the future.
func ago(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return "less than a second"
	}
	return d.Truncate(time.Second).String()
}
