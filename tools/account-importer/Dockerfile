FROM node:22-bookworm-slim AS node
FROM python:3.12-slim-bookworm AS source
ARG AUTH_SOURCE_COMMIT=44c27a8a2ae5e08d37bb68ab007a335782b43467
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates
RUN git init /opt/any-auto-register \
    && git -C /opt/any-auto-register remote add origin https://github.com/zc-zhangchen/any-auto-register.git \
    && git -C /opt/any-auto-register fetch --depth=1 origin "${AUTH_SOURCE_COMMIT}" \
    && git -C /opt/any-auto-register archive FETCH_HEAD | tar -x -C /opt/any-auto-register

FROM python:3.12-slim-bookworm
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    ACCOUNT_IMPORT_AUTH_SOURCE=/opt/any-auto-register
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=source /opt/any-auto-register/platforms /opt/any-auto-register/platforms
COPY --from=source /opt/any-auto-register/core /opt/any-auto-register/core
COPY --from=source /opt/any-auto-register/services /opt/any-auto-register/services
COPY --from=source /opt/any-auto-register/scripts /opt/any-auto-register/scripts
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY *.py ./
COPY static ./static
RUN python -c 'from local_relogin import _load_upstream, DEFAULT_SOURCE_DIR; _load_upstream(DEFAULT_SOURCE_DIR)' \
    && node --version
USER 10001:10001
CMD ["python", "web_service.py"]
