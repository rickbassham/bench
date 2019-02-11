FROM golang

RUN go get github.com/aws/aws-sdk-go
RUN go get github.com/docker/docker/client
RUN go get github.com/pkg/errors
RUN go get github.com/codahale/hdrhistogram
RUN go get github.com/go-redis/redis
RUN go get github.com/google/uuid
RUN go get github.com/spf13/viper

COPY . /go/src/github.com/rickbassham/bench/

RUN go build -o /go/bin/benchrunner github.com/rickbassham/bench/cmd/benchrunner

ENV BENCH_API_URL http://172.17.0.1:3000

CMD /go/bin/benchrunner