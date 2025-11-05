# ========================================
# Dockerfile - vmqfox-api (multi-arch)
# ========================================

# --------- 階段 1: 編譯 (使用官方 Go 映像) ---------
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

# 安裝必要工具
RUN apk add --no-cache git

# 設定工作目錄
WORKDIR /app

# 複製 go.mod 和 go.sum
COPY go.mod go.sum ./

# 下載依賴
RUN go mod download

# 複製原始碼
COPY . .

# 讀取 go.mod 中的 toolchain（若有）
ARG GO_TOOLCHAIN=auto

# 編譯（靜態連結，無 CGO）
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o vmqfox-api cmd/server/main.go

# --------- 階段 2: 最小執行映像 (scratch) ---------
FROM scratch

# 複製編譯好的二進位檔
COPY --from=builder /app/vmqfox-api /vmqfox-api

# 複製預設 config.yaml（可被 volume 覆蓋）
COPY config.example.yaml /config.yaml

# 暴露端口
EXPOSE 8000

# 啟動命令
ENTRYPOINT ["/vmqfox-api"]
CMD ["--config", "/config.yaml"]
