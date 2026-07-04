package serial

import "context"

// setup configures registration reporting and SMS storage for Quectel LTE modems.
func (d *Driver) setup(ctx context.Context) error {
	cmds := []string{
		"ATE0",
		"AT+CREG=2",
		"AT+CGREG=2",
		"AT+CEREG=2",
		"AT+CNMI=2,1,0,0,0",
	}
	for _, cmd := range cmds {
		if _, err := d.exec(ctx, cmd); err != nil {
			return err
		}
	}
	// Prefer modem memory for SMS; ignore error if unsupported.
	_, _ = d.exec(ctx, `AT+CPMS="ME","ME","ME"`)
	return nil
}
