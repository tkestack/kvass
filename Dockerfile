FROM curlimages/curl
ADD kvass /bin/kvass

# On busybox 'nobody' has uid `65534'
USER 65534

ENTRYPOINT ["/bin/kvass"]
