FROM golang:alpine AS build

WORKDIR $GOPATH/workdir

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -v -o /app

FROM docker:latest

WORKDIR /

COPY --from=build /app /app

COPY entrypoint.sh entrypoint.sh
RUN chmod +x entrypoint.sh

ENTRYPOINT ["./entrypoint.sh"]


