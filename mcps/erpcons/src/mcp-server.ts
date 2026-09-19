import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z, type ZodRawShape, type ZodTypeAny } from "zod";
import { registerBrandingTool } from "./branding.js";
import { ErpClient, ErpRequestError, ToolInputError } from "./erp-client.js";
import { intentKey, type Favorite, type RouteMemory, type RouteSuggestion } from "./route-memory.js";
import { normalize, tokenize } from "./text.js";
import type { Catalog, CatalogParam, CatalogTool } from "./types.js";

export { tokenize };

/**
 * Tiền tố tên tool. `erp_tasks`, `erp_advances`...
 *
 * GoClaw còn có `tool_prefix` riêng ở cấu hình MCP server; để trống ô đó, nếu
 * không tên tool thành `erp_erp_tasks`.
 */
const TOOL_PREFIX = "erp_";

const DIRECTORY_MODE = "directory";

/**
 * Trong khoảng này, một câu hỏi chỉ được nhớ với API ĐẦU TIÊN ra dữ liệu.
 *
 * Một câu hỏi thường kéo theo vài lời gọi phụ (xem hồ sơ để biết "tôi" là ai,
 * mở chi tiết từng dòng). Nhớ cả chúng là dạy sai: đo thật, "báo cáo Dplan của
 * tôi" từng bị nhớ thành `profile`.
 */
const INTENT_WINDOW_MS = 2 * 60_000;

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
  /** Key capability xem chi tiết một dòng của danh sách này. */
  detail?: string;
  /** Người này đã từng lấy được dữ liệu bằng capability này cho câu tương tự. */
  learned?: boolean;
}

/** Bộ nhớ định tuyến gắn với người của kết nối hiện tại. */
export interface RouteContext {
  memory: RouteMemory;
  userKey: string;
}

/**
 * Dựng một McpServer cho ĐÚNG một người, từ catalog của chính họ.
 *
 * Catalog đã được Drupal lọc theo quyền, nên hai người có thể thấy hai bộ tool
 * khác nhau — đó là chủ ý, không phải lỗi.
 */
