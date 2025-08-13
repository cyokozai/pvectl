FROM golang:1.24-alpine

SHELL [ "ash", "-c" ]

ENV PATH=$PATH:/go/bin
ENV GOCACHE=/tmp/go-cache
ENV GOMODCACHE=/tmp/go-mod-cache

COPY ./app /home/app

WORKDIR /home

RUN go mod init github.com/cyokozai/pvectl && \
    go get gopkg.in/yaml.v3@latest && \
    go get github.com/google/go-cmp/cmp@latest && \
    go get github.com/Telmate/proxmox-api-go@latest && \
    go install golang.org/x/tools/cmd/goimports@latest && \
    go install golang.org/x/lint/golint@latest

WORKDIR /home/app

CMD [ "tail", "-f", "/dev/null" ]