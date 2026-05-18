#syntax=docker/dockerfile:1

FROM python:3.12-slim

WORKDIR /app

ENV PIP_NO_CACHE_DIR=1 \
	PYTHONUNBUFFERED=1 \
	EMBEDDING_WORKER_FAKE=true \
	OCR_ENABLED=false

COPY worker/ .

RUN python -m pip install --no-cache-dir --retries 10 --timeout 60 -e "."

CMD ["python", "main.py"]
