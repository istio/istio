FROM python:3

RUN git clone https://github.com/ticarpi/jwt_tool
WORKDIR /jwt_tool
COPY run.sh .
COPY jwtconf.ini .
COPY sample-RSA-private.pem .
COPY sample-RSA-public.pem .
RUN chmod +x ./run.sh && chmod +x jwt_tool.py && apt-get update && apt-get install --no-install-recommends -y python3-pip \
  && apt-get clean && rm -rf /var/lib/apt/lists/*
RUN python3 -m pip install --no-cache-dir termcolor==1.1.0 cprint==1.2.2 pycryptodomex==3.10.1 requests==2.25.1
CMD [ "sleep", "365d" ]
