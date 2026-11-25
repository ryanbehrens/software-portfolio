# Build a single static binary; the page, images and apps.json are embedded in it.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /portfolio .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /portfolio /portfolio
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/portfolio"]
