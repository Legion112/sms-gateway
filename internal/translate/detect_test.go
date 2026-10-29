package translate

import "testing"

// Every sample below is the wording of a real message body this gateway
// received, taken from its own store, with every card fragment, balance,
// amount, one-time code and phone number replaced by a synthetic value. The
// wording is what matters here and inventing it would miss the thing that
// makes this hard: operators and banks write Russian in Latin letters, mix
// both languages in one body, and wrap both around numbers and brand names.
// The numbers are incidental to detection, and they were somebody's.
func TestDetectsUzbek(t *testing.T) {
	for _, text := range []string{
		"Xavfsiz xonadon - baxtli oila! Pech va dudburonlarni soz holatda saqlang. Is gazidan saqlanish qoidalariga amal qiling!",
		"Is gazi - ko'rinmas xavf! Isitish pechlari, dudburonlar va shamollatish tizimlarini muntazam tekshiring. O'zingiz va yaqinlaringiz hayotini asrang!",
		"15-oktabrdan 30-noyabrga qadar biznesingizni ro'yxatdan o'tkazing: yuridik shaxslar - hisobot.stat.uz, o'zini o'zi band qilganlar - census.stat.uz. Tel: 1851",
		"21-oktabr - O'zbek tili bayrami kuni. O'zbek tili bayrami kuni barchamizga muborak bo'lsin!",
		"DIQQAT! kodni hech kimga bermang! Uni faqat firibgarlar so'raydi. hambi tizimiga 0004 kartani qo'shish uchun kod: 1000",
		"+998901234567 mobil aloqa raqamini Mobile-ID tizimida ro'yxatga qo'yish uchun tasdiqlash kodi: 100002.",
		"13-14 oktabr kunlari kutilayotgan yomg'irlar yog'ishi sababli, respublikaning tog' va tog' oldi hududlarida sel xavfi bor.",
		"To'lov: UZCARD KB TO VISA USD KB, UZ,09.08.26 15:22,karta ***0001. Miqdor:100000.00 UZS Qoldiq:50000.00 UZS",
	} {
		if got := Detect(text); got != Uzbek {
			t.Errorf("Detect(%.48q) = %q, want %q", text, got, Uzbek)
		}
	}
}

// These are the messages the feature exists to stay away from. A bank receipt
// decoded as Uzbek comes back as fluent nonsense wrapped around a real
// one-time code, which is worse than no translation at all.
func TestDetectsTransliteratedRussian(t *testing.T) {
	for _, text := range []string{
		"Demobank: data plateja 29 October 2026 08:25:33 summa 30.00 USD s karti ***0002. Kod podtverjdeniya: 100001",
		"U Vas ne spisalas' ejemesyachnaya abon. plata po tarifu \"Status Silver\", t.k. na schetu menee 90000.0 sumov.",
		"VNIMANIE! Nikomu ne peredavayte kod iz SMS! code: 100003 Kod dlya dobavleniya karty 0001 v prilozhenii Beepul.",
		"Karta *0003. Xarid/Pokupka \"OPENAI *CHATGPT SUBS\", -7.77, EUR, \"20-10-2026 22:38\". Dostupno: 1.56, USD.",
		"Popolneniye, 25.10.26 v 19:28. Karta (*0003). Summa: 30.00 USD. HUMO KB 2 VISA MC USD>Tashkent UZ. Dostupno: 30.56 USD",
		"Na Vashem balanse ostalos' menee 4000.00 sumov. Pri neobhodimosti mojno popolnit' balans cherez Beepul *2",
		"Napominaem, chto zavtra s balansa Vashego nomera budet spisana 90000.00 sumov po \"Status Silver\".",
	} {
		if got := Detect(text); got != Russian {
			t.Errorf("Detect(%.48q) = %q, want %q", text, got, Russian)
		}
	}
}

