FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy pre-built binary (built by CI/goreleaser)
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM:-.}/tudomesh /app/tudomesh

# Data directory for config, calibration, and floorplan
VOLUME /data

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/tudomesh"]
CMD ["--http", "--mqtt", "--data-dir=/data"]