export function createMcpServer(catalog: Catalog, client: ErpClient, routing?: RouteContext): McpServer {
  const server = new McpServer({ name: "erpcons-mcp", version: "1.0.0" });

  ensureToolCapability(server);

  if (toolMode() === DIRECTORY_MODE) {
    registerDirectoryTools(server, catalog, client, routing);
  } else {
    registerLegacyTools(server, catalog, client);
  }
  // Catalog rỗng = người chưa được bật tra cứu (hoặc không có token): không có
  // danh tính nào để hỏi Drupal công ty của họ.
  if (Object.keys(catalog).length > 0) {
    registerBrandingTool(server, client);
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
function registerDirectoryTools(
  server: McpServer,
  catalog: Catalog,
  client: ErpClient,
  routing?: RouteContext,
): void {
  const allowed = new Set(Object.keys(catalog));
  // Mô hình thường gọi erp_get ngay sau erp_find mà quên `intent`; câu tìm kiếm
  // vừa rồi là thứ gần nhất với ý người hỏi.
  let lastQuery = "";
  const recorded = new Map<string, number>();

  server.tool(
    "erp_find",
    "Tra mô tả tham số của capability ERPcons, hoặc tìm capability khi mục lục trong erp_get không có mục phù hợp (kể cả tool tra cứu `lookup`). Kết quả đã giới hạn theo quyền của người đang hỏi.",
    {
      query: z.string().optional().describe("Từ khoá nghiệp vụ, ví dụ: dự án, hợp đồng, chấm công."),
      domain: z.string().optional().describe("Nhóm nghiệp vụ nếu chắc chắn: hr, timekeeping, recruitment, kpi, applications, finance, proposals, projects, sales, orders, crm, documents, prices, planning, tasks, system. Không chắc thì bỏ trống."),
      kind: z.string().optional().describe("Kiểu dữ liệu: list, detail, lookup, report hoặc support."),
      limit: z.coerce.number().int().min(1).max(20).optional().describe("Số capability tối đa, mặc định 10."),
    },
    async (args: CapabilitySearch) => {
      if (args.query?.trim()) lastQuery = args.query.trim();
      const learned = routing && args.query
        ? routing.memory.suggest(routing.userKey, args.query, allowed)
        : [];
      return {
        content: [{
          type: "text" as const,
          text: JSON.stringify({ capabilities: findCapabilities(catalog, args, learned) }),
        }],
      };
    },
  );

  // Chốt lúc mở kết nối: GoClaw giữ kết nối của mỗi người tới 15 phút nhàn rỗi,
  // nên thói quen mới học chỉ hiện ở đây từ lần nối sau. Trong lúc đó erp_find
  // vẫn đọc kho trực tiếp.
  const favorites = routing ? routing.memory.favorites(routing.userKey, allowed) : [];

  server.tool(
    "erp_get",
    buildGetDescription(catalog, favorites),
    {
      capability: z.string().min(1).describe("Capability key chính xác, lấy từ mục lục hoặc erp_find."),
      params: z.record(z.unknown()).optional().describe("Tham số path/query đúng theo mục lục hoặc mô tả capability."),
      intent: z.string().max(120).optional().describe("Câu hỏi GỐC của người dùng trong lượt này, giữ nguyên chữ, ví dụ: cho tôi báo cáo t8. Mọi lời gọi trong cùng lượt dùng cùng một câu; không mô tả bước đang làm."),
    },
    async ({ capability, params, intent }: {
      capability: string;
      params?: Record<string, unknown>;
      intent?: string;
    }) => {
      const key = capability.trim();
      try {
        const data = await callCapability(catalog, client, key, params ?? {});
        const asked = intent?.trim() || lastQuery;
        // Chỉ nhớ lần gọi RA DỮ LIỆU: danh sách rỗng thường là chọn nhầm API,
        // nhớ nó là dạy sai cho lần sau.
        const askedKey = intentKey(asked);
        const now = Date.now();
        if (routing && askedKey !== "" && hasData(data) && now - (recorded.get(askedKey) ?? 0) > INTENT_WINDOW_MS) {
          recorded.set(askedKey, now);
          routing.memory.record(routing.userKey, asked, key);
        }
        return { content: [{ type: "text" as const, text: JSON.stringify(data) }] };
      } catch (err) {
        const help = err instanceof ToolInputError && catalog[key] ? `\n${describeParams(catalog[key])}` : "";
        return {
          isError: true,
          content: [{ type: "text" as const, text: describe(err) + help }],
        };
      }
    },
  );
}

/**
 * Mô tả `erp_get` kèm MỤC LỤC capability của đúng người này.
 *
 * Mục lục nằm sẵn trong ngữ cảnh nên mô hình chọn key ngay, khỏi tốn một lượt
 * suy luận cho erp_find ở mỗi câu hỏi — và nó hiểu đồng nghĩa ("tăng ca" = "làm
 * thêm giờ") tốt hơn mọi thuật toán khớp chữ. GoClaw chép 200 byte đầu của mô tả
 * vào system prompt, nên câu hướng dẫn phải đứng đầu.
 *
 * `lookup` bị bỏ khỏi mục lục: đó là dữ liệu phụ cho form, hiếm khi là câu trả
 * lời, mà chiếm gần 1/5 độ dài. Vẫn tìm được qua erp_find.
 */
export function buildGetDescription(catalog: Catalog, favorites: Favorite[] = []): string {
  const lines = [
    "Đọc dữ liệu ERPcons: chọn key trong MỤC LỤC dưới đây rồi gọi thẳng, luôn điền `intent`. Chỉ gọi erp_find khi cần mô tả tham số hoặc mục lục không có mục phù hợp.",
    "Chỉ capability trong danh mục của người đang hỏi mới gọi được; không nhận URL. Tham số có * là bắt buộc.",
  ];

  if (favorites.length > 0) {
    lines.push("", "NGƯỜI NÀY HAY HỎI (ưu tiên):");
    for (const { intent, capability } of favorites) lines.push(`- ${intent} -> ${capability}`);
  }

  const byDomain = new Map<string, string[]>();
  for (const [key, tool] of Object.entries(catalog)) {
    if (tool.discoverable === false || tool.kind === "lookup") continue;
    const domain = tool.domain?.trim() || "other";
    const params = Object.entries(tool.params ?? {})
      .map(([name, spec]) => (spec.required ? `${name}*` : name))
      .join(", ");
    const label = tool.label?.trim() || key;
    const row = `${key} — ${label}${params ? ` (${params})` : ""}`;
    byDomain.set(domain, [...(byDomain.get(domain) ?? []), row]);
  }

  lines.push("", "MỤC LỤC:");
  for (const domain of [...byDomain.keys()].sort()) {
    lines.push(`[${domain}]`, ...byDomain.get(domain)!.sort());
  }
  return lines.join("\n");
}

/** Danh sách tham số hợp lệ, gửi kèm lỗi để mô hình tự sửa ngay lượt sau. */
function describeParams(tool: CatalogTool): string {
  const entries = Object.entries(tool.params ?? {});
  if (entries.length === 0) return "Capability này không nhận tham số nào.";
  const rows = entries.map(([name, spec]) => {
    const extra = [spec.type, spec.enum ? `một trong: ${spec.enum.join(", ")}` : "", spec.format ?? ""]
      .filter(Boolean)
      .join("; ");
    return `- ${name}${spec.required ? " (bắt buộc)" : ""} [${extra}]: ${spec.description}`;
  });
  return `Tham số hợp lệ:\n${rows.join("\n")}`;
}

/** Phản hồi có ít nhất một dòng dữ liệu hay chỉ là vỏ rỗng. */
export function hasData(data: unknown): boolean {
  if (data === null || typeof data !== "object") return false;
  for (const [key, value] of Object.entries(data as Record<string, unknown>)) {
    if (key === "total") {
      if (typeof value === "number" && value > 0) return true;
    } else if (Array.isArray(value)) {
      if (value.length > 0) return true;
    } else if (value !== null && typeof value === "object" && Object.keys(value).length > 0) {
      return true;
    }
  }
  return false;
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

/**
 * Tìm capability trong duy nhất catalog đã cấp cho kết nối hiện tại.
 *
 * Chấm điểm thay vì bắt khớp MỌI từ: người dùng hỏi thừa một chữ ("danh sách
 * phiếu xuất kho của tôi") là kiểu khớp-hết trả rỗng, và mô hình kết luận
 * "không có dữ liệu" trong khi capability nằm ngay đó.
 *
 * Có từ khoá thì `domain`/`kind` chỉ CỘNG ĐIỂM, không loại: mô hình hay đoán
 * nhóm sai ("làm thêm giờ" -> hr, trong khi nó ở proposals), và lọc cứng là
 * ra rỗng rồi thử lại 3-4 lượt. Không có từ khoá thì vẫn lọc — lúc đó mô hình
 * đang cố ý duyệt một nhóm.
 */
export function findCapabilities(
  catalog: Catalog,
  search: CapabilitySearch,
  learned: RouteSuggestion[] = [],
): CapabilitySummary[] {
  const queryTokens = tokenize(search.query);
  const domain = normalize(search.domain);
  const kind = normalize(search.kind);
  const limit = Math.min(20, Math.max(1, Math.trunc(search.limit ?? 10)));
  const learnedScore = new Map(learned.map(({ capability, score }) => [capability, score]));
  const details = detailIndex(catalog);

  const entries = summarize(catalog).map((entry) => {
    const detail = details.get(entry.summary.key);
    const summary = { ...entry.summary };
    if (detail) summary.detail = detail;
    if (learnedScore.has(summary.key)) summary.learned = true;
    return { ...entry, summary };
  });
  const inDomain = (summary: CapabilitySummary) => !domain || normalize(summary.domain) === domain;
  const ofKind = (summary: CapabilitySummary) => !kind || normalize(summary.kind) === kind;

  if (queryTokens.length === 0) {
    return entries
      .filter(({ summary }) => inDomain(summary) && ofKind(summary))
      .sort((a, b) => a.summary.key.localeCompare(b.summary.key, "vi"))
      .slice(0, limit)
      .map(({ summary }) => summary);
  }

  return entries
    .map((entry) => {
      const lexical = scoreMatch(entry, queryTokens);
      const memory = learnedScore.get(entry.summary.key);
      if (lexical === 0 && memory === undefined) return { ...entry, score: 0 };
      // Đã từng ra dữ liệu cho câu tương tự thắng mọi độ khớp chữ, nhưng có
      // trần để một lần học sai không đè bẹp mãi mãi.
      const recall = memory === undefined ? 0 : 6 + Math.min(memory, 10);
      const hints = (domain && inDomain(entry.summary) ? 2 : 0) + (kind && ofKind(entry.summary) ? 1 : 0);
      return { ...entry, score: lexical + recall + hints };
    })
    .filter(({ score }) => score > 0)
    .sort((a, b) => b.score - a.score || a.summary.key.localeCompare(b.summary.key, "vi"))
    .slice(0, limit)
    .map(({ summary }) => summary);
}

/**
 * Danh sách -> capability chi tiết của nó, ghép theo đường dẫn
 * (`/projects` <-> `/projects/{node}`). Key thì không ghép được: `projects` đi
 * với `project_detail`, route tự sinh thì `_list` đi với `_detail`.
 */
function detailIndex(catalog: Catalog): Map<string, string> {
  const trailingParam = /\/\{[^/}]+\}$/;
  const byPath = new Map<string, string>();
  for (const [key, tool] of Object.entries(catalog)) {
    if (tool.kind === "detail" && trailingParam.test(tool.path)) {
      byPath.set(tool.path.replace(trailingParam, ""), key);
    }
  }
  const index = new Map<string, string>();
  for (const [key, tool] of Object.entries(catalog)) {
    const detail = tool.kind === "list" ? byPath.get(tool.path) : undefined;
    if (detail) index.set(key, detail);
  }
  return index;
}

interface IndexedCapability {
  summary: CapabilitySummary;
  label: string;
  haystack: string;
}

function summarize(catalog: Catalog): IndexedCapability[] {
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
      const haystack = ` ${tokenize([
        summary.key,
        summary.label,
        summary.domain,
        summary.kind,
        summary.description,
      ].join(" ")).join(" ")} `;
      return { summary, label: ` ${tokenize(summary.label).join(" ")} `, haystack };
    });
}

/**
 * Tiếng Việt một từ thường là HAI âm tiết ("xuất kho", "làm thêm"), nên cặp
 * liền nhau được cộng thêm để "phiếu xuất kho" xếp trên mọi thứ chỉ có "phiếu".
 */
function scoreMatch({ label, haystack }: IndexedCapability, tokens: string[]): number {
  let score = 0;
  for (const token of tokens) {
    if (label.includes(` ${token} `)) {
      score += 3;
    } else if (haystack.includes(` ${token} `)) {
      score += 1;
    }
  }
  for (let i = 0; i + 1 < tokens.length; i++) {
    if (haystack.includes(` ${tokens[i]} ${tokens[i + 1]} `)) {
      score += 2;
    }
  }
  return score;
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
