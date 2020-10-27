FROM ubuntu:18.04
COPY kvass /kvass

ENTRYPOINT ["/kvass"]
