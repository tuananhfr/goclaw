import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z, type ZodRawShape, type ZodTypeAny } from "zod";
import { ErpClient, ErpRequestError, ToolInputError } from "./erp-client.js";
import type { Catalog, CatalogParam } from "./types.js";

/**
 * Tiền tố tên tool. `erp_tasks`, `erp_advances`...
 *
 * GoClaw còn có `tool_prefix` riêng ở cấu hình MCP server; để trống ô đó, nếu
 * không tên tool thành `erp_erp_tasks`.
 */
const TOOL_PREFIX = "erp_";

/**
 * Dựng một McpServer cho ĐÚNG một người, từ catalog của chính họ.
 *
 * Catalog đã được Drupal lọc theo quyền, nên hai người có thể thấy hai bộ tool
 * khác nhau — đó là chủ ý, không phải lỗi.
 */
export function createMcpServer(catalog: Catalog, client: ErpClient): McpServer {
  const server = new McpServer({ name: "erpcons-mcp", version: "1.0.0" });

  ensureToolCapability(server);

  for (const [name, tool] of Object.entries(catalog)) {
    server.tool(
      TOOL_PREFIX + name,
      tool.description,
      buildSchema(tool.params ?? {}),
      async (args: Record<string, unknown>) => {
        try {
          const data = await client.callTool(tool, args);
          return { content: [{ type: "text" as const, text: JSON.stringify(data) }] };
        } catch (err) {
          // Lỗi trả về dạng `isError` chứ không ném: ném thì GoClaw thấy tool
          // hỏng và lượt trả lời đứt, còn `isError` thì mô hình đọc được câu
          // giải thích rồi nói lại cho người dùng ("bạn không có quyền...").
          return {
            isError: true,
            content: [{ type: "text" as const, text: describe(err) }],
          };
        }
      },
    );
  }

  return server;
}

/**
 * Bắt SDK đăng ký handler `tools/list` kể cả khi chưa có tool nào.
 *
 * =============================================================================
 * VÌ SAO PHẢI CHỌC VÀO HÀM NỘI BỘ
 * =============================================================================
 * `McpServer` chỉ đăng ký handler `tools/list` lúc `.tool()` được gọi lần đầu.
 * Người chưa được cấp quyền tra cứu thì catalog rỗng, không tool nào được đăng
 * ký, và `tools/list` trả về JSON-RPC -32601 "Method not found" — GoClaw đọc đó
 * là MCP server HỎNG rồi thử kết nối lại theo cấp số nhân, trong khi thật ra
 * mọi thứ đang đúng: người này chỉ đơn giản là không có tool nào.
 *
 * SDK không có API công khai cho việc này (đã tra `mcp.js`: `registerCapabilities`
 * chỉ được gọi từ trong `setToolRequestHandlers`). Nuốt lỗi để bản SDK sau có
 * đổi tên hàm thì trường hợp thường gặp — catalog KHÔNG rỗng — vẫn chạy, vì
 * `.tool()` tự đăng ký handler.
 */
function ensureToolCapability(server: McpServer): void {
  try {
    (server as unknown as { setToolRequestHandlers(): void }).setToolRequestHandlers();
  } catch (err) {
    console.warn(
      "[MCP] Không ép được handler tools/list:",
      err instanceof Error ? err.message : err,
    );
  }
}

function describe(err: unknown): string {
  if (err instanceof ToolInputError) return err.message;
  if (err instanceof ErpRequestError) return err.message;
  return "Không lấy được dữ liệu từ ERPcons.";
}

/**
 * Tham số catalog -> schema zod cho MCP SDK.
 *
 * Kiểu lạ rơi về `string`: catalog do người viết tay, gõ nhầm `interger` thì
 * tool vẫn chạy được (Drupal ép kiểu bằng `(int)` ở controller) chứ không biến
 * mất khỏi danh sách.
 */
function buildSchema(params: Record<string, CatalogParam>): ZodRawShape {
  const shape: ZodRawShape = {};

  for (const [name, spec] of Object.entries(params)) {
    let field: ZodTypeAny;

    if (spec.enum && spec.enum.length > 0) {
      // MCP truyền tham số qua JSON; enum số vẫn khai dạng chuỗi cho gọn, phía
      // Drupal đọc bằng `(int)` nên không lệch.
      field = z.enum(spec.enum.map(String) as [string, ...string[]]);
    } else if (spec.type === "integer" || spec.type === "number") {
      field = z.coerce.number();
    } else if (spec.type === "boolean") {
      field = z.coerce.boolean();
    } else {
      field = z.string();
    }

    const description = spec.format === "date" && !spec.description.includes("YYYY")
      ? `${spec.description} (YYYY-MM-DD)`
      : spec.description;

    shape[name] = spec.required
      ? field.describe(description)
      : field.optional().describe(description);
  }

  return shape;
}
