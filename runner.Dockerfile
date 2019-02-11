FROM golang

COPY . /go/src/github.com/rickbassham/bench/

RUN go build -o /go/bin/benchrunner github.com/rickbassham/bench/cmd/benchrunner

ENV BENCH_API_URL http://172.17.0.1:3000

CMD /go/bin/benchrunner
