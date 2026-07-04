package modem

import "context"

// PhoneQuerier can resolve the SIM line number via carrier USSD.
type PhoneQuerier interface {
	QueryPhoneUSSD(ctx context.Context, ussdCode string) (string, error)
}
