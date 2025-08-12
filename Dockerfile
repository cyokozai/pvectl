FROM golang:1.24-alpine

SHELL [ "ash", "-c" ]

ENV PATH=$PATH:/go/bin
ENV GOCACHE=/tmp/go-cache
ENV GOMODCACHE=/tmp/go-mod-cache

COPY ./app /home/app

WORKDIR /home/app

CMD [ "tail", "-f", "/dev/null" ]