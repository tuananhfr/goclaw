# Hướng dẫn: Tunnel Ollama local ra VPS qua frp

Cho phép app/goclaw chạy trên VPS dùng **Ollama chạy ở máy local (GPU + model)** làm LLM backend,
thông qua frp — đi ngược lại chiều tunnel của goclaw webhook.

> Trạng thái: **đã dựng & verify chạy thật** (llama3.1:8b trả lời qua tunnel OK).

---

## 1. Mô hình

```
App / goclaw (Docker @ VPS)
        │  gọi HTTP  https://pi1.tekshot-ai.erpcons.vn/v1/...
        ▼
   frps  (VPS  36.50.54.183:7000)      ← vhost route theo Host header
        │
        ▼   frp tunnel (đi ngược về máy local)
   frpc  (Docker "frpc-webhook" @ máy Windows)
        │  localIP = host.docker.internal
        ▼
   Ollama  (máy Windows, cổng 11434)
```

Ollama cung cấp endpoint **OpenAI-compatible** ở `/v1`, nên mọi client OpenAI đều gọi được.

## 2. Thành phần đã có sẵn

| Thành phần | Vị trí | Ghi chú |
|---|---|---|
| frps | VPS `36.50.54.183:7000` | token `123456` (nên đổi) |
| frpc | Docker container `frpc-webhook` (máy local) | mount `frpc.toml`, đã có `extra_hosts: host.docker.internal:host-gateway` |
| Ollama | máy Windows | cổng `11434` |
| Config frpc | `G:\workspace\goclaw-all\goclaw\frpc.toml` | |

---

## 3. Các bước setup

### Bước 1 — Cho Ollama nghe mọi interface (BẮT BUỘC)

frpc chạy trong Docker nên với tới Ollama qua `host.docker.internal`, không phải loopback.
Ollama mặc định chỉ bind `127.0.0.1` → container không nối được. Đổi sang `0.0.0.0`:

```powershell
# Set bền qua reboot (Ollama tray tự đọc lại khi khởi động cùng Windows)
[Environment]::SetEnvironmentVariable("OLLAMA_HOST","0.0.0.0","User")

# Restart Ollama để ăn env mới
Stop-Process -Name "ollama app","ollama" -Force
Start-Process "C:\Users\admin\AppData\Local\Programs\Ollama\ollama app.exe"

# Kiểm tra: phải thấy '::' hoặc '0.0.0.0', KHÔNG phải '127.0.0.1'
Get-NetTCPConnection -State Listen -LocalPort 11434 | Select LocalAddress,LocalPort
```

### Bước 2 — Thêm proxy vào `frpc.toml`

```toml
[[proxies]]
name              = "ollama-pi1"
type              = "http"
localIP           = "host.docker.internal"   # frpc trong Docker; nếu chạy native thì để 127.0.0.1
localPort         = 11434
customDomains     = ["pi1.tekshot-ai.erpcons.vn"]
hostHeaderRewrite = "localhost:11434"         # né lỗi 403 host-check của Ollama
```

> **`hostHeaderRewrite` là mấu chốt:** frpc ghi đè `Host: localhost:11434` khi chuyển request tới
> Ollama. Không có dòng này, Ollama từ chối request đến từ domain lạ.

### Bước 3 — Nạp lại frpc

```powershell
docker restart frpc-webhook
docker logs --tail 10 frpc-webhook    # phải thấy: [ollama-pi1] start proxy success
```

### Bước 4 — Phía VPS (DNS + frps)

- **DNS:** trỏ `pi1.tekshot-ai.erpcons.vn` về VPS (giống `pi4`). Có wildcard `*.tekshot-ai.erpcons.vn` thì khỏi thêm.
- **frps:** cần `vhostHTTPPort` (+ TLS cho HTTPS) — đã sẵn vì `pi4` đang chạy HTTPS. frps tự route theo `customDomains`.

---

## 4. Kiểm chứng

```powershell
# 1) container -> host Ollama
docker exec frpc-webhook wget -qO- http://host.docker.internal:11434/api/version

# 2) qua domain: version + list model
curl https://pi1.tekshot-ai.erpcons.vn/api/version
curl https://pi1.tekshot-ai.erpcons.vn/v1/models

# 3) inference thật (OpenAI-compat)
curl https://pi1.tekshot-ai.erpcons.vn/v1/chat/completions `
  -H "Authorization: Bearer ollama" -H "Content-Type: application/json" `
  -d '{"model":"llama3.1:8b","messages":[{"role":"user","content":"hi"}],"stream":false}'
```

Kết quả mong đợi: (1) `{"version":"0.32.1"}`, (2) danh sách model, (3) có `choices[].message.content`.

---

## 5. Trỏ app/goclaw vào tunnel

Bất kỳ client OpenAI-compat:

| Thông số | Giá trị |
|---|---|
| Base URL | `https://pi1.tekshot-ai.erpcons.vn/v1` |
| API key | để trống / bất kỳ (Ollama nhận mọi Bearer) |
| Model | `llama3.1:8b`, `ministral-3:8b`, … |

**goclaw:** chỉ set Ollama provider host = `https://pi1.tekshot-ai.erpcons.vn` (goclaw tự nối `/v1`).
Có thể set qua env `GOCLAW_OLLAMA_HOST=https://pi1.tekshot-ai.erpcons.vn` hoặc qua UI/DB.
(goclaw sẽ tự nối `/v1` và gửi Bearer giả `"ollama"` — xem `cmd/gateway_providers.go`.)

> ⚠️ Tránh model dạng "thinking" (vd `qwen3.5:*`) nếu để `max_tokens` thấp — phần suy luận ăn hết token, trả `content` rỗng. Dùng instruct model hoặc tăng `max_tokens`.

---

## 6. Bảo mật (nên làm)

Endpoint đang **public** và **Ollama không có auth** → ai biết domain là dùng GPU / pull / xoá model được.
goclaw gửi Bearer giả nên **không dùng được basic-auth của frp** (đụng header `Authorization`). Chặn ở edge VPS:

- **IP-allowlist** đúng IP VPS (vì goclaw gọi ra rồi vòng lại frps), hoặc
- Chỉ cho path `/v1/*` và `/api/tags|chat|generate|embeddings`; **chặn** `/api/pull`, `/api/delete`.
- Đổi token frps `123456` sang chuỗi mạnh.

## 7. Lưu ý vận hành

- Tunnel phụ thuộc: máy Windows bật + Ollama chạy + container `frpc-webhook` chạy.
  Máy sleep/tắt là VPS mất LLM. (Cân nhắc tắt sleep, hoặc để Ollama + Docker auto-start.)
- Config đã persist: `frpc.toml` (mount vào container) + `OLLAMA_HOST` (User env) → sống qua reboot.

## 8. Tái sử dụng cho service khác

Muốn tunnel thêm service local (n8n, ComfyUI, API nội bộ…): copy block ở **Bước 2**,
đổi `name`, `localPort`, `customDomains`. Với service không kén Host header thì bỏ `hostHeaderRewrite`.
