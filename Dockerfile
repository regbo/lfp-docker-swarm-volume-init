FROM golang:alpine

RUN apk add --no-cache docker

# Set the Current Working Directory inside the container
WORKDIR $GOPATH/workdir

# Copy everything from the current directory to the PWD (Present Working Directory) inside the container
COPY *.go ./
COPY go.mod go.mod
COPY go.sum go.sum

# Install the package
RUN go get -d -v ./... &&\
    go build -v -o /app &&\
    rm -rf $GOPATH/workdir


COPY entrypoint.sh entrypoint.sh
RUN chmod +x entrypoint.sh

ENTRYPOINT ["./entrypoint.sh"]


