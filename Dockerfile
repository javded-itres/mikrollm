FROM alpine:3.21 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY dist/mikrollm /mikrollm
EXPOSE 4000
ENTRYPOINT ["/mikrollm"]
CMD ["-data", "/data", "-listen", ":4000"]
