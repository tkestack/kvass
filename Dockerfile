FROM ubuntu:latest
COPY build/kvass /kvass

ENTRYPOINT ["/kvass"]
