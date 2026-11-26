FROM node@sha256:7c400d78797d8cbe3f6bb21d2227b86af4cd24b0014cc9f62ff989cc16de988e
RUN npm install -g @moonshot-ai/kimi-code@2.8.0 \
    && mkdir -p /tmp/kimi-code /run/kimi-web /srv/autoresearch-private/kimi-sessions \
    && chown -R 65532:65532 /tmp/kimi-code /run/kimi-web /srv/autoresearch-private/kimi-sessions
COPY deploy/kimi-token-relay.sh /usr/local/bin/autoresearch-kimi-token-relay
RUN chmod 0755 /usr/local/bin/autoresearch-kimi-token-relay
ENV KIMI_CODE_HOME=/tmp/kimi-code
USER 65532:65532
ENTRYPOINT ["kimi"]
CMD ["web", "--no-open", "--host", "0.0.0.0", "--port", "58627", "--allowed-host", "kimi-web", "--log-level", "silent"]
