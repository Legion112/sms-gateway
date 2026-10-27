package watch

import (
	"context"
	"strings"
	"time"

	"github.com/legion/sms-gateway/internal/modem"
)

// radioPostureTimeout bounds the whole read-correct-verify exchange. The driver
// already bounds each individual write; this is the backstop that stops a modem
// which answers slowly from holding up the watch loop indefinitely.
const radioPostureTimeout = 90 * time.Second

// applyRadioPosture asserts the configured radio posture on a freshly opened
// modem, before anything else talks to it.
//
// This runs at connect rather than on a timer because it is cheap when there is
// nothing to do — two queries, no writes — and because the posture only changes
// when something else changes it: a firmware reset, a replaced module, someone
// at an AT prompt. Catching that at the next connect is soon enough, and it
// avoids competing with the poll loop for the port.
//
// Nothing here is fatal. A modem whose posture cannot be read or set still
// receives SMS on whatever posture it has, and taking it out of service over a
// failed correction would cause exactly the outage the correction prevents.
func (s *Service) applyRadioPosture(ctx context.Context, modemName string, m modem.Modem) {
	controller, ok := m.(modem.RadioController)
	if !ok {
		// The mm driver leaves radio access technology to ModemManager.
		return
	}

	postureCtx, cancel := context.WithTimeout(ctx, radioPostureTimeout)
	defer cancel()

	posture, err := controller.ApplyRadioPosture(postureCtx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		s.deps.Log("radio_posture_failed modem=%s %s error=%v", modemName, posture, redactSecrets(err))
		return
	}
	if len(posture.Changed) == 0 {
		s.deps.Log("modem_radio modem=%s %s", modemName, posture)
		return
	}
	// Worth a distinct line: a modem found in the wrong posture was, until this
	// moment, able to look healthy while receiving nothing.
	s.deps.Log("modem_radio_corrected modem=%s %s changed=%s",
		modemName, posture, strings.Join(posture.Changed, "; "))
}
