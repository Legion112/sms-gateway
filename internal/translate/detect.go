package translate

import (
	"strings"
	"unicode"
)

// Language tags this package tells apart. They are the three that actually
// arrive on these SIMs: Uzbek and Russian from the local operators, banks and
// government senders, English from a handful of international ones.
const (
	Uzbek   = "uz"
	Russian = "ru"
	English = "en"
	Unknown = ""
)

const (
	// minLetters is the shortest text worth classifying. Below it a message is
	// a fragment -- a bare "*102#", a truncated concat part -- and the markers
	// that identify a language have nowhere to appear.
	minLetters = 24

	// cyrillicShare is how much Cyrillic makes a message Russian outright.
	// These operators write Uzbek in the Latin alphabet, so Cyrillic here is
	// Russian; a few stray letters in an otherwise Latin message are not.
	cyrillicShare = 0.15

	// scoreFloor is the evidence a verdict needs, and scoreMargin how far it
	// must beat the runner-up. Both exist because the costly mistake is not a
	// missed translation but a confident wrong one: telling NLLB that
	// transliterated Russian is Uzbek produces fluent nonsense, which is worse
	// than leaving the message alone.
	scoreFloor  = 4
	scoreMargin = 2
)

// Detect reports which language a message body is written in, or Unknown when
// the evidence does not settle it.
//
// It exists because the translation engine cannot answer this question. NLLB is
// told the source language and will dutifully "translate" whatever it is given,
// so something has to decide, and most of this traffic must not be translated:
// roughly half of it is Russian written in Latin letters (`U Vas ne spisalas'
// ejemesyachnaya abon. plata`), which the recipient can already read and which
// an Uzbek decoder turns into gibberish.
func Detect(text string) string {
	norm := normalize(text)
	letters, cyrillic := letterCounts(norm)
	if letters < minLetters {
		return Unknown
	}
	if float64(cyrillic)/float64(letters) > cyrillicShare {
		return Russian
	}

	words := wordSet(norm)
	best, second, tag := 0, 0, Unknown
	for _, p := range profiles {
		s := p.score(norm, words)
		switch {
		case s > best:
			best, second, tag = s, best, p.tag
		case s > second:
			second = s
		}
	}
	if best < scoreFloor || best-second < scoreMargin {
		return Unknown
	}
	return tag
}

// profile is one language's fingerprint: tokens that are common in it and rare
// in the other two, plus substrings and suffixes for signals a word list cannot
// carry on its own.
type profile struct {
	tag      string
	words    map[string]int
	marks    map[string]int
	suffixes map[string]int
}

// score adds up the evidence for this profile, counting every marker at most
// once however often it recurs: an operator template that repeats "balans" four
// times is one piece of evidence, not four.
func (p profile) score(norm string, words map[string]struct{}) int {
	total := 0
	for w, weight := range p.words {
		if _, ok := words[w]; ok {
			total += weight
		}
	}
	for m, weight := range p.marks {
		if strings.Contains(norm, m) {
			total += weight
		}
	}
	for suffix, weight := range p.suffixes {
		if hasSuffixWord(words, suffix) {
			total += weight
		}
	}
	return total
}

// hasSuffixWord reports whether any word long enough to have a stem carries
// this ending. The length floor keeps a three-letter word from matching its own
// whole self and passing a grammatical ending off as one.
func hasSuffixWord(words map[string]struct{}, suffix string) bool {
	for w := range words {
		if len(w) >= len(suffix)+3 && strings.HasSuffix(w, suffix) {
			return true
		}
	}
	return false
}

// normalize lowercases and folds the apostrophe into one character. Uzbek's o'
// and g' are the single most distinctive thing about its Latin orthography, and
// this traffic spells them at least four ways -- o', o`, o‘, o’, oʻ -- so
// without this fold the strongest signal available is also the least reliable.
func normalize(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range strings.ToLower(text) {
		switch r {
		case '‘', '’', 'ʻ', 'ʼ', '`', '´':
			b.WriteRune('\'')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func letterCounts(norm string) (letters, cyrillic int) {
	for _, r := range norm {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Cyrillic, r) {
			cyrillic++
		}
	}
	return letters, cyrillic
}

// wordSet splits normalized text into distinct words, keeping an apostrophe
// that sits between letters so that to'lov stays one word rather than becoming
// the English "to" plus a remainder.
func wordSet(norm string) map[string]struct{} {
	out := make(map[string]struct{})
	var cur strings.Builder
	flush := func() {
		w := strings.Trim(cur.String(), "'")
		cur.Reset()
		if w != "" {
			out[w] = struct{}{}
		}
	}
	for _, r := range norm {
		if unicode.IsLetter(r) || r == '\'' {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}
