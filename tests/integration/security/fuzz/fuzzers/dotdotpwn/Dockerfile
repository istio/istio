FROM perl:5.32.1

RUN git clone https://github.com/wireghoul/dotdotpwn
WORKDIR /dotdotpwn
COPY run.sh /dotdotpwn/
RUN chmod +x /dotdotpwn/run.sh && apt-get update && apt-get install --no-install-recommends libwww-perl -y && apt-get clean && rm -rf /var/lib/apt/lists/*
CMD [ "sleep", "365d" ]
