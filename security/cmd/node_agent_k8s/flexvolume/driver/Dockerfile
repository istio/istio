FROM alpine:3.7

RUN mkdir -p /usr/local/bin
ADD driver.sh /usr/local/bin/
ADD driver /usr/local/bin/

RUN chmod +x /usr/local/bin/driver.sh

ENTRYPOINT ["/usr/local/bin/driver.sh"]
