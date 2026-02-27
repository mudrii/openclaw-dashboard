FROM python:3.12-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash curl jq \
    && rm -rf /var/lib/apt/lists/*

COPY index.html server.py refresh.sh themes.json ./

RUN chmod +x refresh.sh

RUN useradd -r -u 1001 dashboard && \
    mkdir -p /home/dashboard/.openclaw && \
    chown -R dashboard:dashboard /app /home/dashboard
USER dashboard

VOLUME ["/home/dashboard/.openclaw"]

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD python3 -c "import urllib.request; urllib.request.urlopen('http://localhost:8080/')" || exit 1

CMD ["python3", "server.py", "--bind", "0.0.0.0", "--port", "8080"]
