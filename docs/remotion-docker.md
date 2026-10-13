# Remotion skill và render MP4 trên GoClaw

Skill tổng hợp chính thức nằm ở `skills/remotion-best-practices/`, kèm toàn bộ tài liệu con. Nguồn và commit được ghi trong `UPSTREAM.json`; `GOCLAW.md` bổ sung cách chạy trong container.

`Dockerfile.remotion` bổ sung Node/npm, Chromium, FFmpeg, Noto và Remotion 4.0.535 vào image gateway đã có. Image nền được ghim theo digest của bản đang chạy khi cài, nên đây là cập nhật runtime/skill, không phải cập nhật binary GoClaw. Muốn nâng gateway, đặt `GOCLAW_BASE_IMAGE` thành tag hoặc digest đã kiểm tra rồi build lại.

## Build và cập nhật container

Chạy ở gốc repo:

```sh
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.remotion.yml build goclaw
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.remotion.yml up -d --no-deps goclaw
```

Trên triển khai có overlay khác, giữ các overlay đó và thêm `docker-compose.remotion.yml` ở cuối. Dùng đúng tập compose file của triển khai: `.env` trong checkout có thể chứa tập khác với container đang chạy. Lệnh `up` thay container gateway; chờ các job đang chạy kết thúc trước khi thực hiện. Các volume data/workspace/PostgreSQL được giữ nguyên.

Gateway tự seed skill vào database và thư mục version trong data volume khi khởi động. Kiểm tra ở trang Skills: `remotion-best-practices` phải được bật và active. Agent có danh sách skill giới hạn cần được gán thêm slug này; agent cần quyền dùng `exec`, đọc/ghi file. Agent chạy qua sandbox riêng phải có runtime tương ứng trong image sandbox.

Kiểm tra lại render với âm thanh, dưới user không phải root:

```sh
docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.remotion.yml exec -T --user goclaw goclaw sh -s /app/workspace/remotion-check-new < docker/remotion/smoke-test.sh
```

Đường dẫn thử phải chưa tồn tại; script từ chối ghi đè. Nó xuất video 720p, 30 fps, 2 giây với tiếng Việt và một đoạn âm thanh, rồi in thông tin từ ffprobe.

## Tạo và render bằng agent

Ví dụ yêu cầu:

> Dùng skill remotion-best-practices, tạo video giới thiệu Tekshot 10 giây, tỷ lệ 9:16, chữ tiếng Việt. Render thành MP4 và trả file.

Skill hướng dẫn agent sao chép mẫu từ `/opt/remotion/template` vào workspace của nó, dùng thư viện có sẵn ở `/opt/remotion/node_modules`, rồi gọi `remotion render`. Có thể kiểm tra thủ công từ thư mục dự án:

```sh
remotion compositions src/index.tsx
remotion render src/index.tsx Hello out/hello.mp4 --concurrency=1
ffprobe -v error -show_entries format=duration,size -of json out/hello.mp4
```

MP4 và mã nguồn nằm trong workspace volume. Không cài package khi tạo video cơ bản. Khi cần thư viện khác, dùng cơ chế duyệt cài package có sẵn của GoClaw.

## Kiểm chứng lúc cài (12/10/2026)

- Image `goclaw-remotion:local` đã được build và đưa vào container `goclaw-goclaw-1`; health check trả `status: ok`.
- Database đã seed `remotion-best-practices`, trạng thái `active`, `enabled = true`, `is_system = true`.
- Render dưới uid 1000, bỏ mọi capability, bật no-new-privileges, không có mạng ngoài: video H.264 1280×720, 30 fps, 60 frame, âm thanh AAC, dài 2.005 giây. Frame xem thử hiển thị đúng dấu tiếng Việt.
- Mẫu đã render và mã nguồn được giữ trong volume ở `/app/workspace/remotion-demo`, MP4 ở `out/hello.mp4`.
- Kiểm chứng runtime/skill; chưa chạy một lượt hội thoại LLM yêu cầu agent tạo video.

## Giới hạn vận hành

- Gateway và render chia sẻ CPU/RAM. Cấu hình mặc định render một worker; chưa có hàng đợi chung cho các lượt render của nhiều agent.
- Remotion khuyến nghị Debian cho hệ thống render chuyên dụng. Bản này giữ Alpine của GoClaw và dùng Chromium hệ thống cùng compositor musl; khi nâng Chromium/Remotion phải render thử lại.
- Studio preview chưa được publish ra host. Cài này phục vụ tạo code và xuất MP4 trong container.
- Đây không thêm API video vào Tekshot hoặc thay luồng `studio_video` hiện tại.
- License thương mại của Remotion áp dụng riêng: https://www.remotion.dev/license.

## Cập nhật skill và rollback

Khi lấy phiên bản upstream mới, giữ lại phần tích hợp trong `SKILL.md` và `GOCLAW.md`, cập nhật `UPSTREAM.json`, và cập nhật đồng bộ phiên bản các gói `remotion`/`@remotion/*` trong `docker/remotion/package.json`. Tạo lại lockfile rồi build image và render thử trước khi đổi container.

Rollback runtime/skill image về gateway trước khi cài:

```sh
docker compose -f docker-compose.yml -f docker-compose.postgres.yml up -d --no-deps --pull never goclaw
```

Skill đã seed vẫn nằm trong database/data volume. Khi rollback runtime, tắt `remotion-best-practices` ở trang Skills để agent không dùng runtime không còn trong image.
