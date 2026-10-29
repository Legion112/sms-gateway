package translate

// The marker sets below were drawn from the gateway's own stored traffic -- the
// distinct message bodies these two SIMs have received -- rather than from a
// general word frequency list. That matters because the vocabulary that has to
// be told apart here is narrow and peculiar: operator balance notices, bank
// card receipts, government advisories and one-time codes, half of them in
// Russian transliterated into Latin letters.
//
// Weights are coarse on purpose. A marker is worth 3 when it is near-conclusive
// on its own, 2 when it is a common function word of one language and absent
// from the others, and 1 when it only leans.
var profiles = []profile{uzbekProfile, russianProfile, englishProfile}

var uzbekProfile = profile{
	tag: Uzbek,
	// o' and g' are letters of the Uzbek Latin alphabet with no counterpart in
	// either other language. Transliterated Russian does use a trailing
	// apostrophe for the soft sign (spisalas', tol'ko), which is why these are
	// matched as the specific vowel and consonant pairs and not as apostrophes.
	marks: map[string]int{
		"o'": 3,
		"g'": 3,
	},
	words: map[string]int{
		"va": 2, "uchun": 2, "bilan": 2, "yoki": 2, "emas": 2,
		"ham": 2, "hamda": 2, "hech": 2, "faqat": 2, "mumkin": 2,
		"kerak": 2, "qadar": 2, "sababli": 2, "orqali": 2, "bo'yicha": 2,
		"siz": 2, "sizni": 2, "sizning": 2, "sizda": 2, "sizga": 2,
		"biz": 2, "bizning": 2, "o'z": 2, "o'zingiz": 2, "ushbu": 2,
		"quyidagi": 2, "barcha": 2, "yangi": 1, "bugun": 2, "endi": 2,
		"yana": 2, "juda": 2, "eng": 1, "bor": 1, "yo'q": 2,
		// Imperatives and finite verbs. Operator and government SMS are mostly
		// instructions, so these carry more of the traffic than nouns do.
		"qiling": 2, "oling": 2, "eting": 2, "bermang": 2, "tekshiring": 2,
		"saqlang": 2, "to'ldiring": 2, "tanlang": 2, "yoqing": 2, "o'chiring": 2,
		"bo'lsin": 2, "qilindi": 2, "to'ldirildi": 2, "bo'ladi": 2, "bajaring": 2,
		"so'raydi": 2, "so'raymiz": 2, "kelibsiz": 2, "ulang": 2,
		// Nouns that recur across these senders without a Russian or English
		// twin in the same spelling.
		"mablag'": 2, "so'm": 2, "so'mga": 2, "miqdor": 2, "qoldiq": 2,
		"hisobda": 2, "hisobingizni": 2, "ma'lumot": 2, "mijoz": 2, "xizmat": 2,
		"xizmatini": 2, "raqam": 1, "raqamini": 2, "tizimida": 2, "ilovasida": 2,
		"tasdiqlash": 2, "ro'yxatdan": 2, "kuni": 2, "kunlari": 2, "muddati": 2,
		"xavf": 2, "xavfli": 2, "xavfsiz": 2, "diqqat": 2, "hurmatli": 2,
	},
	// Productive Uzbek endings. They are worth only 1 each because a single
	// ending is weak evidence, but a genuine Uzbek sentence almost always
	// carries several.
	suffixes: map[string]int{
		"ingiz": 1, "larni": 1, "lardan": 1, "lari": 1, "ning": 1,
		"moqda": 1, "ayotgan": 1, "lsin": 1, "mang": 1, "imiz": 1,
	},
}

var russianProfile = profile{
	tag: Russian,
	words: map[string]int{
		// Function words. These are what transliterated Russian leaks even when
		// every noun in the message is a brand or an amount.
		"ne": 2, "na": 2, "po": 2, "dlya": 2, "chto": 2,
		"vash": 2, "vashego": 2, "vashem": 2, "vas": 2, "vy": 2,
		"mojno": 2, "cherez": 2, "menee": 2, "pri": 2, "etogo": 2,
		"tol'ko": 2, "budet": 2, "budut": 2, "zavtra": 2, "nikomu": 2,
		"esli": 2, "takje": 2, "snova": 2, "pojaluysta": 2, "vsego": 2,
		// Banking and operator vocabulary in transliteration. In Uzbek the same
		// notices use miqdor, qoldiq, hisob and to'lov instead, so these do not
		// collide.
		"sumov": 3, "schetu": 3, "scheta": 3, "karti": 2, "karty": 2,
		"plateja": 3, "podtverjdeniya": 3, "popolnen": 3, "popolneniye": 3,
		"popolnite": 3, "popolnit'": 3, "dostupno": 3, "pokupka": 2, "oplata": 1,
		"spisana": 3, "spisalas'": 3, "spisaniya": 3, "sredstv": 3, "sredstva": 3,
		"nedostatochno": 3, "peredavayte": 3, "prilozhenii": 3, "zablokirovan": 3,
		"neuplatu": 3, "napominaem": 3, "rekomenduem": 3, "izbejanie": 3,
		"pravilno": 2, "vveden": 2, "ostalos'": 3, "neobhodimosti": 3,
		"ejemesyachnaya": 3, "abon": 2, "plata": 2, "platy": 2, "tarifu": 2,
		"perevod": 2, "denejniy": 3, "vnimanie": 3, "summa": 1, "summu": 2,
		"ogranicheniya": 3, "deystvuyut": 3, "svyazi": 2, "zvonki": 2,
		"vkhodyashchie": 3, "rasxodovat'sya": 3, "dobavleniya": 3,
	},
}

var englishProfile = profile{
	tag: English,
	words: map[string]int{
		"the": 2, "and": 2, "you": 2, "your": 2, "for": 2,
		"this": 2, "that": 2, "with": 2, "from": 2, "are": 2,
		"was": 2, "have": 2, "has": 2, "can": 2, "will": 2,
		"not": 2, "all": 1, "also": 2, "please": 2, "welcome": 2,
		"use": 2, "link": 2, "tap": 2, "log": 1, "account": 2,
		"internet": 1, "hours": 2, "temporarily": 2, "blocked": 2, "unlock": 2,
		"message": 2, "send": 2, "sent": 2, "forwarded": 2, "should": 2,
		"change": 2, "credentials": 2, "network": 1, "here": 2, "there": 2,
	},
}
