FROM ubuntu:latest
RUN apt update && apt install -y curl
COPY kvass /kvass
ENTRYPOINT ["/kvass"]