func TestDetectsCyrillicAsRussian(t *testing.T) {
	for _, text := range []string{
		"Добро пожаловать в Россию! В целях безопасности интернет и SMS ограничены на 24 часа.",
		"Пополните узбекскую банковскую карту Visa из РФ: http://qisq.uz/J5GvY",
		"Все роуминговые услуги снова доступны. Пожалуйста, включите и выключите авиа режим.",
	} {
		if got := Detect(text); got != Russian {
			t.Errorf("Detect(%.48q) = %q, want %q", text, got, Russian)
		}
	}
}

// English arrives from Telegram, Steam and the roaming networks. Translating it
// out of Uzbek would be the same mistake as translating Russian.
func TestDetectsEnglish(t *testing.T) {
	for _, text := range []string{
		"Welcome to the MTS network! Internet and SMS are temporarily blocked for 24 hours. To use the Internet tap the link.",
		"Telegram code: 100004  You can also tap on this link to log in: https://t.me/login/100004",
		"Hello there. This message send from phone to sms gateway and should be forwarded to telegram private chat",
	} {
		if got := Detect(text); got != English {
			t.Errorf("Detect(%.48q) = %q, want %q", text, got, English)
		}
	}
}

// A body carrying both languages is left alone on purpose. The operator has
// already said the same thing in Russian, so there is nothing a translation
// would add, and deciding which half to feed the engine is guesswork.
func TestBilingualIsUndecided(t *testing.T) {
	for _, text := range []string{
		"Balans popolnen na 25000.0 sumov. Balans: 12345.67890 sumov. Balansingizga 25000.0 so'm qabul qilindi. Balans: 12345.67890 sum.",
		"Kartaning orqa tarafidagi CVV2 kodi yoki karta amal qilish muddati noto'g'ri kiritilgan. Ne pravilno vveden kod na obratnoy storone karti CVV2 ili srok deystviya karti.Karta *0003. Xarid/Pokupka \"STEAMGAMES.COM 42595\", -2.00, USD",
	} {
		if got := Detect(text); got != Unknown {
			t.Errorf("Detect(%.48q) = %q, want undecided", text, got)
		}
	}
}

func TestShortAndJunkAreUndecided(t *testing.T) {
	for _, text := range []string{
		"*102#.",
		"",
		"Another sms to me",
		// A UCS2 body the modem handed over undecoded. There is no language
		// here to find, and the engine must not be asked to invent one.
		"006F201800630068006900720069006E0067002E000A0034002E00200054006100",
	} {
		if got := Detect(text); got != Unknown {
			t.Errorf("Detect(%.48q) = %q, want undecided", text, got)
		}
	}
}

// The operators spell Uzbek's o' and g' with at least four different marks,
// sometimes several within one message. All of them have to count, or the
// strongest signal in the alphabet becomes the least dependable.
func TestApostropheVariantsAllCount(t *testing.T) {
	const base = "Telefon sozlamalarida rouming maXlumotlari funksiyasini yoqing va tarmoqni qoXlda tanlang hamda qayta yoqing"
	for name, mark := range map[string]string{
		"ascii":          "'",
		"right single":   "’",
		"left single":    "‘",
		"turned comma":   "ʻ",
		"modifier apost": "ʼ",
		"backtick":       "`",
	} {
		text := replaceAll(base, "X", mark)
		if got := Detect(text); got != Uzbek {
			t.Errorf("%s: Detect = %q, want %q", name, got, Uzbek)
		}
	}
}

func replaceAll(s, old, new string) string {
	out := ""
	for _, r := range s {
		if string(r) == old {
			out += new
			continue
		}
		out += string(r)
	}
	return out
}

func TestMarkerCountsOnceHoweverOftenItRepeats(t *testing.T) {
	// "dostupno" four times is one operator template, not four independent
	// reasons to believe anything.
	once := score(russianProfile, "karta dostupno summa")
	many := score(russianProfile, "karta dostupno dostupno dostupno dostupno summa")
	if once != many {
		t.Errorf("repetition changed the score: %d then %d", once, many)
	}
}

func score(p profile, text string) int {
	norm := normalize(text)
	return p.score(norm, wordSet(norm))
}
