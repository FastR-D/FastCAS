FROM node:22.22.2-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.26.8-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
COPY sdk/go ./sdk/go
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /fastcas ./cmd/fastcas

FROM alpine:3.22
RUN addgroup -S -g 65532 fastcas && adduser -S -D -u 65532 -G fastcas fastcas && mkdir -p /state /opt/fastcas/web && chown -R fastcas:fastcas /state
COPY --from=backend /fastcas /usr/local/bin/fastcas
COPY --from=web /src/web/dist/ /opt/fastcas/web/
USER fastcas:fastcas
EXPOSE 8900
ENTRYPOINT ["/usr/local/bin/fastcas"]
CMD ["serve"]
