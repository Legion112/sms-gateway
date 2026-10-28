package cli

import (
	"fmt"

	"github.com/legion/sms-gateway/internal/cmdutil"
	"github.com/legion/sms-gateway/internal/config"
	"github.com/legion/sms-gateway/internal/modem"
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

	path := reg.SMSPath()
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
