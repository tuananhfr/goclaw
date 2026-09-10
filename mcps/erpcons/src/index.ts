/**
 * MCP server cho ERPcons — cho agent GoClaw ĐỌC dữ liệu ERPcons thay mặt đúng
 * người đang hỏi.
 *
 * =============================================================================
 * LUỒNG
 * =============================================================================
 *   1. Người dùng mở trợ lý  -> Drupal đúc token API riêng cho họ và đẩy sang
 *      GoClaw dưới dạng credential MCP theo user.
 *   2. Agent cần dữ liệu     -> GoClaw mở kết nối SSE tới đây, gắn token của
 *      chính người đó vào header `X-ERP-Assistant-Token`.
 *   3. Sidecar               -> hỏi Drupal "người này được đọc những gì"
 *      (`/assistant/tools`), dựng tool đúng bấy nhiêu.
 *   4. Tool được gọi         -> GET thẳng REST API của ERPcons, mang token đó.
 *
 * Drupal xác thực token thành người dùng thật rồi chạy y hệt một phiên trình
 * duyệt: `_role`/`_permission` của route gác cửa, controller tự ép phạm vi. Nên
 * sidecar này KHÔNG hiện thực phân quyền nào — nó không được phép biết đủ để
 * làm việc đó.
 *
 * =============================================================================
 * MỖI KẾT NỐI MỘT SERVER, KHÔNG DÙNG CHUNG
 * =============================================================================
 * Token nằm ở header của KẾT NỐI, không ở từng lời gọi. Nên danh tính phải bị
 * khoá vào instance được tạo lúc mở kết nối. Chia sẻ một McpServer giữa nhiều
 * kết nối là mở đường cho người này đọc dữ liệu người kia — đúng thứ mà toàn bộ
 * thiết kế token-theo-user sinh ra để chặn.
 */
import express from "express";
import { SSEServerTransport } from "@modelcontextprotocol/sdk/server/sse.js";
import { ErpClient } from "./erp-client.js";
import { createMcpServer } from "./mcp-server.js";

const PORT = parseInt(process.env.PORT ?? "3300", 10);
const ERP_API_BASE = (process.env.ERP_API_BASE ?? "").replace(/\/$/, "");
const REQUEST_TIMEOUT_MS = parseInt(process.env.ERP_TIMEOUT_MS ?? "20000", 10);

const TOKEN_HEADER = "x-erp-assistant-token";

const app = express();
const sessions = new Map<
  string,
  { transport: SSEServerTransport; server: ReturnType<typeof createMcpServer> }
>();

app.get("/health", (_req, res) => {
  res.json({
    status: ERP_API_BASE === "" ? "misconfigured" : "ok",
    erpApiBase: ERP_API_BASE || null,
    activeSessions: sessions.size,
    uptime: process.uptime(),
  });
});

