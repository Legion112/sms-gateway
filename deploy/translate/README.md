# Translation appliance

A local translation service for `sms-gateway`. One endpoint, one model, no
outbound connections once built.

## Why this exists

These SIMs receive Uzbek, and the operator does not read it. Every hosted
translation API was ruled out by the requirement that message bodies — which
carry one-time codes, card numbers and balances — never leave hardware the
operator controls. That leaves a local model, and the choice of model turned out
to be the whole problem.

**LibreTranslate and Argos Translate cannot do Uzbek at all.** Their package
index carries 100 language pairs and not one of them involves `uz`. This matters
because LibreTranslate is the obvious answer to "local open-source translation",
and it is the wrong one here.

**A general-purpose local LLM is not safe for this traffic.** Asked to translate
the civil-defence advisory below, a 14B instruct model rendered *"pech va
dudburonlar"* (stoves and chimneys) as *"fire and smoke detectors"*, and its
Russian attempt invented a sentence that was not in the original at all
("keep calm and avoid panic"). Uzbek is low-resource for general models and they
fail fluently, which is the dangerous way to fail.

**NLLB-200 is purpose-built for exactly this.** It covers `uzn_Latn` and gets
the same sentence right: *"Keep the furnace and the chimney in order."*

## Why the 600M model and not the 1.3B

`nllb-200-distilled-1.3B` is visibly more fluent, which is tempting. It also
hallucinates currency:

| | `distilled-600M` | `distilled-1.3B` |
|---|---|---|
| `Balansingiz 25000.00 so'mga to'ldirildi` | "Your balance has been filled for 25000.00 **soums**" | "Your balance has been increased to **$25,000.00**" |

On a gateway that forwards bank notifications, a model that turns so'm into
dollars is worse than a clumsy one. The 600M model is the default for that
reason. Change `MODEL` in `compose.yaml` if your traffic has no money in it.

## Sentences are translated one at a time

Handed a whole SMS, this model silently drops trailing clauses. Of six real
messages measured, two lost their last sentence — and in one of them that
sentence was the one carrying the code:

```
DIQQAT! kodni hech kimga bermang! ... hambi tizimiga 0004 kartani qo'shish uchun kod: 1000

whole message:  WARNING! Don't give the code to anyone! It's only requested by scammers.
per sentence:   WARNING! Don't give the code to anyone! It's only asked by fraudsters.
                Code for adding card 0004 to the system: 1000
```

So `server.py` splits on sentence boundaries and translates each piece. That
costs a little cross-sentence context and recovers the content, which is the
better trade for a reader who has no other way to check.

## What it still gets wrong

`is gazi` is carbon monoxide, and the model renders it as "heat gas". It is
also inconsistent about `dudburon` (a chimney or flue), which it has called
both "chimney" and "stoves". The gateway always forwards the original
alongside the translation, which is what makes errors like these recoverable —
read amounts and codes off the original.

## Running it

```bash
docker compose up -d --build
```

The build downloads the model (~2.5 GB) and bakes it into the image, so the
container needs no network afterwards. A cold start takes under a minute to load
the model.

Publish it on the interface the gateway reaches you by, not on all of them:

```bash
TRANSLATE_BIND=192.0.2.10 TRANSLATE_API_KEY=some-shared-secret docker compose up -d
```

Then point the gateway at it:

```yaml
translate:
  enabled: true
  endpoint: http://192.0.2.10:5000
  api_key: some-shared-secret
  target: en
```

and check it without waiting for an SMS:

```bash
sms-gateway translate "Is gazi - ko'rinmas xavf! Isitish pechlari va dudburonlarni tekshiring."
```

## The API

`POST /translate`, which is the LibreTranslate request shape:

```json
{"q": "Xavfsiz xonadon", "source": "uz", "target": "en", "format": "text"}
```

```json
{"translatedText": "A safe home"}
```

`GET /languages` lists what is loaded. Failures come back as
`{"error": "..."}` with a 4xx or 5xx status, and the gateway treats any of them
as "no translation this time".

Nothing about this appliance is special to the gateway: it speaks a published
API, so anything else that speaks it can take its place.

## Where it must not run

Not on a Raspberry Pi 3. The gateway's own Pi has 905 MB of RAM with about 745
MB free, and the 600M model needs most of that to itself. Run this on a machine
with room and let the Pi call it over the LAN — the gateway is built to forward
the original unchanged whenever this service is unreachable, so the appliance
being down or asleep costs the translation and nothing else.
