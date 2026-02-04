FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN go build -o /app main.go

FROM alpine:3.19
COPY --from=build /app /app
COPY templates/ /templates/
COPY static/ /static/
WORKDIR /
EXPOSE 8080
CMD ["/app"]
