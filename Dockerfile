FROM ubuntu:latest
COPY kvass /kvass

ENTRYPOINT ["/kvass"]
