# 📋 Kế Hoạch Nâng Cấp: Multi-Page Support (Option A)

## 🎯 Mục Tiêu
Nâng cấp Facebook MCP Server để hỗ trợ quản lý nhiều Facebook Pages thông qua một SSE connection duy nhất (Token Registry approach). Khắc phục lỗi GoClaw ghi đè page khi thêm page mới.

## 🏗️ Kiến Trúc
- **Session State**: Thay vì mỗi session giữ 1 `GraphClient`, server sẽ giữ một `PageRegistry` (Map quản lý nhiều `GraphClient` cho các page khác nhau).
- **Tool Params**: Tất cả các tools tương tác với Facebook sẽ được thêm tham số `page_id` (optional). Nếu không truyền, hệ thống sẽ sử dụng **default page** (page được add đầu tiên).
- **Bảo mật**: Dựa trên thảo luận, token là vĩnh viễn (long-lived) nên bỏ qua rủi ro token expiration. Tokens sẽ được add thông qua MCP tools.

---

## 🛠️ Task Breakdown

### Phase 1: State Management & Types
- [ ] Định nghĩa interface `PageConfig` chứa `pageId`, `accessToken`, `name` (optional).
- [ ] Cập nhật `src/mcp-server.ts`: Thay vì truyền cứng 1 token/pageId, tạo một lớp `PageRegistry` để quản lý danh sách các `GraphClient`.
- [ ] Chỉnh sửa logic khởi tạo server trong `index.ts`: Vẫn hỗ trợ token mặc định từ headers/env (làm default page) để đảm bảo backward compatibility.

### Phase 2: Cập Nhật GraphClient
- [ ] Review `GraphClient` (`src/graph-client.ts`) để đảm bảo các thao tác get/post hoàn toàn độc lập và stateless (hiện tại đã stateless).
- [ ] Thêm phương thức để test token (gọi GET `/{pageId}` hoặc `/me`) để validate token ngay khi đăng ký.

### Phase 3: Thêm Tools Quản Lý Page (Mới)
- [ ] `fb_add_page`: Tool nhận `page_id`, `access_token`, `name` (optional). Khởi tạo `GraphClient`, validate token, thêm vào registry.
- [ ] `fb_list_pages`: Liệt kê các page đang có trong registry, đánh dấu page nào là default.
- [ ] `fb_remove_page`: Xóa page khỏi registry.
- [ ] `fb_set_default_page`: Thay đổi page mặc định.

### Phase 4: Refactor 15 MCP Tools Hiện Tại
Thêm tham số `page_id` (optional) vào schema của TẤT CẢ các tools hiện có.

**4.1. Posts (`src/tools/posts.ts`)**
- [ ] `fb_create_post`, `fb_create_post_with_media`, `fb_edit_post`, `fb_delete_post`, `fb_schedule_post`, `fb_get_posts`.

**4.2. Media (`src/tools/media.ts`)**
- [ ] `fb_upload_photo`, `fb_create_photo_post`.

**4.3. Comments (`src/tools/comments.ts`)**
- [ ] `fb_get_comments`, `fb_create_post_comment`, `fb_reply_comment`, `fb_delete_comment`, `fb_hide_comment`.

**4.4. Insights (`src/tools/insights.ts`)**
- [ ] `fb_get_post_insights`, `fb_get_page_info`, `fb_get_rate_limit`.

*Note: Cập nhật hàm xử lý để lấy đúng `GraphClient` từ registry dựa trên `page_id`. Nếu `page_id` trống, lấy default page. Trả về error prefix `[Page: X]` để AI dễ debug.*

---

## ✅ Tiêu Chí Nghiệm Thu (Verification Checklist)
- [ ] Có thể add thêm 1 page mới thông qua tool `fb_add_page` mà không mất page ban đầu.
- [ ] Tool `fb_list_pages` trả về chính xác danh sách > 1 pages.
- [ ] Tool cũ (không truyền `page_id`) vẫn hoạt động bình thường trên default page.
- [ ] Thao tác post/đọc với `page_id` cụ thể hoạt động đúng trên page đó.
- [ ] Lỗi token không hợp lệ được báo ngay lúc chạy `fb_add_page`.
