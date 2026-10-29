# The translation appliance for sms-gateway.
#
# The model is downloaded at build time and copied into the final image, so the
# running container makes no outbound connection at all: once built, this
# translates with the network unplugged. That is the point -- the operator's
# requirement was that message text never leaves hardware they control.

ARG MODEL=facebook/nllb-200-distilled-600M

FROM python:3.12-slim AS model
ARG MODEL
RUN pip install --no-cache-dir "huggingface_hub==0.25.2"
# Weights, tokenizer and config only. Nothing here needs the repo's extras.
RUN python -c "\
from huggingface_hub import snapshot_download; \
snapshot_download('${MODEL}', local_dir='/models/nllb', \
    allow_patterns=['*.json', '*.model', '*.bin', '*.txt'])"

FROM python:3.12-slim
# CPU torch: this runs a 600M-parameter model on short text, where a GPU buys
# a second or two and costs a far larger image and a driver dependency.
RUN pip install --no-cache-dir torch==2.4.1 --index-url https://download.pytorch.org/whl/cpu \
    && pip install --no-cache-dir transformers==4.44.2 sentencepiece==0.2.0 \
    && rm -rf /root/.cache/pip

COPY --from=model /models/nllb /models/nllb
COPY server.py /app/server.py

ENV MODEL_DIR=/models/nllb \
    HOST=0.0.0.0 \
    PORT=5000 \
    HF_HUB_OFFLINE=1 \
    TRANSFORMERS_OFFLINE=1

EXPOSE 5000

# Loading the model takes the better part of a minute on a cold start, which is
# what start-period covers.
HEALTHCHECK --interval=30s --timeout=10s --start-period=120s --retries=3 \
    CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:5000/languages', timeout=5)"

CMD ["python", "-u", "/app/server.py"]
