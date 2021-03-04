FROM byrnedo/alpine-curl
COPY kvass /kvass

ENTRYPOINT ["/kvass"]
