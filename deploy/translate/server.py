"""A translation appliance for sms-gateway, speaking the LibreTranslate API.

Why this exists, and why it is Python when the rest of the project is Go:
translating Uzbek needs NLLB-200, because it is the only open model family that
covers the language at all -- LibreTranslate's own Argos package index has no
Uzbek in it, and a general-purpose LLM small enough to self-host gets this
traffic wrong in ways that matter (one rendered "stoves and chimneys" as "fire
and smoke detectors"). NLLB has no Go runtime that does not need cgo, which
this project does not use. So the model is wrapped here instead, behind the
same HTTP API the gateway would speak to any self-hosted translation server.

Nothing in this file is meant to be edited or extended. It is one endpoint over
a fixed model, run from a container, and the gateway treats it as replaceable:
anything that answers POST /translate the same way will do.

No text leaves this process. The model is loaded from a local directory and
there is no outbound network call on the translation path.
"""

import json
import os
import re
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from transformers import AutoModelForSeq2SeqLM, AutoTokenizer

MODEL_DIR = os.environ.get("MODEL_DIR", "/models/nllb")
HOST = os.environ.get("HOST", "0.0.0.0")
PORT = int(os.environ.get("PORT", "5000"))
API_KEY = os.environ.get("API_KEY", "")
MAX_CHARS = int(os.environ.get("MAX_CHARS", "2000"))
NUM_BEAMS = int(os.environ.get("NUM_BEAMS", "4"))
# The shortest piece worth sending to the model on its own.
MIN_SENTENCE = int(os.environ.get("MIN_SENTENCE", "8"))

# NLLB names languages by script as well as tongue, and these SIMs only ever
# carry the Latin-script Uzbek the operators write.
CODES = {
    "uz": "uzn_Latn",
    "en": "eng_Latn",
    "ru": "rus_Cyrl",
}

print(f"loading {MODEL_DIR}", flush=True)
TOKENIZER = AutoTokenizer.from_pretrained(MODEL_DIR)
MODEL = AutoModelForSeq2SeqLM.from_pretrained(MODEL_DIR)
MODEL.eval()
# One model, one forward pass at a time. The gateway sends a message at a time
# and a lock is cheaper to reason about than a batching queue.
LOCK = threading.Lock()


# Sentence boundaries, keeping the terminator with the sentence it ends.
SENTENCE = re.compile(r"(?<=[.!?])\s+")


def split_sentences(text: str) -> list[str]:
    """Break text into sentences, one per translation call.

    Handed a whole SMS at once, this model silently drops trailing clauses: of
    six real messages measured, two lost their last sentence, and in one of
    them that sentence was the one carrying the code. Translating sentence by
    sentence costs a little cross-sentence context and recovers the content,
    which is the better trade for a message the reader cannot check against
    anything but the original.
    """
    out: list[str] = []
    for part in SENTENCE.split(text.strip()):
        part = part.strip()
        if not part:
            continue
        # An abbreviation ("t.k.", "20-avgust.") splits into fragments too
        # short to translate on their own, where the model invents a sentence.
        # Those are glued back onto what came before them.
        if out and len(part.replace(".", "").strip()) < MIN_SENTENCE:
            out[-1] = out[-1] + " " + part
            continue
        out.append(part)
    return out


def translate_one(text: str, src: str, tgt: str) -> str:
    encoded = TOKENIZER(text, return_tensors="pt", truncation=True, max_length=512)
    generated = MODEL.generate(
        **encoded,
        forced_bos_token_id=TOKENIZER.convert_tokens_to_ids(tgt),
        max_new_tokens=256,
        num_beams=NUM_BEAMS,
    )
    return TOKENIZER.batch_decode(generated, skip_special_tokens=True)[0]


def translate(text: str, source: str, target: str) -> str:
    src, tgt = CODES[source], CODES[target]
    with LOCK:
        TOKENIZER.src_lang = src
        out = [translate_one(part, src, tgt) for part in split_sentences(text)]
    return " ".join(p for p in out if p.strip())


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _send(self, status: int, payload: dict) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:
        if self.path.rstrip("/") in ("/languages", ""):
            self._send(200, [{"code": c, "name": c} for c in CODES])
            return
        self._send(404, {"error": "not found"})

    def do_POST(self) -> None:
        if self.path.rstrip("/") != "/translate":
            self._send(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            self._send(400, {"error": "bad Content-Length"})
            return
        if length <= 0 or length > 64 * 1024:
            self._send(400, {"error": "empty or oversized request"})
            return
        try:
            req = json.loads(self.rfile.read(length))
        except (ValueError, UnicodeDecodeError):
            self._send(400, {"error": "body is not JSON"})
            return

        if API_KEY and req.get("api_key") != API_KEY:
            self._send(403, {"error": "bad api_key"})
            return

        text = (req.get("q") or "").strip()
        source = (req.get("source") or "").lower()
        target = (req.get("target") or "").lower()
        if not text:
            self._send(400, {"error": "q is required"})
            return
        if len(text) > MAX_CHARS:
            self._send(400, {"error": f"q longer than {MAX_CHARS} characters"})
            return
        if source not in CODES or target not in CODES:
            self._send(400, {"error": f"unsupported language pair {source}->{target}"})
            return
        if source == target:
            self._send(400, {"error": "source and target are the same"})
            return

        try:
            out = translate(text, source, target)
        except Exception as exc:  # the gateway treats any failure as "no translation"
            self._send(500, {"error": f"{type(exc).__name__}: {exc}"})
            return
        self._send(200, {"translatedText": out})

    # The default handler logs the request line of every call. These are
    # translation requests, so that line is fine, but the body never is.
    def log_message(self, fmt: str, *args) -> None:
        print("%s - %s" % (self.address_string(), fmt % args), flush=True)


if __name__ == "__main__":
    # Bound before the ready line is printed, so that a port already in use
    # reads as a failure to start rather than as a server that is up.
    server = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"ready on {HOST}:{PORT}", flush=True)
    server.serve_forever()
