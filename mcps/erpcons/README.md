# ERPcons MCP

Cho agent GoClaw **đọc** dữ liệu ERPcons (Drupal) thay mặt đúng người đang hỏi.

Người dùng thấy gì trên màn hình ERPcons thì trợ lý thấy đúng bấy nhiêu — không
hơn. Sidecar này không hiện thực phân quyền nào; nó chuyển tiếp danh tính rồi để
Drupal quyết định, y hệt một phiên trình duyệt.

## Luồng

```
Mở trợ lý   →  Drupal đúc token API riêng cho người đó
               và đẩy sang GoClaw làm credential MCP theo user
Agent cần dữ liệu
            →  GoClaw mở SSE tới sidecar, gắn token của CHÍNH người đó vào header
            →  sidecar hỏi Drupal "người này đọc được gì" (/assistant/tools)
               rồi dựng tool đúng bấy nhiêu
Tool chạy   →  GET thẳng /api/v1/... của ERPcons, mang token đó
```

Token nằm ở header của **kết nối**, không ở từng lời gọi — nên mỗi kết nối phục
vụ đúng một người và không có API nào ở đây nhận `userId` làm tham số.

## Cấu hình

Mọi thứ khác nhau giữa các môi trường nằm ở `.env`, **không** ở compose file —
phải sửa compose file khi deploy là sớm muộn cũng quên.

| Biến `.env` | Mặc định | Ý nghĩa |
| --- | --- | --- |
| `ERPCONS_API_BASE` | **không có** | Gốc REST của ERPcons. Dev: `http://erpcons.localhost/api/v1`. Production: `https://lpc.vn/erpcons/api/v1` |
| `ERPCONS_TIMEOUT_MS` | `20000` | Hạn cho mỗi lời gọi sang Drupal |
| `ERPCONS_MCP_PORT` | `3300` | Chỉ có tác dụng ở dev (xem dưới) |

`ERPCONS_API_BASE` **cố ý không có mặc định**. Một mặc định trỏ về máy dev nghĩa
là quên khai ở production thì sidecar lặng lẽ gọi nhầm địa chỉ; để trống thì
`docker compose` từ chối chạy kèm câu chỉ thẳng vào `.env`.

Chạy cùng GoClaw:

```bash
# thêm docker-compose.erpcons-mcp.yml vào COMPOSE_FILE trong .env
# và khai ERPCONS_API_BASE
docker compose up -d --build erpcons-mcp
docker exec goclaw-erpcons-mcp-1 wget -qO- http://localhost:3300/health
```

### Dev khác production ở đâu

`docker-compose.erpcons-mcp.yml` dùng chung cho mọi môi trường. Phần chỉ đúng ở
máy dev nằm trong `docker-compose.erpcons-local.yml` (vốn đã có sẵn và chỉ được
thêm vào `COMPOSE_FILE` của máy dev):

- `extra_hosts` trỏ `erpcons.localhost` về host gateway, để container gọi được
  WAMP đang chạy trên máy và Host header vẫn khớp vhost Apache.
- Publish cổng 3300 ra host, để chạy tay `test-client.mjs` / `test-chat.mjs`.

Production không cần cả hai: Drupal ở tên miền thật, còn GoClaw gọi sidecar bằng
tên container trong mạng compose.

Rồi thêm MCP server trong giao diện GoClaw:

```
Transport: SSE
URL:       http://erpcons-mcp:3300/sse
Settings:  {"require_user_credentials": true}
Tool prefix: (để trống)
```

Cuối cùng chép UUID của server đó vào `/admin/config/services/erp-assistant`
bên Drupal.

**`require_user_credentials` là bắt buộc.** Thiếu nó, GoClaw mở một kết nối dùng
chung không mang token của ai, và mọi người đều thấy 0 tool.

**Để trống `tool_prefix`** — sidecar đã tự thêm `erp_`, khai thêm là thành
`erp_erp_tasks`.

## Thêm một API cho trợ lý

Sửa `tools/catalog.yml` **bên Drupal** (`web/modules/custom/erp_assistant/`) rồi
xoá cache. Không phải sửa sidecar, không build lại image, không restart GoClaw.
Sidecar cố ý không biết ERPcons có những API nào.

## Hai quy tắc đã trả giá để có

**1. `0 tool` chỉ được mang nghĩa "người này thật sự không có tool".**
Không tải được catalog (Drupal lỗi, token đã bị xoay) thì sidecar trả **503** và
từ chối kết nối, chứ không phục vụ 0 tool. Lý do: GoClaw giữ kết nối trong pool
và đánh dấu là khoẻ — phục vụ 0 tool lúc lỗi sẽ khiến nó cache một kết nối rỗng
và người dùng mất hẳn khả năng tra cứu, im lặng, cho tới khi ai đó khởi động lại
thứ gì đó.

**2. Token chết thì tự đóng kết nối.**
Token đi theo kết nối, nên một kết nối đang mở dùng mãi token nó nhận lúc đầu.
Khi Drupal xoay token (gán lại agent cho một người), sidecar gặp 401 và chủ động
đóng để GoClaw nối lại bằng credential mới.

> `poolTryReconnect` bên GoClaw nối lại bằng **header cũ đã lưu**, nên nó sẽ thử
> lại bằng token chết và bị 503 — đúng như mong muốn: `connected` giữ nguyên
> `false`, và lượt chat kế tiếp mới đọc credential mới từ kho của GoClaw. Đường
> hồi phục mất khoảng 90 giây (health check 30s × 3 lần), sau đó tự lành hoàn
> toàn. Trong khoảng đó trợ lý nói "phiên tra cứu đã hết hiệu lực" chứ không nói
> sai thành "bạn không có quyền".

## Chẩn đoán

```bash
# Người này thấy tool nào?
node test-client.mjs http://localhost:3300/sse <token>

# Gọi thử một tool
node test-client.mjs http://localhost:3300/sse <token> erp_tasks '{"limit":3}'

# Cả đường: hỏi trợ lý một câu cần dữ liệu thật
GOCLAW_GATEWAY_TOKEN=... node test-chat.mjs erpcons 28 "Tôi có bao nhiêu công việc?"
```

Log của sidecar in vòng đời từng phiên (`Mở` / `Đóng` / `Từ chối`) kèm thời gian
tải catalog — đủ để trả lời câu hay gặp nhất: "trợ lý bảo không tra được, là do
đâu".
