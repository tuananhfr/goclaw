import type { CatalogTool, Catalog, CatalogResponse } from "./types.js";

/**
 * Cầu nối tới REST API của ERPcons, mang danh tính của ĐÚNG một người.
 *
 * =============================================================================
 * MỖI KẾT NỐI MỘT TOKEN, KHÔNG DÙNG CHUNG
 * =============================================================================
 * GoClaw tách kết nối MCP theo (tenant, server, userID) và gắn credential riêng
 * của từng người vào header lúc mở kết nối. Nên một instance của lớp này phục vụ
 * đúng một người, và mọi lời gọi nó phát ra đều được Drupal xác thực thành chính
 * người đó. Không có chỗ nào nhận `userId` làm tham số — nếu có, đó sẽ là đường
 * để một người đọc dữ liệu của người khác.
 */
export class ErpClient {
  /**
   * Gọi khi ERPcons trả 401, tức token gắn vào kết nối này đã hết giá trị.
   *
   * Đặt ở đây thay vì để `index.ts` tự dò mã lỗi vì chỉ lớp này biết chắc 401
   * đến từ token chứ không phải từ một lỗi khác.
   */
  onUnauthorized?: () => void;

  constructor(
    private readonly baseUrl: string,
    private readonly token: string,
    private readonly timeoutMs: number,
  ) {}

  /**
   * Tải danh mục tool. Trả `{}` khi người này chưa được bật trợ lý.
   *
   * Lỗi mạng ném ra ngoài để `index.ts` quyết định — lúc mở kết nối thì "không
   * lấy được catalog" và "người này không có tool nào" phải phân biệt được,
   * nếu không thì GoClaw ngừng gặp sự cố mà tưởng là cấu hình đúng.
   */
  async fetchCatalog(): Promise<Catalog> {
    const body = await this.getJson<CatalogResponse>("/assistant/tools", {});
    return body.tools ?? {};
  }

  /**
   * Gọi một tool: ghép URL, gọi GET, cắt gọn phản hồi.
   *
   * Tham số đã được zod kiểm ở tầng trên nên ở đây chỉ còn việc ghép.
   */
  async callTool(tool: CatalogTool, args: Record<string, unknown>): Promise<unknown> {
    let path = tool.path;
    const query: Record<string, string> = {};

    for (const [name, spec] of Object.entries(tool.params ?? {})) {
      const value = args[name];
      if (value === undefined || value === null || value === "") continue;

      if (spec.in === "path") {
        // encodeURIComponent chứ không nối thẳng: tham số đường dẫn tới từ LLM,
        // và một dấu `/` lọt vào là đổi hẳn endpoint được gọi.
        path = path.replace(`{${name}}`, encodeURIComponent(String(value)));
      } else {
        query[name] = String(value);
      }
    }

    // Còn `{...}` nghĩa là thiếu tham số bắt buộc. Dừng ở đây thay vì để Drupal
    // trả 404 — thông báo "không tìm thấy" sẽ khiến LLM tưởng dữ liệu không tồn
    // tại rồi trả lời sai, thay vì hỏi lại người dùng.
    const missing = path.match(/\{([a-z0-9_]+)\}/i);
    if (missing) {
      throw new ToolInputError(`Thiếu tham số bắt buộc "${missing[1]}".`);
    }

    const raw = await this.getJson<Record<string, unknown>>(path, query);
    return trimResponse(raw, tool);
  }

  private async getJson<T>(path: string, query: Record<string, string>): Promise<T> {
    const url = new URL(this.baseUrl.replace(/\/$/, "") + path);
    for (const [k, v] of Object.entries(query)) url.searchParams.set(k, v);

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);

    let response: Response;
    try {
      response = await fetch(url, {
        method: "GET",
        headers: {
          "X-ERP-Assistant-Token": this.token,
          Accept: "application/json",
        },
        signal: controller.signal,
      });
    } catch (err) {
      const aborted = err instanceof Error && err.name === "AbortError";
      throw new ErpRequestError(
        aborted
          ? "ERPcons không phản hồi kịp. Thử lại sau ít phút."
          : "Không kết nối được tới ERPcons.",
        aborted ? 504 : 502,
      );
    } finally {
      clearTimeout(timer);
    }