app.get("/sse", async (req, res) => {
  const token = (req.headers[TOKEN_HEADER] as string | undefined) ?? "";

  if (ERP_API_BASE === "") {
    console.error("[SSE] ERP_API_BASE chưa đặt — từ chối kết nối.");
    res.status(500).end();
    return;
  }

  const client = new ErpClient(ERP_API_BASE, token, REQUEST_TIMEOUT_MS);
  let catalog = {};
  const catalogStart = Date.now();

  if (token === "") {
    // Người chưa được cấp token: phục vụ 0 tool thay vì báo lỗi. GoClaw coi lỗi
    // kết nối là sự cố rồi thử lại theo cấp số nhân, trong khi đây là trạng
    // thái BÌNH THƯỜNG của tài khoản chưa bật tra cứu dữ liệu.
    console.warn("[SSE] Không có token — phục vụ 0 tool.");
  } else {
    try {
      catalog = await client.fetchCatalog();
    } catch (err) {
      // ==================================================================
      // KHÔNG TÁCH ĐƯỢC CATALOG THÌ TỪ CHỐI KẾT NỐI, ĐỪNG PHỤC VỤ 0 TOOL
      // ==================================================================
      // "0 tool" phải luôn có nghĩa "người này thật sự không có tool", không
      // bao giờ được mang nghĩa "không hỏi được". GoClaw giữ kết nối trong pool
      // và đánh dấu là khoẻ; nếu ở đây trả về 0 tool khi Drupal lỗi hoặc token
      // đã bị xoay, GoClaw sẽ cache một kết nối RỖNG và người dùng mất hẳn khả
      // năng tra cứu — im lặng, cho tới khi có ai đó khởi động lại thứ gì đó.
      //
      // Đặc biệt nguy hiểm với token đã xoay: `poolTryReconnect` nối lại bằng
      // header CŨ đã lưu, nên nó sẽ "thành công" mãi mãi với một token chết.
      // Từ chối kết nối khiến GoClaw để nguyên trạng thái mất kết nối, và lượt
      // sau `getUserMCPTools` đọc lại credential MỚI từ kho của nó rồi nối lại.
      const message = err instanceof Error ? err.message : String(err);
      console.error(`[SSE] Từ chối kết nối — không tải được catalog: ${message}`);
      res.status(503).end();
      return;
    }
  }
  const catalogMs = Date.now() - catalogStart;

  const toolCount = Object.keys(catalog).length;
  const server = createMcpServer(catalog, client);
  const transport = new SSEServerTransport("/messages", res);
  sessions.set(transport.sessionId, { transport, server });

  const openedAt = Date.now();
  console.log(
    `[SSE] Mở ${transport.sessionId} — ${toolCount} tool, catalog ${catalogMs}ms${token === "" ? ", KHÔNG token" : ""}.`,
  );

  // Token chết -> tự đóng kết nối này.
  //
  // Token nằm ở header của KẾT NỐI, nên một kết nối đang mở sẽ dùng mãi token
  // nó nhận lúc đầu. Khi Drupal xoay token (gán lại agent cho một người) thì
  // kết nối cũ còn sống và mọi lời gọi trả 401 — cho tới khi pool của GoClaw
  // hết hạn nhàn rỗi, tức tối đa 15 phút người dùng không tra cứu được gì.
  //
  // Đóng chủ động ở đây khiến GoClaw thấy mất kết nối, bỏ cache tool của người
  // đó rồi nối lại — lần nối mới lấy credential mới từ kho của nó. Tự lành, không
  // cần Drupal gọi ngược sang GoClaw để báo "tôi vừa xoay token".
  client.onUnauthorized = () => {
    if (!sessions.has(transport.sessionId)) return;
    console.warn(`[SSE] Token hết hiệu lực — đóng ${transport.sessionId} để GoClaw nối lại.`);
    // setImmediate: trả xong phản hồi cho lời gọi hiện tại rồi mới ngắt, nếu
    // không mô hình mất luôn câu giải thích vừa sinh ra.
    setImmediate(() => res.end());
  };

  res.on("close", () => {
    sessions.delete(transport.sessionId);
    server.close().catch(() => {});
    console.log(
      `[SSE] Đóng ${transport.sessionId} sau ${Date.now() - openedAt}ms. Còn ${sessions.size} phiên.`,
    );
  });

  try {
    await server.connect(transport);
  } catch (err) {
    console.error("[SSE] Lỗi connect:", err);
    sessions.delete(transport.sessionId);
  }
});

// KHÔNG gắn middleware phân tích body ở đây: SSEServerTransport đọc raw stream.
app.post("/messages", async (req, res) => {
  const sessionId = req.query.sessionId as string;
  const entry = sessions.get(sessionId);

  if (!entry) {
    console.warn(
      `[MSG] Không thấy phiên ${sessionId}. Đang có: ${[...sessions.keys()].join(", ") || "(không có)"}`,
    );
    res.status(404).json({ error: "Session not found", sessionId });
    return;
  }

  try {
    await entry.transport.handlePostMessage(req, res);
  } catch (err) {
    console.error("[MSG] Lỗi xử lý message:", err);
    res.status(500).json({ error: "Internal error" });
  }
});

app.listen(PORT, () => {
  console.log(`ERPcons MCP server — cổng ${PORT}, ERP_API_BASE=${ERP_API_BASE || "(chưa đặt!)"}`);
});
