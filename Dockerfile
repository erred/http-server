FROM golang:alpine AS build

WORKDIR /app
ENV CGO_ENABLED=0
COPY . .
RUN go build -o /bin/http-server

FROM scratch

COPY --from=build /bin/http-server /bin/

ENTRYPOINT ["/bin/http-server"]
