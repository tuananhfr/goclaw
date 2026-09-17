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

const DIRECTORY_MODE = "directory";

export interface CapabilitySearch {
  query?: string;
  domain?: string;
  kind?: string;
  limit?: number;
}

export interface CapabilitySummary {
  key: string;
  label: string;
  domain: string;
  kind: string;
  description: string;
  requiredParams: string[];
  optionalParams: string[];
  params: Record<string, CatalogParam>;
}

/**
 * Dựng một McpServer cho ĐÚNG một người, từ catalog của chính họ.
 *
 * Catalog đã được Drupal lọc theo quyền, nên hai người có thể thấy hai bộ tool
 * khác nhau — đó là chủ ý, không phải lỗi.
 */
export function createMcpServer(catalog: Catalog, client: ErpClient): McpServer {
  const server = new McpServer({ name: "erpcons-mcp", version: "1.0.0" });

  ensureToolCapability(server);

  if (toolMode() === DIRECTORY_MODE) {
    registerDirectoryTools(server, catalog, client);
  } else {
    registerLegacyTools(server, catalog, client);
  }

  return server;
}

/**
 * Hai tool cố định thay cho hàng trăm schema endpoint.
 *
 * `erp_find` chỉ tìm trong catalog đã được Drupal lọc cho đúng user của kết
 * nối. `erp_get` nhận capability key, tuyệt đối không nhận URL do mô hình tự
 * đặt. Vì vậy directory mode giảm context mà không nới quyền đọc.
 */
function registerDirectoryTools(server: McpServer, catalog: Catalog, client: ErpClient): void {
  server.tool(
    "erp_find",
    "Tìm khả năng đọc dữ liệu ERPcons phù hợp với câu hỏi. Gọi tool này trước khi chưa biết capability key; kết quả đã được giới hạn theo quyền của người đang hỏi.",
    {
      query: z.string().optional().describe("Từ khoá nghiệp vụ, ví dụ: dự án, hợp đồng, chấm công."),
      domain: z.string().optional().describe("Nhóm nghiệp vụ nếu đã biết, ví dụ projects, hr, documents."),
      kind: z.string().optional().describe("Kiểu dữ liệu: list, detail, lookup, report hoặc support."),
      limit: z.coerce.number().int().min(1).max(20).optional().describe("Số capability tối đa, mặc định 10."),
    },
    async (args: CapabilitySearch) => ({
      content: [{
        type: "text" as const,
        text: JSON.stringify({ capabilities: findCapabilities(catalog, args) }),
      }],
    }),
  );

  server.tool(
    "erp_get",
    "Đọc dữ liệu bằng một capability key do erp_find trả về. Chỉ capability nằm trong catalog của người đang hỏi mới được gọi; không chấp nhận URL hoặc HTTP method.",
    {
      capability: z.string().min(1).describe("Capability key chính xác do erp_find trả về."),
      params: z.record(z.unknown()).optional().describe("Tham số path/query đúng theo mô tả capability."),
    },
    async ({ capability, params }: { capability: string; params?: Record<string, unknown> }) => {
      try {
        const data = await callCapability(catalog, client, capability, params ?? {});
        return { content: [{ type: "text" as const, text: JSON.stringify(data) }] };
      } catch (err) {
        return {
          isError: true,
          content: [{ type: "text" as const, text: describe(err) }],
        };
      }
    },
  );
}

function registerLegacyTools(server: McpServer, catalog: Catalog, client: ErpClient): void {

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
}

/** Tìm capability trong duy nhất catalog đã cấp cho kết nối hiện tại. */
export function findCapabilities(catalog: Catalog, search: CapabilitySearch): CapabilitySummary[] {
  const queryTokens = normalize(search.query).split(/\s+/).filter(Boolean);
  const domain = normalize(search.domain);
  const kind = normalize(search.kind);
  const limit = Math.min(20, Math.max(1, Math.trunc(search.limit ?? 10)));

  return Object.entries(catalog)
    .filter(([, tool]) => tool.discoverable !== false)
    .map(([key, tool]) => {
      const paramEntries = Object.entries(tool.params ?? {});
      const summary: CapabilitySummary = {
        key,
        label: tool.label?.trim() || key,
        domain: tool.domain?.trim() || "other",
        kind: tool.kind?.trim() || "read",
        description: tool.description,
        requiredParams: paramEntries.filter(([, spec]) => spec.required).map(([name]) => name),
        optionalParams: paramEntries.filter(([, spec]) => !spec.required).map(([name]) => name),
        params: tool.params ?? {},
      };
      const haystack = normalize([
        summary.key,
        summary.label,
        summary.domain,
        summary.kind,
        summary.description,
      ].join(" "));
      return { summary, haystack };
    })
    .filter(({ summary, haystack }) =>
      (!domain || normalize(summary.domain) === domain)
      && (!kind || normalize(summary.kind) === kind)
      && (queryTokens.length === 0 || queryTokens.every((token) => haystack.includes(token))))
    .sort((a, b) => a.summary.key.localeCompare(b.summary.key, "vi"))
    .slice(0, limit)
    .map(({ summary }) => summary);
}

/** Gọi bằng key; URL luôn lấy từ catalog, không bao giờ lấy từ đầu vào LLM. */
export async function callCapability(
  catalog: Catalog,
  client: Pick<ErpClient, "callTool">,
  capability: string,
  params: Record<string, unknown>,
): Promise<unknown> {
  const key = capability.trim();
  const tool = catalog[key];
  if (!tool) {
    throw new ToolInputError(`Capability "${key}" không có trong danh mục được cấp cho bạn.`);
  }
  return client.callTool(tool, params);
}

function toolMode(): string {
  return (process.env.ERPCONS_MCP_TOOL_MODE ?? DIRECTORY_MODE).trim().toLowerCase();
}

function normalize(value: unknown): string {
  return String(value ?? "").trim().toLocaleLowerCase("vi");
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