    if (!response.ok) {
      if (response.status === 401) this.onUnauthorized?.();
      throw new ErpRequestError(messageForStatus(response.status), response.status);
    }

    return (await response.json()) as T;
  }
}

/** Đầu vào của LLM không hợp lệ — nói lại cho nó sửa, không phải lỗi hệ thống. */
export class ToolInputError extends Error {}

/** ERPcons từ chối hoặc không trả lời. `status` quyết định có thử lại không. */
export class ErpRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

/**
 * Câu trả lời cho LLM khi Drupal từ chối.
 *
 * Viết bằng tiếng Việt và theo giọng nghiệp vụ vì chuỗi này đi thẳng vào ngữ
 * cảnh của mô hình rồi thường vọng ra câu trả lời cho người dùng. "403" thì mô
 * hình sẽ bịa ra một lời giải thích; "bạn không có quyền" thì nó nói đúng.
 */
function messageForStatus(status: number): string {
  // 401 và 403 KHÔNG được nói giống nhau.
  //
  // 403 = Drupal biết bạn là ai và từ chối: câu trả lời đúng là "bạn không có
  // quyền". 401 = token không còn giá trị (đã xoay, đã thu hồi, hoặc người này
  // vừa bị gỡ khỏi bảng gán agent) — nói "không có quyền" ở đây là đổ lỗi cho
  // người dùng về một sự cố kỹ thuật, và họ sẽ đi hỏi quản trị về một quyền mà
  // họ vẫn đang có.
  if (status === 401) {
    return "Phiên tra cứu ERPcons đã hết hiệu lực. Đóng và mở lại trợ lý để được cấp lại quyền tra cứu.";
  }
  if (status === 403) {
    return "Bạn không có quyền xem dữ liệu này trong ERPcons.";
  }
  if (status === 404) {
    return "Không tìm thấy dữ liệu tương ứng trong ERPcons.";
  }
  if (status >= 500) {
    return "ERPcons đang gặp sự cố, chưa lấy được dữ liệu.";
  }
  return `ERPcons từ chối yêu cầu (mã ${status}).`;
}

/**
 * Cắt gọn phản hồi theo `trim`/`drop` của catalog.
 *
 * Hai lý do phải cắt, xem docblock đầu `tools/catalog.yml` bên Drupal: kích
 * thước (một kế hoạch Dplan trả 64KB) và dữ liệu nhạy cảm (hồ sơ nhân sự có
 * CCCD, số tài khoản). `keep` là danh sách TRẮNG nên trường mới thêm bên Drupal
 * không tự lọt vào ngữ cảnh mô hình.
 */
export function trimResponse(raw: Record<string, unknown>, tool: CatalogTool): unknown {
  const out: Record<string, unknown> = { ...raw };

  // `ok` luôn true ở đây (lỗi đã ném ở trên) nên chỉ tốn token.
  delete out.ok;
  for (const key of tool.drop ?? []) delete out[key];

  for (const [key, rule] of Object.entries(tool.trim ?? {})) {
    const value = out[key];
    if (value === undefined || value === null) continue;

    if (Array.isArray(value)) {
      const limit = rule.max_items ?? value.length;
      const sliced = value.slice(0, limit);
      out[key] = rule.keep ? sliced.map((item) => pick(item, rule.keep!)) : sliced;

      // Nói rõ đã cắt. Thiếu dòng này thì mô hình tưởng đã thấy hết và trả lời
      // "bạn có 40 việc" trong khi thực tế là 300.
      if (value.length > limit) {
        out[`${key}_truncated`] =
          `Đã cắt còn ${limit}/${value.length} dòng. Thu hẹp bộ lọc nếu cần xem phần còn lại.`;
      }
    } else if (rule.keep) {
      out[key] = pick(value, rule.keep);
    }
  }

  // `pager` chỉ cần tổng số; `page`/`limit` là chuyện của client.
  const pager = out.pager as Record<string, unknown> | undefined;
  if (pager && typeof pager === "object") {
    out.total = pager.total ?? pager.count;
    delete out.pager;
  }

  return out;
}

function pick(value: unknown, keep: string[]): unknown {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return value;
  const source = value as Record<string, unknown>;
  const picked: Record<string, unknown> = {};
  for (const key of keep) {
    if (source[key] !== undefined) picked[key] = source[key];
  }
  return picked;
}
