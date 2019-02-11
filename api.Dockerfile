FROM golang

COPY . /go/src/github.com/rickbassham/bench/

RUN go build -o /go/bin/benchapi github.com/rickbassham/bench/cmd/benchapi

CMD /go/bin/benchapi
