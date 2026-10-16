FROM alpine:3.22

RUN apk add --no-cache tor \
	&& mkdir -p /var/lib/tor \
	&& chown -R tor:tor /var/lib/tor \
	&& chmod 700 /var/lib/tor

COPY torrc /etc/tor/torrc

USER tor
EXPOSE 9050

HEALTHCHECK --interval=15s --timeout=5s --start-period=25s --retries=8 \
	CMD nc -z 127.0.0.1 9050 || exit 1

CMD ["tor", "-f", "/etc/tor/torrc"]
