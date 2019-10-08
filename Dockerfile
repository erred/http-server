FROM golang:alpine AS build

ENV CGO_ENABLED=0
WORKDIR /app
COPY . .
RUN go build -mod=vendor -o /bin/app

FROM scratch

COPY --from=build /bin/app .

ENTRYPOINT ["/app"]
CMD ["/workspace"]
