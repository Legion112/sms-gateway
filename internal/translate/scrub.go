package translate

import "strings"

// minEchoRunes is the shortest run of the message worth hunting for in an error
// string. Below it a match is as likely to be a common word as a leak.
const minEchoRunes = 16

// scrubBody removes the message from an engine's error before it is logged.
//
// It is not enough to write log lines carefully: the error comes from outside
// this package, and a translation service that rejects a request very commonly
// quotes the request back. These bodies carry one-time codes, card numbers and
// balances, and the journal is read by anyone who can read the journal.
//
// Every run of the body at least minEchoRunes long is replaced, so a service
// that echoes the whole message, a truncated head of it or a fragment from the
// middle is caught alike.
func scrubBody(message, body string) string {
	body = strings.TrimSpace(body)
	runes := []rune(body)
	if message == "" || len(runes) < minEchoRunes {
		return message
	}
	// Longest first, so that the whole body is replaced as one piece rather
	// than left as a string of markers.
	for width := len(runes); width >= minEchoRunes; width-- {
		for start := 0; start+width <= len(runes); start++ {
			chunk := string(runes[start : start+width])
			if strings.Contains(message, chunk) {
				message = strings.ReplaceAll(message, chunk, "<body>")
			}
		}
		if !strings.Contains(message, "<body>") {
			continue
		}
		// Something matched at this width; anything shorter is inside it.
		break
	}
	return message
}
