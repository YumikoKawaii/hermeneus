FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hermeneus ./cmd/hermeneus

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hermeneus /hermeneus
EXPOSE 9000
ENTRYPOINT ["/hermeneus"]
