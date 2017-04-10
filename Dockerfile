FROM golang:1.7.1

RUN mkdir /glide \
    && cd /glide \
    && wget https://github.com/Masterminds/glide/releases/download/v0.12.3/glide-v0.12.3-linux-386.tar.gz \
    && tar -zxf glide-v0.12.3-linux-386.tar.gz \
    && cp linux-386/glide ./ \
    && rm glide-v0.12.3-linux-386.tar.gz \
    && rm -rf linux-386

RUN mkdir -p "$GOPATH/src/github.com/dreyk/kube-nfs-proxy"
COPY ./start.go "$GOPATH/src/github.com/dreyk/kube-nfs-proxy/start/go"
COPY ./glide* "$GOPATH/src/github.com/dreyk/kube-nfs-proxy/"

RUN cd "$GOPATH/src/github.com/dreyk/kube-nfs-proxy" \
    && /glide/glide install

RUN go install "github.com/dreyk/kube-nfs-proxy"