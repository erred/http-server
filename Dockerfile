FROM golang:alpine AS build

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /bin/http-server

FROM scratch

COPY --from=build /bin/http-server /bin/

ENTRYPOINT ["/bin/http-server"]
